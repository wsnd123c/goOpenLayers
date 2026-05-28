package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-spatial/tegola/internal/log"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Query parameter holds normalized parameter data ready to be inserted in the
// final query
type QueryParameterValue struct {
	// Token to replace e.g., !TOKEN!
	Token string
	// SQL expression to be inserted. Contains "?" that will be replaced with an
	//  ordinal argument e.g., "$1"
	SQL string
	// Value that will be passed to the final query in arguments list
	Value interface{}
	// Raw parameter and value for debugging and monitoring
	RawParam string
	// RawValue will be "" if the param wasn't passed and defaults were used
	RawValue string
}

type Params map[string]QueryParameterValue

const (
	taskIDParamToken      = "!TASKID!"
	schemaParamToken      = "!SCHEMA!"
	dynamicGeomParamToken = "!GEOM_COLUMN!"
)

var safeIdentifierRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func safeIdentifier(name string) bool {
	return safeIdentifierRegexp.MatchString(name)
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (params Params) schemaName() (string, bool) {
	if params == nil {
		return "", false
	}

	v, ok := params[schemaParamToken]
	if !ok {
		return "", false
	}

	schemaName := fmt.Sprintf("%v", v.Value)
	if schemaName == "" {
		return "", false
	}
	if !safeIdentifier(schemaName) {
		log.Errorf("invalid schema name: %s", schemaName)
		return "", false
	}

	return schemaName, true
}

// 处理 !TASKID! 的公共函数。qualifySchema 为 true 时，URL 中的 schema 会拼到表名前。
func (params Params) replaceTaskID(name string, qualifySchema bool) string {
	if params == nil {
		return name
	}

	if v, ok := params[taskIDParamToken]; ok {
		tableName := fmt.Sprintf("%v", v.Value)

		// 安全校验（只允许字母、数字、下划线）
		if !safeIdentifier(tableName) {
			log.Errorf("invalid table name: %s", tableName)
			return name
		}

		if qualifySchema {
			if schemaName, ok := params.schemaName(); ok {
				tableName = schemaName + "." + tableName
			}
		}

		// 替换 !TASKID! 为 tableName
		name = strings.ReplaceAll(name, taskIDParamToken, tableName)
	} else {
		log.Warn("param not found in request")
	}

	return name
}

// ReplaceParams substitutes configured query parameter tokens for their values
// within the provided SQL string
func (params Params) ReplaceParams(sql string, args *[]interface{}) string {
	if params == nil {
		//log.Warn("ReplaceParams called with nil params")
		return sql
	}

	var (
		cache = make(map[string]string)
		sb    strings.Builder
	)
	sql = params.replaceTaskID(sql, true) // 使用公共函数处理 !TASKID!
	for _, token := range ParameterTokenRegexp.FindAllString(sql, -1) {

		// ---- 2. 默认参数替换逻辑 ----
		resultSQL, ok := cache[token]
		if ok {
			sql = strings.ReplaceAll(sql, token, resultSQL)
			continue
		}

		param, ok := params[token]
		if !ok {
			// 未识别的 token，跳过
			continue
		}

		sb.Reset()
		sb.Grow(len(param.SQL))
		argFound := false

		// 替换 param 中的 ?
		for _, c := range param.SQL {
			if c != '?' {
				sb.WriteRune(c)
				continue
			}

			if !argFound {
				*args = append(*args, param.Value)
				argFound = true
			}
			sb.WriteString(fmt.Sprintf("$%d", len(*args)))
		}

		resultSQL = sb.String()
		cache[token] = resultSQL
		sql = strings.ReplaceAll(sql, token, resultSQL)
	}

	//log.Infof("Final SQL after ReplaceParams:\n%s", sql)
	return sql
}

// 自个造的一个处理动态传过来mvt的方法
func (params Params) ReplaceMvtTableName(name string) string {
	return params.replaceTaskID(name, false) // MVT 图层名保持老逻辑，不拼 schema
}

// ReplaceTableName 返回用于 SQL 访问的真实表名，传 schema 时会拼成 schema.table。
func (params Params) ReplaceTableName(name string) string {
	return params.replaceTaskID(name, true)
}

func splitQualifiedTableName(tableName string) (schemaName, unqualifiedTableName string, err error) {
	parts := strings.Split(tableName, ".")
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("invalid qualified table name: %s", tableName)
	}
}

type TableMetadata struct {
	Columns       string
	GeometryField string
}

func GetColumnsFromDB(ctx context.Context, pool *pgxpool.Pool, tableName, geomField string) (string, error) {
	metadata, err := GetTableMetadataFromDB(ctx, pool, tableName, geomField)
	if err != nil {
		return "", err
	}

	return metadata.Columns, nil
}

func GetTableMetadataFromDB(ctx context.Context, pool *pgxpool.Pool, tableName, geomField string) (TableMetadata, error) {
	schemaName, unqualifiedTableName, err := splitQualifiedTableName(tableName)
	if err != nil {
		return TableMetadata{}, err
	}
	schemaName = strings.ToLower(schemaName)
	unqualifiedTableName = strings.ToLower(unqualifiedTableName)

	var (
		query string
		args  []interface{}
	)
	if schemaName == "" {
		query = `
		SELECT column_name, udt_name
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position;
	`
		args = []interface{}{unqualifiedTableName}
	} else {
		query = `
		SELECT column_name, udt_name
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		ORDER BY ordinal_position;
	`
		args = []interface{}{schemaName, unqualifiedTableName}
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return TableMetadata{}, fmt.Errorf("querying columns for %s: %w", tableName, err)
	}
	defer rows.Close()

	type columnInfo struct {
		name    string
		udtName string
	}

	var columns []columnInfo
	var geometryCandidates []string
	for rows.Next() {
		var col, udtName string
		if err := rows.Scan(&col, &udtName); err != nil {
			return TableMetadata{}, err
		}
		columns = append(columns, columnInfo{name: col, udtName: udtName})
		if udtName == "geometry" || udtName == "geography" {
			geometryCandidates = append(geometryCandidates, col)
		}
	}
	if err := rows.Err(); err != nil {
		return TableMetadata{}, err
	}
	if len(columns) == 0 {
		return TableMetadata{}, fmt.Errorf("no columns found for table %s", tableName)
	}

	geometryField := ""
	preferredGeomField := strings.ToLower(geomField)
	for _, candidate := range geometryCandidates {
		if strings.ToLower(candidate) == preferredGeomField {
			geometryField = candidate
			break
		}
	}
	if geometryField == "" {
		for _, preferred := range []string{"geom", "geometry"} {
			for _, candidate := range geometryCandidates {
				if strings.ToLower(candidate) == preferred {
					geometryField = candidate
					break
				}
			}
			if geometryField != "" {
				break
			}
		}
	}
	if geometryField == "" && len(geometryCandidates) > 0 {
		geometryField = geometryCandidates[0]
	}
	if geometryField == "" {
		return TableMetadata{}, fmt.Errorf("no geometry column found for table %s", tableName)
	}

	var cols []string
	for _, column := range columns {
		if column.name == geometryField {
			continue
		}
		// 给列名加上双引号，保证区分大小写
		cols = append(cols, `"`+column.name+`"`)
	}
	if len(cols) == 0 {
		cols = append(cols, "NULL AS _tegola_empty_columns")
	}

	return TableMetadata{
		Columns:       strings.Join(cols, ", "),
		GeometryField: geometryField,
	}, nil
}

// 支持动态列的替换
func (params Params) ReplaceParamsWithColumns(
	ctx context.Context,
	pool *pgxpool.Pool,
	geomField string,
	sql string,
	args *[]interface{},
	mvtTableName string,
) (string, error) {
	if params == nil {
		log.Warn("ReplaceParamsWithColumns called with nil params")
		return sql, nil
	}

	var (
		cache         = make(map[string]string)
		sb            strings.Builder
		tableMetadata *TableMetadata
	)

	getTableMetadata := func() (TableMetadata, error) {
		if tableMetadata != nil {
			return *tableMetadata, nil
		}

		metadata, err := GetTableMetadataFromDB(ctx, pool, mvtTableName, geomField)
		if err != nil {
			return TableMetadata{}, err
		}
		tableMetadata = &metadata
		return metadata, nil
	}

	for _, token := range ParameterTokenRegexp.FindAllString(sql, -1) {
		if token == dynamicGeomParamToken {
			metadata, err := getTableMetadata()
			if err != nil {
				return "", fmt.Errorf("failed to get geometry column for table %s: %w", mvtTableName, err)
			}
			sql = strings.ReplaceAll(sql, token, quoteIdentifier(metadata.GeometryField))
			continue
		}

		//处理 !COLUMNS!
		if token == "!COLUMNS!" {
			if _, ok := params[taskIDParamToken]; ok {
				metadata, err := getTableMetadata()
				if err != nil {
					return "", fmt.Errorf("failed to get columns for table %s: %w", mvtTableName, err)
				}
				//log.Infof("Replacing token %s with columns: %s", token, colList)
				sql = strings.ReplaceAll(sql, token, metadata.Columns)
			} else {
				log.Warn(" param not found for !COLUMNS!")
			}
			continue
		}

		//  默认参数替换逻辑
		resultSQL, ok := cache[token]
		if ok {
			sql = strings.ReplaceAll(sql, token, resultSQL)
			continue
		}

		param, ok := params[token]
		if !ok {

			continue
		}

		sb.Reset()
		sb.Grow(len(param.SQL))
		argFound := false

		// 替换 param 中的 ?
		for _, c := range param.SQL {
			if c != '?' {
				sb.WriteRune(c)
				continue
			}

			if !argFound {
				*args = append(*args, param.Value)
				argFound = true
			}
			sb.WriteString(fmt.Sprintf("$%d", len(*args)))
		}

		resultSQL = sb.String()
		cache[token] = resultSQL
		sql = strings.ReplaceAll(sql, token, resultSQL)
	}

	//log.Infof("Final SQL after ReplaceParamsWithColumns:\n%s", sql)
	return sql, nil
}
