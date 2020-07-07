package db

import (
	"fmt"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/treeverse/lakefs/logging"
)

func extractSchemaFromURI(uri string) (string, error) {
	var schema string
	parsed, err := url.Parse(uri)
	if err != nil {
		return schema, err
	}
	schema = parsed.Query().Get("search_path")
	if schema == "" {
		return "", fmt.Errorf("search_path not present in database connection string")
	}
	return schema, nil
}

func ConnectDB(driver string, uri string) (Database, error) {
	schema, err := extractSchemaFromURI(uri)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	conn, err := sqlx.Connect(driver, uri)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	// TODO(barak): viper configuration with defaults
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(25)
	conn.SetConnMaxLifetime(5 * time.Minute)

	tx, err := conn.Beginx()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	// validate collate "C"
	var collate string
	row := tx.QueryRow("SELECT datcollate AS collation FROM pg_database WHERE datname = current_database()")
	err = row.Scan(&collate)
	if err != nil {
		return nil, fmt.Errorf("could not validate database collate: %w", err)
	}
	if collate != "C" {
		return nil, fmt.Errorf("connected database collate (%s) is not set to \"C\"", collate)
	}
	_, err = tx.Exec("CREATE SCHEMA IF NOT EXISTS " + schema)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	logging.Default().WithFields(logging.Fields{
		"driver": driver,
		"uri":    uri,
	}).Info("initialized DB connection")
	return NewSqlxDatabase(conn), nil
}
