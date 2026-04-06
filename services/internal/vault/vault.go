// Package vault provides a thin wrapper around the HashiCorp Vault API client
// for secret retrieval and SIGHUP-triggered reload.
package vault

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	vaultapi "github.com/hashicorp/vault/api"
)

// Client wraps the Vault API client.
type Client struct {
	client *vaultapi.Client
}

// NewClient creates a Vault client pointed at addr, authenticates with token,
// and verifies the connection with a token self-lookup. Returns an error if the
// Vault is unreachable or the token is invalid.
func NewClient(addr, token string) (*Client, error) {
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr

	c, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating vault client: %w", err)
	}
	c.SetToken(token)

	if _, err := c.Auth().Token().LookupSelf(); err != nil {
		return nil, fmt.Errorf("vault token lookup failed: %w", err)
	}

	return &Client{client: c}, nil
}

// GetSecret retrieves a single string value from a KV v2 secret at path under
// the given key. path must include the "secret/data/" prefix, e.g.
// "secret/data/navi/prod/postgres".
func (c *Client) GetSecret(path, key string) (string, error) {
	secret, err := c.client.Logical().Read(path)
	if err != nil {
		return "", fmt.Errorf("reading vault path %q: %w", path, err)
	}
	if secret == nil {
		return "", fmt.Errorf("vault path %q not found", path)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected data format at vault path %q", path)
	}

	raw, ok := data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found at vault path %q", key, path)
	}

	val, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("key %q at vault path %q is not a string", key, path)
	}

	return val, nil
}

// Ping verifies the Vault connection is healthy by performing a token
// self-lookup. Returns a non-nil error if the Vault is unreachable or the
// token has expired.
func (c *Client) Ping(ctx context.Context) error {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if _, err := c.client.Auth().Token().LookupSelfWithContext(reqCtx); err != nil {
		return fmt.Errorf("vault ping: %w", err)
	}
	return nil
}

// RegisterSIGHUPReload starts a goroutine that calls reloadFn each time the
// process receives SIGHUP. The reloadFn is expected to handle its own error
// logging; errors are not propagated.
func RegisterSIGHUPReload(reloadFn func() error) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			_ = reloadFn()
		}
	}()
}
