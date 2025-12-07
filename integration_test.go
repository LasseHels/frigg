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
	"sync"
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

	var out buffer

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
			env.grafana.AssertDashboardExists(collect, env.apiKey, "default", "useddashboardapi")
			env.grafana.AssertDashboardDoesNotExist(collect, env.apiKey, "default", "ignoreduserdashboard")
			env.grafana.AssertDashboardDoesNotExist(collect, env.purpleKey, env.purpleNamespace, "purpleunuseddashboard")
		}, time.Second*10, time.Millisecond*100)

		requests := env.github.Requests()

		expectedKeys := []string{
			"GET /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/default/ignoreduserdashboard.json",
			"PUT /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/default/ignoreduserdashboard.json",
			"GET /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/default/unuseddashboard.json",
			"PUT /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/default/unuseddashboard.json",
			"GET /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/org-2/purpleunuseddashboard.json",
			"PUT /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/org-2/purpleunuseddashboard.json",
		}
		assertEqualKeys(t, expectedKeys, requests)

		expectedBackups := map[string]string{
			"default/ignoreduserdashboard": `{"message":"Back up deleted Grafana dashboard default/ignoreduserdashboard",` +
				`"content":"eyJlZGl0YWJsZSI6ZmFsc2UsInNjaGVtYVZlcnNpb24iOjQyLCJ0aW1lIjp7ImZyb20iOiJub3ctNmgiLCJ0byI6I` +
				`m5vdyJ9LCJ0aW1lcGlja2VyIjp7fSwidGltZXpvbmUiOiJicm93c2VyIiwidGl0bGUiOiJpZ25vcmVkdXNlcmRhc2hib2FyZCJ9"` +
				`,"branch":"main"}`,
			"default/unuseddashboard": `{"message":"Back up deleted Grafana dashboard default/unuseddashboard",` +
				`"content":"eyJlZGl0YWJsZSI6ZmFsc2UsInNjaGVtYVZlcnNpb24iOjQyLCJ0aW1lIjp7ImZyb20iOiJub3ctNmgiLCJ0byI6I` +
				`m5vdyJ9LCJ0aW1lcGlja2VyIjp7fSwidGltZXpvbmUiOiJicm93c2VyIiwidGl0bGUiOiJ1bnVzZWRkYXNoYm9hcmQifQ=="` +
				`,"branch":"main"}`,
			"org-2/purpleunuseddashboard": `{"message":"Back up deleted Grafana dashboard org-2/purpleunuseddashboard",` +
				`"content":"eyJlZGl0YWJsZSI6ZmFsc2UsInNjaGVtYVZlcnNpb24iOjQyLCJ0aW1lIjp7ImZyb20iOiJub3ctNmgiLCJ0byI6I` +
				`m5vdyJ9LCJ0aW1lcGlja2VyIjp7fSwidGltZXpvbmUiOiJicm93c2VyIiwidGl0bGUiOiJwdXJwbGV1bnVzZWRkYXNoYm9hcmQifQ=="` +
				`,"branch":"main"}`,
		}

		for dashboardPath, expectedBody := range expectedBackups {
			key := "PUT /api/v3/repos/octocat/hello-world/contents/deleted-dashboards/" + dashboardPath + ".json"
			putRequests := requests[key]
			require.Len(t, putRequests, 1)

			body := readRequestBody(t, putRequests[0])
			assert.JSONEq(t, expectedBody, body)
		}
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
			`"qXQslCwCEssfGqKRa9patJDjtRbOf5XNGwOXXjdRnx0X","name":"ignoreduserdashboard","title":"ignoreduserdashboard",`+
			`"raw_json":"{\"editable\":false,\"schemaVersion\":42,\"time\":{\"from\":\"now-6h\",\"to\":\"now\"},\`+
			`"timepicker\":{},\"timezone\":\"browser\",\"title\":\"ignoreduserdashboard\"}"}`,
	)
	assert.Contains(
		t,
		logs,
		`"msg":"Deleted unused dashboard","release":"integration-test","dry":false,"namespace":"default","uid":`+
			`"mspoU2EJfAXM0lQ8bBBUha0AszqQEKUKMjzu4Gc3rnEX","name":"unuseddashboard","title":"unuseddashboard",`+
			`"raw_json":"{\"editable\":false,\"schemaVersion\":42,\"time\":{\"from\":\"now-6h\",\"to\":\"now\"},\`+
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
			`"title":"purpleunuseddashboard","raw_json":"{\"editable\":false,\"schemaVersion\":42,`+
			`\"time\":{\"from\":\"now-6h\",\"to\":\"now\"},\"timepicker\":{},\"timezone\":\"browser\",`+
			`\"title\":\"purpleunuseddashboard\"}"}`,
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
	github          *integrationtest.GitHub
}

func setup(t *testing.T) testEnvironment {
	t.Helper()

	now := time.Now().UTC()
	loki := integrationtest.NewLoki(t)
	github := integrationtest.NewGitHub(t)
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
	grafana.CreateDashboard(t, apiKey, "default", "useddashboardapi")
	grafana.CreateDashboard(t, apiKey, "default", "unuseddashboard")
	grafana.CreateDashboard(t, apiKey, "default", "ignoreduserdashboard")
	grafana.CreateDashboard(t, purpleKey, purpleNamespace, "purpleunuseddashboard")

	t.Setenv("GITHUB_API_URL", github.URL())

	// Frigg counts two different activities as a dashboard "view": an API request to get the dashboard and a user
	// opening the dashboard in Grafana's UI. Ensure that both types of activity are represented in the test.
	grafana.ViewDashboardInUI(t, apiKey, "default", "useddashboard")
	grafana.AssertDashboardExists(t, apiKey, "default", "useddashboardapi")
	// Create a log entry in Loki for the ignored user.
	grafana.ViewDashboardInUI(t, ignoredKey, "default", "ignoreduserdashboard")

	t.Setenv("LOKI_ENDPOINT", fmt.Sprintf("http://%s", loki.Host()))
	t.Setenv("GRAFANA_ENDPOINT", fmt.Sprintf("http://%s", grafana.Host()))

	return testEnvironment{
		secretsPath:     secretsPath,
		apiKey:          apiKey,
		purpleKey:       purpleKey,
		purpleNamespace: purpleNamespace,
		grafana:         grafana,
		github:          github,
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

func readRequestBody(t *testing.T, req *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return string(body)
}

func assertEqualKeys(t *testing.T, expected []string, actual map[string][]*http.Request) {
	t.Helper()
	var actualKeys []string
	for key := range actual {
		actualKeys = append(actualKeys, key)
	}
	assert.ElementsMatch(t, expected, actualKeys)
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

// buffer is a thread-safe bytes.Buffer.
type buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
