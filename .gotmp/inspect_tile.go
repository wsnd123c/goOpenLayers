package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) != 6 {
		fmt.Println("usage: inspect_tile <schema> <table> <z> <x> <y>")
		os.Exit(2)
	}

	schemaName := os.Args[1]
	tableName := os.Args[2]
	z, _ := strconv.Atoi(os.Args[3])
	x, _ := strconv.Atoi(os.Args[4])
	y, _ := strconv.Atoi(os.Args[5])

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://postgres:postgres@127.0.0.1:35432/rjxt?sslmode=disable")
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	var geomColumn string
	err = pool.QueryRow(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = lower($1)
		  AND table_name = lower($2)
		  AND udt_name IN ('geometry', 'geography')
		ORDER BY CASE lower(column_name)
			WHEN 'geom' THEN 0
			WHEN 'geometry' THEN 1
			ELSE 2
		END, ordinal_position
		LIMIT 1;
	`, schemaName, tableName).Scan(&geomColumn)
	if err != nil {
		panic(fmt.Errorf("geometry column lookup: %w", err))
	}

	q := fmt.Sprintf(`
		WITH tile AS (
			SELECT ST_TileEnvelope(%d, %d, %d) AS env
		)
		SELECT
			count(*) AS total,
			count(*) FILTER (WHERE %s IS NOT NULL) AS with_geom,
			COALESCE(ST_SRID(%s), 0) AS srid,
			ST_AsText(ST_Extent(%s)) AS extent,
			count(*) FILTER (WHERE %s && tile.env) AS bbox_hits,
			count(*) FILTER (WHERE ST_Intersects(%s, tile.env)) AS intersects_hits,
			count(*) FILTER (WHERE ST_Intersects(%s, ST_Transform(tile.env, ST_SRID(%s)))) AS transformed_hits,
			ST_AsText(tile.env) AS tile_env_3857,
			ST_AsText(ST_Transform(tile.env, 4326)) AS tile_env_4326
		FROM %s.%s, tile
		GROUP BY tile.env, %s
		LIMIT 1;
	`,
		z, x, y,
		quoteIdent(geomColumn),
		quoteIdent(geomColumn),
		quoteIdent(geomColumn),
		quoteIdent(geomColumn),
		quoteIdent(geomColumn),
		quoteIdent(geomColumn), quoteIdent(geomColumn),
		quoteIdent(schemaName), quoteIdent(tableName),
		quoteIdent(geomColumn),
	)

	var total, withGeom, srid, bboxHits, intersectsHits, transformedHits int64
	var extent, tile3857, tile4326 *string
	err = pool.QueryRow(ctx, q).Scan(&total, &withGeom, &srid, &extent, &bboxHits, &intersectsHits, &transformedHits, &tile3857, &tile4326)
	if err != nil {
		panic(err)
	}

	fmt.Printf("table=%s.%s geom=%s\n", schemaName, tableName, geomColumn)
	fmt.Printf("total=%d with_geom=%d srid=%d\n", total, withGeom, srid)
	fmt.Printf("bbox_hits=%d intersects_hits=%d transformed_hits=%d\n", bboxHits, intersectsHits, transformedHits)
	if extent != nil {
		fmt.Printf("extent=%s\n", *extent)
	}
	if tile3857 != nil {
		fmt.Printf("tile_env_3857=%s\n", *tile3857)
	}
	if tile4326 != nil {
		fmt.Printf("tile_env_4326=%s\n", *tile4326)
	}
}

func quoteIdent(s string) string {
	out := `"`
	for _, r := range s {
		if r == '"' {
			out += `""`
		} else {
			out += string(r)
		}
	}
	return out + `"`
}
