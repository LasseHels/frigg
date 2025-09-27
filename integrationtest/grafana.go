package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const grafanaDefaultPort = 3000

type Grafana struct {
	container testcontainers.Container
	host      string
}

func NewGrafana(t *testing.T, logConsumers ...testcontainers.LogConsumer) *Grafana {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "grafana/grafana:12.0.0",
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

// CreateAPIKey creates a Grafana API key with admin permissions using the service account API.
func (g *Grafana) CreateAPIKey(t *testing.T, name string) string {
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
	req.SetBasicAuth("admin", "admin")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var serviceAccountResult struct {
		ID int64 `json:"id"`
	}

	err = json.NewDecoder(resp.Body).Decode(&serviceAccountResult)
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
	req.SetBasicAuth("admin", "admin")

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var tokenResult struct {
		Key string `json:"key"`
	}

	err = json.NewDecoder(resp.Body).Decode(&tokenResult)
	require.NoError(t, err)

	return tokenResult.Key
}

// CreateDashboard in Grafana using the traditional HTTP API.
func (g *Grafana) CreateDashboard(t *testing.T, apiKey, name string) {
	t.Helper()
	// https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#create-dashboard.
	url := fmt.Sprintf("http://%s/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards", g.host)

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

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
}

func (g *Grafana) AssertDashboardExists(t assert.TestingT, apiKey, name string) {
	g.assertGetDashboardStatusCode(t, apiKey, name, http.StatusOK)
}

func (g *Grafana) AssertDashboardDoesNotExist(t assert.TestingT, apiKey, name string) {
	g.assertGetDashboardStatusCode(t, apiKey, name, http.StatusNotFound)
}

func (g *Grafana) assertGetDashboardStatusCode(t assert.TestingT, apiKey, name string, expectedStatusCode int) {
	statusCode := g.getDashboard(t, apiKey, name)
	assert.Equal(
		t,
		expectedStatusCode,
		statusCode,
		"expected code %d when retrieving dashboard with name %q",
		expectedStatusCode,
		name,
	)
}

// getDashboard by name using the traditional HTTP API.
func (g *Grafana) getDashboard(t assert.TestingT, apiKey, name string) int {
	// https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#get-dashboard.
	url := fmt.Sprintf(
		"http://%s/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/%s",
		g.host,
		name,
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	assert.NoError(t, err) //nolint:testifylint

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(t, err) //nolint:testifylint
	defer func() {
		assert.NoError(t, resp.Body.Close())
	}()

	return resp.StatusCode
}
