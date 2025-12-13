package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	grafanaDefaultPort = 3000
	// DefaultOrgID is the ID of the default organisation in Grafana.
	DefaultOrgID = 1
)

type Grafana struct {
	container testcontainers.Container
	host      string
}

func NewGrafana(t *testing.T, logConsumers ...testcontainers.LogConsumer) *Grafana {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "grafana/grafana:12.2.0",
		ExposedPorts: []string{fmt.Sprintf("%d", grafanaDefaultPort)},
		Env: map[string]string{
			"GF_SECURITY_ADMIN_PASSWORD": "admin",
			"GF_INSTALL_PLUGINS":         "",
			// Disable telemetry and analytics.
			"GF_ANALYTICS_REPORTING_ENABLED": "false",
			"GF_ANALYTICS_CHECK_FOR_UPDATES": "false",
			// Enable API key auth.
			"GF_AUTH_ANONYMOUS_ENABLED": "false",
			// https://grafana.com/docs/grafana/v12.0/setup-grafana/configure-grafana/#router_logging.
			"GF_SERVER_ROUTER_LOGGING": "true",
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      "testdata/provisioning/dashboards",
				ContainerFilePath: "/etc/grafana/provisioning/dashboards",
				FileMode:          0o755,
			},
		},
		WaitingFor: wait.ForAll(
			wait.ForExposedPort(),
			wait.ForHTTP("/api/health"),
		),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: logConsumers,
		},
	}

	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	g := &Grafana{
		container: container,
		host:      mustGetHost(t.Context(), container, grafanaDefaultPort),
	}

	t.Cleanup(func() {
		require.NoError(t, g.stop())
	})

	return g
}

func (g *Grafana) stop() error {
	timeout := time.Second * 5
	return g.container.Stop(context.Background(), &timeout)
}

func (g *Grafana) Host() string {
	return g.host
}

// CreateOrganisation creates a new organisation in Grafana and returns its ID.
func (g *Grafana) CreateOrganisation(t testing.TB, name string) int {
	t.Helper()

	// https://grafana.com/docs/grafana/v12.0/developers/http_api/org/#create-organization.
	url := fmt.Sprintf("http://%s/api/orgs", g.host)

	payload := []byte(fmt.Sprintf(`{"name": %q}`, name))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewBuffer(payload))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "admin")

	body := do(t, req, http.StatusOK)

	var result struct {
		OrgID int `json:"orgId"`
	}

	err = json.Unmarshal(body, &result)
	require.NoError(t, err)

	return result.OrgID
}

// CreateAPIKey creates a Grafana API key with admin permissions using the service account API.
func (g *Grafana) CreateAPIKey(t *testing.T, name string, orgID int) string {
	t.Helper()
	// https://grafana.com/docs/grafana/v12.0/developers/http_api/serviceaccount/#create-service-account.
	serviceAccountURL := fmt.Sprintf("http://%s/api/serviceaccounts", g.host)

	serviceAccountPayload := map[string]any{
		"name": name,
		"role": "Admin",
	}

	payloadBytes, err := json.Marshal(serviceAccountPayload)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, serviceAccountURL, bytes.NewBuffer(payloadBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	// https://grafana.com/docs/grafana/v12.0/developers/http_api/#x-grafana-org-id-header.
	req.Header.Set("X-Grafana-Org-Id", fmt.Sprintf("%d", orgID))
	req.SetBasicAuth("admin", "admin")

	body := do(t, req, http.StatusCreated)

	var serviceAccountResult struct {
		ID    int64 `json:"id"`
		OrgID int64 `json:"orgId"`
	}

	err = json.Unmarshal(body, &serviceAccountResult)
	require.NoError(t, err)

	// https://grafana.com/docs/grafana/v12.0/developers/http_api/serviceaccount/#create-service-account-tokens.
	tokenURL := fmt.Sprintf("http://%s/api/serviceaccounts/%d/tokens", g.host, serviceAccountResult.ID)

	tokenPayload := map[string]any{
		"name": name + "-token",
	}

	tokenPayloadBytes, err := json.Marshal(tokenPayload)
	require.NoError(t, err)

	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, tokenURL, bytes.NewBuffer(tokenPayloadBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grafana-Org-Id", fmt.Sprintf("%d", orgID))
	req.SetBasicAuth("admin", "admin")

	body = do(t, req, http.StatusOK)

	var tokenResult struct {
		Key string `json:"key"`
	}

	err = json.Unmarshal(body, &tokenResult)
	require.NoError(t, err)

	return tokenResult.Key
}

// CreateDashboard in Grafana using the traditional HTTP API.
func (g *Grafana) CreateDashboard(t *testing.T, apiKey, namespace, name string) {
	t.Helper()
	// https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#create-dashboard.
	url := fmt.Sprintf("http://%s/apis/dashboard.grafana.app/v1beta1/namespaces/%s/dashboards", g.host, namespace)

	payload := []byte(fmt.Sprintf(`
{
  "metadata": {
    "name": "%s"
  },
  "spec": {
    "editable": false,
    "schemaVersion": 41,
    "time": {
      "from": "now-6h",
      "to": "now"
    },
    "timepicker": {},
    "timezone": "browser",
    "title": "%s",
    "version": 0
  }
}
`, name, name))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewBuffer(payload))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	_ = do(t, req, http.StatusCreated)
}

// CreateDashboardWithTags creates a dashboard in Grafana with the specified tags.
func (g *Grafana) CreateDashboardWithTags(t *testing.T, apiKey, namespace, name string, tags []string) {
	t.Helper()

	tagsJSON := "[]"
	if len(tags) > 0 {
		var tagStrings []string
		for _, tag := range tags {
			tagStrings = append(tagStrings, fmt.Sprintf("%q", tag))
		}
		tagsJSON = fmt.Sprintf("[%s]", strings.Join(tagStrings, ","))
	}

	url := fmt.Sprintf("http://%s/apis/dashboard.grafana.app/v1beta1/namespaces/%s/dashboards", g.host, namespace)
	payload := []byte(fmt.Sprintf(`
{
  "metadata": {
    "name": %q
  },
  "spec": {
    "editable": false,
    "schemaVersion": 41,
    "time": {
      "from": "now-6h",
      "to": "now"
    },
    "timepicker": {},
    "timezone": "browser",
    "title": %q,
    "tags": %s,
    "version": 0
  }
}
`, name, name, tagsJSON))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewBuffer(payload))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	_ = do(t, req, http.StatusCreated)
}

func (g *Grafana) AssertDashboardExists(t assert.TestingT, apiKey, namespace, name string) {
	g.assertGetDashboardStatusCode(t, apiKey, namespace, name, http.StatusOK)
}

func (g *Grafana) AssertDashboardDoesNotExist(t assert.TestingT, apiKey, namespace, name string) {
	g.assertGetDashboardStatusCode(t, apiKey, namespace, name, http.StatusNotFound)
}

func (g *Grafana) assertGetDashboardStatusCode(
	t assert.TestingT,
	apiKey,
	namespace,
	name string,
	expectedStatusCode int,
) {
	statusCode := g.getDashboard(t, apiKey, namespace, name)
	assert.Equal(
		t,
		expectedStatusCode,
		statusCode,
		"expected code %d when retrieving dashboard with name %q in namespace %q",
		expectedStatusCode,
		name,
		namespace,
	)
}

// getDashboard by name using the traditional HTTP API.
func (g *Grafana) getDashboard(t assert.TestingT, apiKey, namespace, name string) int {
	// https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#get-dashboard.
	url := fmt.Sprintf(
		"http://%s/apis/dashboard.grafana.app/v1beta1/namespaces/%s/dashboards/%s",
		g.host,
		namespace,
		name,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	assert.NoError(t, err) //nolint:testifylint

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err) //nolint:testifylint
	defer func() {
		assert.NoError(t, resp.Body.Close())
	}()

	return resp.StatusCode
}

// ViewDashboardInUI simulates viewing a dashboard in the Grafana UI by calling the /dto endpoint.
// When a user views a dashboard in the Grafana web interface, the frontend JavaScript makes an API call
// to the /dto endpoint to fetch the dashboard data. This method simulates that API call.
func (g *Grafana) ViewDashboardInUI(t testing.TB, apiKey, namespace, name string) {
	url := fmt.Sprintf(
		"http://%s/apis/dashboard.grafana.app/v1beta1/namespaces/%s/dashboards/%s/dto",
		g.host,
		namespace,
		name,
	)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	assert.NoError(t, err) //nolint:testifylint
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	_ = do(t, req, http.StatusOK)
}

func do(t testing.TB, req *http.Request, expectedStatus int) []byte {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, resp.StatusCode, string(body))

	return body
}
