// Package nats provides NATS JetStream connection helpers with NKEY + mTLS
// authentication, retry logic, stream management, and health checking.
package nats

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// Config holds NATS connection parameters including NKEY authentication and
// mTLS material. All fields are required when connecting to a TLS endpoint.
type Config struct {
	// URL is the NATS server address, e.g. "tls://127.0.0.1:4222".
	URL string
	// NKeySeed is the NKEY seed for this service's identity. Required.
	NKeySeed string
	// TLSCert is the PEM-encoded client certificate. Required for tls:// URLs.
	TLSCert []byte
	// TLSKey is the PEM-encoded client private key. Required for tls:// URLs.
	TLSKey []byte
	// TLSCA is the PEM-encoded CA certificate used to verify the server.
	// Required for tls:// URLs.
	TLSCA []byte
}

// Connect establishes a NATS connection using NKEY authentication and mTLS.
// It retries up to 3 times with exponential backoff (1s, 2s) on failure.
func Connect(cfg Config) (*nats.Conn, error) {
	opts, err := buildOptions(cfg)
	if err != nil {
		return nil, err
	}

	var (
		conn *nats.Conn
		cerr error
	)
	for attempt := 0; attempt < 3; attempt++ {
		conn, cerr = nats.Connect(cfg.URL, opts...)
		if cerr == nil {
			return conn, nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}
	return nil, fmt.Errorf("connecting to NATS at %q after 3 attempts: %w", cfg.URL, cerr)
}

// buildOptions constructs the nats.Option slice from cfg.
func buildOptions(cfg Config) ([]nats.Option, error) {
	kp, err := nkeys.FromSeed([]byte(cfg.NKeySeed))
	if err != nil {
		return nil, fmt.Errorf("parsing NATS NKEY seed: %w", err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("deriving NATS public key: %w", err)
	}

	opts := []nats.Option{
		nats.Nkey(pub, func(nonce []byte) ([]byte, error) {
			sig, err := kp.Sign(nonce)
			if err != nil {
				return nil, fmt.Errorf("signing NATS nonce: %w", err)
			}
			return sig, nil
		}),
	}

	if strings.HasPrefix(cfg.URL, "tls://") {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, nats.Secure(tlsCfg))
	}

	return opts, nil
}

// buildTLSConfig constructs a tls.Config from the PEM material in cfg.
func buildTLSConfig(cfg Config) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cfg.TLSCA) {
		return nil, fmt.Errorf("parsing NATS CA certificate")
	}

	clientCert, err := tls.X509KeyPair(cfg.TLSCert, cfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("parsing NATS client certificate: %w", err)
	}

	return &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// JetStream returns a JetStream context from an existing connection.
func JetStream(conn *nats.Conn) (nats.JetStreamContext, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("creating jetstream context: %w", err)
	}
	return js, nil
}

// EnsureStream creates the JetStream stream named name with the given subjects
// if it does not already exist. Safe to call on every service startup.
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
