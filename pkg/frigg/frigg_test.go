package frigg_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/LasseHels/frigg/pkg/frigg"
	"github.com/LasseHels/frigg/pkg/grafana"
	"github.com/LasseHels/frigg/pkg/loki"
	"github.com/LasseHels/frigg/pkg/server"
)

func TestFrigg_Start_HasCorrectStructure(t *testing.T) {
	t.Parallel()

	// Create test config with minimal valid settings
	config := &frigg.Config{
		Server: server.Config{
			Host: "localhost",
			Port: 0, // Use any available port
		},
		Loki: loki.Config{
			Endpoint: "http://loki.example.com",
		},
		Grafana: grafana.Config{
			Endpoint: "http://grafana.example.com",
		},
		Prune: grafana.PruneConfig{
			Dry:      true,
			Interval: 1 * time.Hour, // Long interval to avoid immediate execution
			Period:   1 * time.Hour,
			Labels:   map[string]string{"app": "grafana"},
		},
	}

	// Create test secrets
	secrets := &frigg.Secrets{
		Grafana: grafana.Secrets{
			Token: "test-token",
		},
	}

	// Create logger that discards output to avoid noise
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create registry
	registry := prometheus.NewRegistry()

	// Initialize Frigg
	f := config.Initialise(logger, registry, secrets)
	assert.NotNil(t, f)

	// Test that the Frigg instance was created successfully
	// We can't easily test the Start method without mocking HTTP calls,
	// but we can verify that the initialization completed successfully
}

func TestConfig_Initialise_CreatesPruner(t *testing.T) {
	t.Parallel()

	// Create test config
	config := &frigg.Config{
		Server: server.Config{
			Host: "localhost",
			Port: 8080,
		},
		Loki: loki.Config{
			Endpoint: "http://loki.example.com",
		},
		Grafana: grafana.Config{
			Endpoint: "http://grafana.example.com",
		},
		Prune: grafana.PruneConfig{
			Dry:          true,
			Interval:     10 * time.Minute,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		},
	}

	// Create test secrets
	secrets := &frigg.Secrets{
		Grafana: grafana.Secrets{
			Token: "test-token",
		},
	}

	// Create logger
	logger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create registry
	registry := prometheus.NewRegistry()

	// Initialize should not panic and should return a Frigg instance
	f := config.Initialise(logger, registry, secrets)
	assert.NotNil(t, f)
}

func TestConfig_Initialise_PanicsOnInvalidGrafanaURL(t *testing.T) {
	t.Parallel()

	// Create config with invalid Grafana URL (this should not happen in practice due to validation)
	config := &frigg.Config{
		Server: server.Config{
			Host: "localhost",
			Port: 8080,
		},
		Loki: loki.Config{
			Endpoint: "http://loki.example.com",
		},
		Grafana: grafana.Config{
			Endpoint: ":", // Invalid URL
		},
		Prune: grafana.PruneConfig{
			Dry:      true,
			Interval: 10 * time.Minute,
			Period:   24 * time.Hour,
			Labels:   map[string]string{"app": "grafana"},
		},
	}

	secrets := &frigg.Secrets{
		Grafana: grafana.Secrets{
			Token: "test-token",
		},
	}

	logger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
	registry := prometheus.NewRegistry()

	// Should panic due to invalid URL
	assert.Panics(t, func() {
		config.Initialise(logger, registry, secrets)
	})
}