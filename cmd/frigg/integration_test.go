package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"golang.org/x/sync/errgroup"

	"github.com/LasseHels/frigg/integrationtest"
)

func TestFriggIntegration(t *testing.T) {
	integrationtest.SkipIfShort(t)

	var out bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	// Schedule a cancellation to ensure that the context is cancelled even if the test fails before the context is
	// cancelled manually.
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	release = "integration-test"
	secretsPath, apiKey, grafana := setup(t)

	eg.Go(func() error {
		err := run(ctx, "testdata/integration_config.yaml", secretsPath, &out)
		assert.NoError(t, err)
		return err
	})

	serverURL := "http://localhost:8080"
	waitForServer(t, serverURL)

	t.Run("health endpoint returns healthy", func(t *testing.T) {
		body := assertOK(t, fmt.Sprintf("%s/health", serverURL))
		assert.Equal(t, "healthy", body, "Health endpoint returned incorrect body")
	})

	t.Run("metrics endpoint returns metrics", func(t *testing.T) {
		body := assertOK(t, fmt.Sprintf("%s/metrics", serverURL))
		assert.Contains(t, body, "go_", "Metrics should contain Go metrics")
		assert.Contains(t, body, "process_", "Metrics should contain process metrics")
	})

	t.Run("prunes dashboards", func(t *testing.T) {
		grafana.AssertDashboardDoesNotExist(t, apiKey, "unuseddashboard")
		grafana.AssertDashboardDoesNotExist(t, apiKey, "ignoreduserdashboard")
		grafana.AssertDashboardExists(t, apiKey, "useddashboard")
	})

	logs := out.String()
	assert.Contains(t, logs, "Loading configuration file from path testdata/integration_config.yaml\n")
	assert.Contains(t, logs, fmt.Sprintf("Loading secrets file from path %s\n", secretsPath))
	assert.Contains(t, logs, `"msg":"Registered route","release":"integration-test","path":"/health","methods":["GET"]`)
	assert.Contains(t, logs, `"msg":"Registered route","release":"integration-test","path":"/metrics","methods":["GET"]`)

	t.Run("shuts down gracefully", func(t *testing.T) {
		cancel()
		err := eg.Wait()
		require.NoError(t, err)
	})
}

func setup(t *testing.T) (string, string, *integrationtest.Grafana) {
	t.Helper()

	now := time.Now().UTC()
	loki := integrationtest.NewLoki(t)
	grafana := integrationtest.NewGrafana(
		t,
		integrationtest.NewLogger(t),
		&LokiLogConsumer{
			loki: loki,
			t:    t,
			timestamps: map[string]time.Time{
				"/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/useddashboard":        now.Add(-5 * time.Minute),
				"/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/ignoreduserdashboard": now.Add(-5 * time.Minute),
				"/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/unuseddashboard":      now.Add(-15 * time.Minute), //nolint:lll
			},
		},
	)

	apiKey := grafana.CreateAPIKey(t, "test-key")

	secrets := fmt.Sprintf(`
grafana:
  token: %s
`, apiKey)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	err := os.WriteFile(secretsPath, []byte(secrets), os.ModePerm)
	require.NoError(t, err)

	grafana.CreateDashboard(t, apiKey, "useddashboard")
	grafana.CreateDashboard(t, apiKey, "unuseddashboard")
	grafana.CreateDashboard(t, apiKey, "ignoreduserdashboard")

	grafana.AssertDashboardExists(t, apiKey, "useddashboard")

	t.Setenv("LOKI_ENDPOINT", fmt.Sprintf("http://%s", loki.Host()))
	t.Setenv("GRAFANA_ENDPOINT", fmt.Sprintf("http://%s", grafana.Host()))

	return secretsPath, apiKey, grafana
}

func waitForServer(t *testing.T, url string) {
	require.Eventually(t, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}

		_ = resp.Body.Close()
		return true
	}, 5*time.Second, 10*time.Millisecond, "Server did not start in time")
}

func assertOK(t *testing.T, url string) string {
	resp, err := http.Get(url)
	require.NoError(t, err, "Failed to make request to URL %q", url)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "URL %q returned incorrect status code", url)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")
	return string(body)
}

type LokiLogConsumer struct {
	loki       *integrationtest.Loki
	t          *testing.T
	timestamps map[string]time.Time
}

func (l *LokiLogConsumer) Accept(log testcontainers.Log) {
	timestamp := time.Now().UTC()

	for matcher, at := range l.timestamps {
		if strings.Contains(string(log.Content), matcher) {
			timestamp = at
			break
		}
	}

	lokiLogs := []integrationtest.LogEntry{
		{
			Timestamp: timestamp,
			Message:   string(log.Content),
			Labels: map[string]string{
				"app": "grafana",
				"env": "integration-test",
			},
		},
	}

	err := l.loki.PushLogs(l.t.Context(), lokiLogs)
	require.NoError(l.t, err)
}
