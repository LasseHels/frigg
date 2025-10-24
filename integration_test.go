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
	env := setup(t)

	eg.Go(func() error {
		err := run(ctx, "testdata/integration_config.yaml", env.secretsPath, &out)
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
		assert.EventuallyWithT(t, func(collect *assert.CollectT) {
			env.grafana.AssertDashboardDoesNotExist(collect, env.apiKey, "default", "unuseddashboard")
			env.grafana.AssertDashboardExists(collect, env.apiKey, "default", "useddashboard")
			env.grafana.AssertDashboardDoesNotExist(collect, env.apiKey, "default", "ignoreduserdashboard")
			env.grafana.AssertDashboardDoesNotExist(collect, env.purpleKey, env.purpleNamespace, "purpleunuseddashboard")
		}, time.Second*10, time.Millisecond*100)
	})

	logs := out.String()
	assert.Contains(t, logs, "Loading configuration file from path testdata/integration_config.yaml\n")
	assert.Contains(t, logs, fmt.Sprintf("Loading secrets file from path %s\n", env.secretsPath))
	assert.Contains(t, logs, `"msg":"Registered route","release":"integration-test","path":"/health","methods":["GET"]`)
	assert.Contains(t, logs, `"msg":"Registered route","release":"integration-test","path":"/metrics","methods":["GET"]`)
	assert.Contains(
		t,
		logs,
		`"msg":"Deleted unused dashboard","release":"integration-test","dry":false,"namespace":"default","uid":`+
			`"qXQslCwCEssfGqKRa9patJDjtRbOf5XNGwOXXjdRnx0X","name":"ignoreduserdashboard",`+
			`"raw_json":"{\"editable\":false,\"schemaVersion\":41,\"time\":{\"from\":\"now-6h\",\"to\":\"now\"},\`+
			`"timepicker\":{},\"timezone\":\"browser\",\"title\":\"ignoreduserdashboard\"}"}`,
	)
	assert.Contains(
		t,
		logs,
		`"msg":"Deleted unused dashboard","release":"integration-test","dry":false,"namespace":"default","uid":`+
			`"mspoU2EJfAXM0lQ8bBBUha0AszqQEKUKMjzu4Gc3rnEX","name":"unuseddashboard",`+
			`"raw_json":"{\"editable\":false,\"schemaVersion\":41,\"time\":{\"from\":\"now-6h\",\"to\":\"now\"},\`+
			`"timepicker\":{},\"timezone\":\"browser\",\"title\":\"unuseddashboard\"}"}`,
	)
	assert.Contains(
		t,
		logs,
		`"msg":"Finished pruning Grafana dashboards","release":"integration-test","dry":false,"namespace":"default",`+
			`"deleted_count":2,"deleted_dashboards":"default/ignoreduserdashboard, default/unuseddashboard"}`,
	)
	assert.Contains(
		t,
		logs,
		`"msg":"Deleted unused dashboard","release":"integration-test","dry":false,"namespace":"org-2",`+
			`"uid":"JfROTruBNXvUjXF8aD455yc2sRiB397AL6Pscl8QChsX","name":"purpleunuseddashboard",`+
			`"raw_json":"{\"editable\":false,\"schemaVersion\":41,\"time\":{\"from\":\"now-6h\",\"to\":\"now\"},`+
			`\"timepicker\":{},\"timezone\":\"browser\",\"title\":\"purpleunuseddashboard\"}"}`,
	)
	assert.Contains(
		t,
		logs,
		`"msg":"Finished pruning Grafana dashboards","release":"integration-test","dry":false,"namespace":"org-2",`+
			`"deleted_count":1,"deleted_dashboards":"org-2/purpleunuseddashboard"}`,
	)

	t.Run("shuts down gracefully", func(t *testing.T) {
		cancel()
		err := eg.Wait()
		require.NoError(t, err)
	})
}

type testEnvironment struct {
	secretsPath     string
	apiKey          string
	purpleKey       string
	purpleNamespace string
	grafana         *integrationtest.Grafana
}

func setup(t *testing.T) testEnvironment {
	t.Helper()

	now := time.Now().UTC()
	loki := integrationtest.NewLoki(t)
	grafana := integrationtest.NewGrafana(
		t,
		&lokiLogConsumer{
			loki: loki,
			t:    t,
			timestamps: map[string]time.Time{
				"useddashboard":         now.Add(-5 * time.Minute),
				"ignoreduserdashboard":  now.Add(-5 * time.Minute),
				"unuseddashboard":       now.Add(-15 * time.Minute),
				"purpleunuseddashboard": now.Add(-15 * time.Minute),
			},
		},
	)

	orgID := grafana.CreateOrganisation(t, "purple")
	purpleNamespace := fmt.Sprintf("org-%d", orgID)
	apiKey := grafana.CreateAPIKey(t, "test-key", integrationtest.DefaultOrgID)
	ignoredKey := grafana.CreateAPIKey(t, "john-doe", integrationtest.DefaultOrgID)
	purpleKey := grafana.CreateAPIKey(t, "purple-key", orgID)

	secrets := fmt.Sprintf(`
grafana:
  tokens:
    default: %s
    %s: %s

backup:
  github:
    token: 'ghp_exampletoken123'
`, apiKey, purpleNamespace, purpleKey)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	err := os.WriteFile(secretsPath, []byte(secrets), os.ModePerm)
	require.NoError(t, err)

	grafana.CreateDashboard(t, apiKey, "default", "useddashboard")
	grafana.CreateDashboard(t, apiKey, "default", "unuseddashboard")
	grafana.CreateDashboard(t, apiKey, "default", "ignoreduserdashboard")
	grafana.CreateDashboard(t, purpleKey, purpleNamespace, "purpleunuseddashboard")

	// Assert that the dashboard exists to create a read log entry in Loki.
	grafana.AssertDashboardExists(t, apiKey, "default", "useddashboard")
	// Create a log entry in Loki for the ignored user.
	grafana.AssertDashboardExists(t, ignoredKey, "default", "ignoreduserdashboard")

	t.Setenv("LOKI_ENDPOINT", fmt.Sprintf("http://%s", loki.Host()))
	t.Setenv("GRAFANA_ENDPOINT", fmt.Sprintf("http://%s", grafana.Host()))

	return testEnvironment{
		secretsPath:     secretsPath,
		apiKey:          apiKey,
		purpleKey:       purpleKey,
		purpleNamespace: purpleNamespace,
		grafana:         grafana,
	}
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

type lokiLogConsumer struct {
	loki       *integrationtest.Loki
	t          *testing.T
	timestamps map[string]time.Time
}

func (l *lokiLogConsumer) Accept(log testcontainers.Log) {
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

	// Deliberately don't use l.t.Context() as that often leads to "context cancelled" errors as logs are attempted
	// pushed at the end of a test. Pushing logs to a local Loki instance should not take more than a second.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := l.loki.PushLogs(ctx, lokiLogs)
	require.NoError(l.t, err)
}
