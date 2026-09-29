package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Connect(dbUrl string) (*sql.DB, error) {
	conn, err := sql.Open("pgx", dbUrl)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: Unable to connect to database: %w", err)
	}
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(25)
	conn.SetConnMaxLifetime(5 * 60) // 5 minutes
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("sql.Ping: Ping database failed, %w", err)
	}
	return conn, nil
}
