// Package nats provides NATS JetStream connection helpers with retry logic,
// stream management, and health checking.
package nats

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Connect establishes a NATS connection with up to 3 attempts using exponential
// backoff (1s, 2s). Returns an error if all attempts fail.
func Connect(url string) (*nats.Conn, error) {
	var (
		conn *nats.Conn
		err  error
	)
	for attempt := 0; attempt < 3; attempt++ {
		conn, err = nats.Connect(url)
		if err == nil {
			return conn, nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}
	return nil, fmt.Errorf("connecting to NATS at %q after 3 attempts: %w", url, err)
}

// JetStream returns a JetStream context from an existing connection.
func JetStream(conn *nats.Conn) (nats.JetStreamContext, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("creating jetstream context: %w", err)
	}
	return js, nil
}

// EnsureStream creates the JetStream stream named name with the given subjects if
// it does not already exist. It is safe to call on every service startup.
func EnsureStream(js nats.JetStreamContext, name string, subjects []string) error {
	_, err := js.StreamInfo(name)
	if err == nil {
		return nil
	}
	if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("checking NATS stream %q: %w", name, err)
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:     name,
		Subjects: subjects,
	}); err != nil {
		return fmt.Errorf("creating NATS stream %q: %w", name, err)
	}
	return nil
}

// HealthCheck returns nil if the connection is open and not draining.
func HealthCheck(conn *nats.Conn) error {
	if !conn.IsConnected() {
		return fmt.Errorf("NATS connection is not open (status: %s)", conn.Status())
	}
	return nil
}
