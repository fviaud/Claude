package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestConnectMissingURL(t *testing.T) {
	_, err := Connect(context.Background(), "")
	if !errors.Is(err, ErrMissingURL) {
		t.Fatalf("Connect(\"\") error = %v, want ErrMissingURL", err)
	}
}

func TestConnectInvalidURL(t *testing.T) {
	if _, err := Connect(context.Background(), "not a url ::"); err == nil {
		t.Fatal("Connect with invalid URL: want error, got nil")
	}
}

// TestConnectIntegration needs a running PostgreSQL server.
// Run it with: TEST_DATABASE_URL=postgres://... go test ./internal/database
func TestConnectIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	version, err := ServerVersion(ctx, pool)
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if !strings.HasPrefix(version, "PostgreSQL") {
		t.Errorf("ServerVersion = %q, want prefix \"PostgreSQL\"", version)
	}
}
