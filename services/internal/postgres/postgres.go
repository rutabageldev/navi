// Package postgres provides connection pool setup, migration running, and health
// checks for the Foundation Postgres instance.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // file source driver
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Config holds connection parameters for Postgres.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	Schema   string
}

// Connect creates a pgx connection pool, sets the search_path to cfg.Schema,
// and verifies connectivity. Returns an error if the database is unreachable or
// credentials are invalid.
func Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?search_path=%s&sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Schema,
	)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return pool, nil
}

// RunMigrations runs all pending migrations from migrationsPath against the
// database represented by pool. Migrations are scoped to schema. A source path
// of "file://services/digest/migrations" is typical when run from the repo root.
func RunMigrations(db *pgxpool.Pool, migrationsPath, schema string) error {
	sqlDB := stdlib.OpenDBFromPool(db)
	defer func() { _ = sqlDB.Close() }()

	driver, err := migratepostgres.WithInstance(sqlDB, &migratepostgres.Config{
		SchemaName: schema,
	})
	if err != nil {
		return fmt.Errorf("creating migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(migrationsPath, "postgres", driver)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

// HealthCheck executes a trivial query to verify the connection pool is live.
func HealthCheck(ctx context.Context, db *pgxpool.Pool) error {
	var n int
	if err := db.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
		return fmt.Errorf("postgres health check: %w", err)
	}
	return nil
}
