package provider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/go-spatial/tegola/internal/log"
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

// ReplaceParams substitutes configured query parameter tokens for their values
// within the provided SQL string
func (params Params) ReplaceParams(sql string, args *[]interface{}) string {
	if params == nil {
		log.Warn("ReplaceParams called with nil params")
		return sql
	}

	var (
		cache = make(map[string]string)
		sb    strings.Builder
	)

	for _, token := range ParameterTokenRegexp.FindAllString(sql, -1) {

		// ---- 1. 特殊处理 taskId ----
		if token == "!TASKID!" {
			if v, ok := params["!TASKID!"]; ok {
				tableName := fmt.Sprintf("%v", v.Value)

				// 安全校验（只允许字母、数字、下划线）
				validName := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
				if !validName.MatchString(tableName) {
					log.Errorf("invalid table name: %s", tableName)
					continue
				}

				log.Infof("Replacing token %s with table: %s", token, tableName)
				sql = strings.ReplaceAll(sql, token, tableName)
			} else {
				log.Warn("taskId param not found in request")
			}
			continue
		}

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

	log.Infof("Final SQL after ReplaceParams:\n%s", sql)
	return sql
}
