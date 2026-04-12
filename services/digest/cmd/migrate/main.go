// Command migrate runs pending database migrations for the navi digest service.
// It loads Postgres credentials from Vault and applies all unapplied migrations
// under services/digest/migrations/ against the schema for the target environment.
//
// Usage:
//
//	migrate -env <dev|staging|prod>
//
// Required environment variables:
//
//	VAULT_ADDR   — Vault server address (e.g. https://10.0.40.10:8200)
//	VAULT_TOKEN  — Vault token with read access to secret/data/navi/{env}/postgres
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/rutabageldev/navi/services/internal/postgres"
	"github.com/rutabageldev/navi/services/internal/vault"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	env := flag.String("env", "", "environment: dev | staging | prod")
	flag.Parse()

	if *env == "" {
		*env = os.Getenv("NAVI_ENV")
	}
	switch *env {
	case "dev", "staging", "prod":
	case "":
		return fmt.Errorf("-env flag or NAVI_ENV environment variable is required")
	default:
		return fmt.Errorf("unknown environment %q — want dev|staging|prod", *env)
	}

	vaultAddr := os.Getenv("VAULT_ADDR")
	if vaultAddr == "" {
		return fmt.Errorf("VAULT_ADDR environment variable is required")
	}
	vaultToken := os.Getenv("VAULT_TOKEN")
	if vaultToken == "" {
		return fmt.Errorf("VAULT_TOKEN environment variable is required")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).With(
		"service", "digest",
		"component", "migrate",
		"environment", *env,
	)
	slog.SetDefault(logger)

	slog.Info("connecting to vault", "addr", vaultAddr)
	vc, err := vault.NewClient(vaultAddr, vaultToken)
	if err != nil {
		return fmt.Errorf("initialising vault: %w", err)
	}

	pgCfg, err := loadPostgresConfig(vc, "secret/data/navi/"+*env+"/postgres")
	if err != nil {
		return fmt.Errorf("loading postgres config: %w", err)
	}

	ctx := context.Background()
	slog.Info("connecting to postgres", "host", pgCfg.Host, "schema", pgCfg.Schema)
	pool, err := postgres.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pool.Close()

	slog.Info("running migrations", "path", "services/digest/migrations", "schema", pgCfg.Schema)
	if err := postgres.RunMigrations(pool, "file://services/digest/migrations", pgCfg.Schema); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	slog.Info("migrations complete")
	return nil
}

// loadPostgresConfig reads all required postgres fields from Vault.
func loadPostgresConfig(vc *vault.Client, path string) (postgres.Config, error) {
	get := func(key string) (string, error) {
		return vc.GetSecret(path, key)
	}
	host, err := get("host")
	if err != nil {
		return postgres.Config{}, err
	}
	port, err := get("port")
	if err != nil {
		return postgres.Config{}, err
	}
	user, err := get("user")
	if err != nil {
		return postgres.Config{}, err
	}
	password, err := get("password")
	if err != nil {
		return postgres.Config{}, err
	}
	database, err := get("database")
	if err != nil {
		return postgres.Config{}, err
	}
	schema, err := get("schema")
	if err != nil {
		return postgres.Config{}, err
	}
	return postgres.Config{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Database: database,
		Schema:   schema,
	}, nil
}
