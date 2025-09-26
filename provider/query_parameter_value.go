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

// 处理 !TASKID! 的公共函数
func (params Params) replaceTaskID(name string) string {
	if params == nil {
		return name
	}

	if v, ok := params["!TASKID!"]; ok {
		tableName := fmt.Sprintf("%v", v.Value)

		// 安全校验（只允许字母、数字、下划线）
		validName := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
		if !validName.MatchString(tableName) {
			log.Errorf("invalid table name: %s", tableName)
			return name
		}

		// 替换 !TASKID! 为 tableName
		name = strings.ReplaceAll(name, "!TASKID!", tableName)
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
	sql = params.replaceTaskID(sql) // 使用公共函数处理 !TASKID!
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
	return params.replaceTaskID(name) // 使用公共函数处理 !TASKID!
}
func getColumnsFromDB(ctx context.Context, pool *pgxpool.Pool, tableName, geomField string) (string, error) {
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position;
	`

	rows, err := pool.Query(ctx, query, tableName)
	if err != nil {
		return "", fmt.Errorf("querying columns for %s: %w", tableName, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return "", err
		}
		// 排除 geometry 和 id 字段（id 已在 SQL 中明确包含）
		if col == geomField || col == "id" {
			continue
		}
		// 给列名加上双引号，保证区分大小写
		cols = append(cols, `"`+col+`"`)
	}

	return strings.Join(cols, ", "), nil
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
		cache = make(map[string]string)
		sb    strings.Builder
	)

	for _, token := range ParameterTokenRegexp.FindAllString(sql, -1) {

		//处理 !COLUMNS!
		if token == "!COLUMNS!" {
			if _, ok := params["!TASKID!"]; ok {

				colList, err := getColumnsFromDB(ctx, pool, mvtTableName, geomField)
				if err != nil {
					return "", fmt.Errorf("failed to get columns for table %s: %w", mvtTableName, err)
				}
				//log.Infof("Replacing token %s with columns: %s", token, colList)
				sql = strings.ReplaceAll(sql, token, colList)
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
