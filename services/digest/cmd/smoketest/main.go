// Command smoketest runs the P0 smoke test suite against a live navi-digest service.
// Each test prints PASS or FAIL to stdout; the binary exits 1 if any test fails.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	env := flag.String("env", "dev", "environment: dev | staging | prod")
	addr := flag.String("addr", "", "host:port override; if empty, resolved from -env")
	flag.Parse()

	if *addr == "" {
		switch *env {
		case "dev":
			*addr = "127.0.0.1:8082"
		case "staging":
			*addr = "10.0.40.10:8081"
		case "prod":
			*addr = "10.0.40.10:8083"
		default:
			fmt.Fprintf(os.Stderr, "unknown env %q — want dev|staging|prod\n", *env)
			os.Exit(1)
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	base := "http://" + *addr

	var passed, failed int
	run := func(name string, fn func() error) {
		if err := fn(); err != nil {
			fmt.Printf("FAIL: %s: %v\n", name, err)
			failed++
		} else {
			fmt.Printf("PASS: %s\n", name)
			passed++
		}
	}

	// ready is populated by TestHealthReady; subsequent tests read from it.
	var ready struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Checks  struct {
			Postgres string `json:"postgres"`
			NATS     string `json:"nats"`
			Vault    string `json:"vault"`
		} `json:"checks"`
	}

	run("TestHealthLive", func() error {
		resp, err := client.Get(base + "/v1/health/live")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("got %d, want 200", resp.StatusCode)
		}
		return nil
	})

	run("TestHealthReady", func() error {
		resp, err := client.Get(base + "/v1/health/ready")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("got %d, want 200: %s", resp.StatusCode, body)
		}
		return json.Unmarshal(body, &ready)
	})

	run("TestVersionPresent", func() error {
		if ready.Version == "" {
			return fmt.Errorf("version field is empty")
		}
		return nil
	})

	run("TestPostgresCheck", func() error {
		if ready.Checks.Postgres != "ok" {
			return fmt.Errorf("postgres=%q", ready.Checks.Postgres)
		}
		return nil
	})

	run("TestNATSCheck", func() error {
		if ready.Checks.NATS != "ok" {
			return fmt.Errorf("nats=%q", ready.Checks.NATS)
		}
		return nil
	})

	run("TestVaultCheck", func() error {
		if ready.Checks.Vault != "ok" {
			return fmt.Errorf("vault=%q", ready.Checks.Vault)
		}
		return nil
	})

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
