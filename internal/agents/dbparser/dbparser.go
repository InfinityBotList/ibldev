package dbparser

import (
	"context"
	"fmt"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type Schema struct {
	TableName  string  `json:"table_name"`
	ColumnName string  `json:"column_name"`
	Type       string  `json:"type"`
	IsNullable bool    `json:"nullable"`
	Array      bool    `json:"array"`
	DefaultSQL *string `json:"default_sql"`
	DefaultVal any     `json:"default_val"`
}

type schemaData struct {
	ColumnDefault *string `db:"column_default"`
	TableName     string  `db:"table_name"`
	ColumnName    string  `db:"column_name"`
	DataType      string  `db:"data_type"`
	ElementType   *string `db:"element_type"`
	IsNullable    string  `db:"is_nullable"`
}

func GetSchema(ctx context.Context, pool *pgxpool.Pool) ([]Schema, error) {
	var sqlString string = `
	SELECT c.is_nullable, c.table_name, c.column_name, c.column_default, c.data_type AS data_type, e.data_type AS element_type FROM information_schema.columns c LEFT JOIN information_schema.element_types e
	ON ((c.table_catalog, c.table_schema, c.table_name, 'TABLE', c.dtd_identifier)
= (e.object_catalog, e.object_schema, e.object_name, e.object_type, e.collection_type_identifier))
WHERE table_schema = 'public' order by table_name, ordinal_position
`
	rows, err := pool.Query(ctx, sqlString)

	if err != nil {
		return nil, err
	}

	var result []Schema

	for rows.Next() {
		var schema Schema

		data := schemaData{}

		err := rows.Scan(&data.IsNullable, &data.TableName, &data.ColumnName, &data.ColumnDefault, &data.DataType, &data.ElementType)

		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		// Create new transaction to get default column
		if data.ColumnDefault != nil && *data.ColumnDefault != "" {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return nil, err
			}

			var defaultV any

			err = tx.QueryRow(ctx, "SELECT "+*data.ColumnDefault).Scan(&defaultV)

			if err != nil {
				return nil, err
			}

			err = tx.Rollback(ctx)

			if err != nil {
				return nil, err
			}

			// Check for [16]uint8 case
			if defaultVal, ok := defaultV.([16]uint8); ok {
				defaultV = fmt.Sprintf("%x-%x-%x-%x-%x", defaultVal[0:4], defaultVal[4:6], defaultVal[6:8], defaultVal[8:10], defaultVal[10:16])
			}

			schema.DefaultVal = defaultV
		} else {
			schema.DefaultVal = nil
		}

		// Now check if the column is tagged properly
		var itag pgtype.UUID
		if err := pool.QueryRow(ctx, "SELECT itag FROM"+data.TableName).Scan(&itag); err != nil {
			if err == pgx.ErrNoRows {
				fmt.Println("Tagging", data.TableName)
				_, err := pool.Exec(ctx, "ALTER TABLE "+data.TableName+" ADD COLUMN itag uuid not null unique default uuid_generate_v4()")
				if err != nil {
					return nil, err
				}
			}
		}

		schema.ColumnName = data.ColumnName
		schema.TableName = data.TableName
		schema.DefaultSQL = data.ColumnDefault

		schema.IsNullable = (data.IsNullable == "YES")

		if data.DataType == "ARRAY" {
			schema.Type = *data.ElementType
			schema.Array = true
		} else {
			schema.Type = data.DataType
		}

		result = append(result, schema)
	}

	fmt.Println("Loaded", len(result), "columns into seed-ci")

	return result, nil
}
