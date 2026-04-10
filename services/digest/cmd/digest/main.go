// Command digest is the Navi daily intelligence service.
// It initialises all dependencies, serves health endpoints, and shuts down cleanly.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	digestapi "github.com/rutabageldev/navi/services/digest/internal/api"
	internalnats "github.com/rutabageldev/navi/services/internal/nats"
	"github.com/rutabageldev/navi/services/internal/postgres"
	"github.com/rutabageldev/navi/services/internal/telemetry"
	"github.com/rutabageldev/navi/services/internal/vault"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// --- 1. Read required environment variables ---
	env := requireEnv("NAVI_ENV")
	vaultAddr := requireEnv("VAULT_ADDR")
	vaultToken := requireEnv("VAULT_TOKEN")
	naviHost := getEnv("NAVI_HOST", "0.0.0.0")
	port := getEnv("NAVI_PORT", "8080")
	logLevel := getEnv("NAVI_LOG_LEVEL", "info")

	// --- 2. Initialise structured logger (early, before Vault) ---
	level := parseLogLevel(logLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With(
		"service", "digest",
		"version", version,
		"environment", env,
	)
	slog.SetDefault(logger)

	// --- 3. Initialise Vault ---
	slog.Info("connecting to vault", "addr", vaultAddr)
	vc, err := vault.NewClient(vaultAddr, vaultToken)
	if err != nil {
		return fmt.Errorf("initialising vault: %w", err)
	}

	// --- 4. Retrieve secrets from Vault ---
	prefix := "secret/data/navi/" + env
	pgCfg, err := loadPostgresConfig(vc, prefix+"/postgres")
	if err != nil {
		return fmt.Errorf("loading postgres config: %w", err)
	}
	natsCfg, err := loadNATSConfig(vc, prefix+"/nats", prefix+"/nats/tls")
	if err != nil {
		return fmt.Errorf("loading nats config: %w", err)
	}
	otlpEndpoint, err := vc.GetSecret(prefix+"/telemetry", "endpoint")
	if err != nil {
		return fmt.Errorf("loading telemetry endpoint: %w", err)
	}

	// --- 5. Initialise OTEL ---
	ctx := context.Background()
	shutdown, err := telemetry.InitTracer(ctx, telemetry.Config{
		ServiceName:    "navi-digest",
		ServiceVersion: version,
		Environment:    env,
		OTLPEndpoint:   otlpEndpoint,
	})
	if err != nil {
		return fmt.Errorf("initialising telemetry: %w", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutCtx); err != nil {
			slog.Error("telemetry shutdown error", "error", err)
		}
	}()

	// --- 6. Initialise Postgres ---
	slog.Info("connecting to postgres", "host", pgCfg.Host, "schema", pgCfg.Schema)
	pool, err := postgres.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pool.Close()

	if err := postgres.RunMigrations(pool, "file://services/digest/migrations", pgCfg.Schema); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// --- 7. Initialise NATS ---
	slog.Info("connecting to nats", "url", natsCfg.URL)
	nc, err := internalnats.Connect(natsCfg)
	if err != nil {
		return fmt.Errorf("connecting to nats: %w", err)
	}
	defer func() {
		if err := nc.Drain(); err != nil {
			slog.Error("nats drain error", "error", err)
		}
	}()

	streamName := "NAVI_" + env
	js, err := internalnats.JetStream(nc)
	if err != nil {
		return fmt.Errorf("creating jetstream context: %w", err)
	}
	if err := internalnats.EnsureStream(js, streamName, []string{"navi." + env + ".>"}); err != nil {
		return fmt.Errorf("ensuring nats stream: %w", err)
	}

	// --- 8. Register SIGHUP reload ---
	vault.RegisterSIGHUPReload(func() error {
		slog.Info("SIGHUP received — reloading secrets")
		// Secrets are re-read from Vault on next request; stateless reload.
		return nil
	})

	// --- 9. Build HTTP server ---
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(requestIDResponse)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)

	h := digestapi.NewHandler(vc, pool, nc, version)
	r.Get("/v1/health/live", h.HealthLive)
	r.Get("/v1/health/ready", h.HealthReady)

	addr := naviHost + ":" + port
	srv := &http.Server{
		Addr:         addr,
		Handler:      telemetry.NewHTTPHandler("navi-digest", r),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("navi digest service started",
		"version", version,
		"environment", env,
		"addr", addr,
	)

	// --- 10. Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server error: %w", err)
	case sig := <-quit:
		slog.Info("shutting down", "signal", sig.String())
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}

	return nil
}

// requestIDResponse copies the request ID assigned by middleware.RequestID into
// the X-Request-ID response header, satisfying the ADR-0010 baseline requirement.
func requestIDResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rid := middleware.GetReqID(r.Context()); rid != "" {
			w.Header().Set("X-Request-ID", rid)
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs each completed request at INFO level with method, path,
// status, duration, and request ID, satisfying the ADR-0010 baseline requirement.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http request", //nolint:gosec // G706: logging request path is intentional in a request logger
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
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

// loadNATSConfig reads NATS connection parameters and TLS material from Vault.
// connPath holds the url and seed fields; tlsPath holds the cert, key, and ca fields.
func loadNATSConfig(vc *vault.Client, connPath, tlsPath string) (internalnats.Config, error) {
	get := func(path, key string) (string, error) {
		return vc.GetSecret(path, key)
	}
	url, err := get(connPath, "url")
	if err != nil {
		return internalnats.Config{}, err
	}
	seed, err := get(connPath, "seed")
	if err != nil {
		return internalnats.Config{}, err
	}
	cert, err := get(tlsPath, "cert")
	if err != nil {
		return internalnats.Config{}, err
	}
	key, err := get(tlsPath, "key")
	if err != nil {
		return internalnats.Config{}, err
	}
	ca, err := get(tlsPath, "ca")
	if err != nil {
		return internalnats.Config{}, err
	}
	return internalnats.Config{
		URL:      url,
		NKeySeed: seed,
		TLSCert:  []byte(cert),
		TLSKey:   []byte(key),
		TLSCA:    []byte(ca),
	}, nil
}

// requireEnv returns the value of an environment variable or exits with an error.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

// getEnv returns the value of an environment variable or a default.
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseLogLevel converts a string log level to slog.Level.
func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
