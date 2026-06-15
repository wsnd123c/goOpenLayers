package postgis

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestFormatPGXTraceLog(t *testing.T) {
	msg := formatPGXTraceLog("Query", map[string]any{
		"args": []any{"editing"},
		"err": &pgconn.PgError{
			Message: "bind message supplies 1 parameters, but prepared statement requires 0",
			Code:    "08P01",
		},
		"pid":  123,
		"time": int64(36507000),
		"sql":  "SELECT *\nFROM task_63980e78fa33.prod_JZW\nWHERE status = $1",
	})

	for _, unwanted := range []string{"map[string]interface", "[]interface", "(*pgconn.PgError)", "\\n"} {
		if strings.Contains(msg, unwanted) {
			t.Fatalf("formatted log contains %q: %s", unwanted, msg)
		}
	}

	for _, wanted := range []string{
		"PostGIS(pgx): Query",
		`err="bind message supplies 1 parameters, but prepared statement requires 0 code=08P01"`,
		"args=[editing]",
		"pid=123",
		"time=36.507ms",
		`sql="SELECT * FROM task_63980e78fa33.prod_JZW WHERE status = $1"`,
	} {
		if !strings.Contains(msg, wanted) {
			t.Fatalf("formatted log missing %q: %s", wanted, msg)
		}
	}
}
