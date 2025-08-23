package main

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
	"golang.org/x/sync/errgroup"

	"github.com/LasseHels/frigg/integrationtest"
	"github.com/LasseHels/frigg/pkg/grafana"
	"github.com/LasseHels/frigg/pkg/loki"
)

func TestFriggIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	var out bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	// Schedule a cancellation to ensure that the context is cancelled even if the test fails before the context is
	// cancelled manually.
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	release = "integration-test"

	eg.Go(func() error {
		err := run(ctx, "testdata/integration_config.yaml", "testdata/integration_secrets.yaml", &out)
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

	logs := out.String()
	assert.Contains(t, logs, "Loading configuration file from path testdata/integration_config.yaml\n")
	assert.Contains(t, logs, "Loading secrets file from path testdata/integration_secrets.yaml\n")
	assert.Contains(t, logs, `"msg":"Registered route","release":"integration-test","path":"/health","methods":["GET"]`)
	assert.Contains(t, logs, `"msg":"Registered route","release":"integration-test","path":"/metrics","methods":["GET"]`)

	t.Run("shuts down gracefully", func(t *testing.T) {
		cancel()
		err := eg.Wait()
		require.NoError(t, err)
	})
}

func TestDashboardPruning(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start Loki container
	lokiContainer := integrationtest.NewLoki(ctx)
	defer func() {
		_ = lokiContainer.Stop()
	}()

	// Start Grafana container  
	grafanaContainer := integrationtest.NewGrafana(ctx)
	defer func() {
		_ = grafanaContainer.Stop()
	}()

	// Create Grafana API key
	apiKey, err := grafanaContainer.CreateAPIKey(ctx, "test-key")
	require.NoError(t, err)

	// Create test dashboards
	dashboards := []struct {
		uid   string
		title string
	}{
		{"unused-dashboard", "Unused Dashboard"},
		{"used-dashboard", "Used Dashboard"},
		{"ignored-user-dashboard", "Ignored User Dashboard"},
	}

	for _, dashboard := range dashboards {
		err := grafanaContainer.CreateDashboard(ctx, apiKey, dashboard.uid, dashboard.title)
		require.NoError(t, err, "Failed to create dashboard %s", dashboard.uid)
	}

	// Verify dashboards were created
	for _, dashboard := range dashboards {
		exists, err := grafanaContainer.DashboardExists(ctx, apiKey, dashboard.uid)
		require.NoError(t, err)
		require.True(t, exists, "Dashboard %s should exist", dashboard.uid)
	}

	// Write logs to Loki to simulate dashboard reads
	now := time.Now()
	recentTime := now.Add(-5 * time.Minute) // Within 10-minute prune period
	oldTime := now.Add(-15 * time.Minute)   // Outside 10-minute prune period

	logs := []integrationtest.LogEntry{
		// Dashboard read by normal user within prune period (should not be deleted)
		{
			Timestamp: recentTime,
			Message:   `logger=context traceID=98851e40b0d1c0ea804c599bad281f71 userId=123 orgId=1 uname=john.doe@example.com t=2025-08-17T11:33:40.776603819Z level=info msg="Request Completed" method=GET path=/api/dashboards/uid/used-dashboard status=200 remote_addr=1.2.3.4 time_ms=80 duration=80.180134ms size=26213 referer="https://grafana.example.com/d/used-dashboard/used-dashboard" db_call_count=15 handler=/api/dashboards/uid/:uid status_source=server`,
			Labels: map[string]string{
				"app":     "grafana",
				"level":   "info",
				"path":    "/api/dashboards/uid/used-dashboard",
				"uname":   "john.doe@example.com",
				"handler": "/api/dashboards/uid/:uid",
			},
		},
		// Dashboard read by ignored user within prune period (should be deleted)
		{
			Timestamp: recentTime,
			Message:   `logger=context traceID=98851e40b0d1c0ea804c599bad281f72 userId=456 orgId=1 uname=admin t=2025-08-17T11:33:40.776603819Z level=info msg="Request Completed" method=GET path=/api/dashboards/uid/ignored-user-dashboard status=200 remote_addr=1.2.3.4 time_ms=80 duration=80.180134ms size=26213 referer="https://grafana.example.com/d/ignored-user-dashboard/ignored-user-dashboard" db_call_count=15 handler=/api/dashboards/uid/:uid status_source=server`,
			Labels: map[string]string{
				"app":     "grafana",
				"level":   "info",
				"path":    "/api/dashboards/uid/ignored-user-dashboard",
				"uname":   "admin",
				"handler": "/api/dashboards/uid/:uid",
			},
		},
		// Dashboard not read within prune period (should be deleted)
		{
			Timestamp: oldTime,
			Message:   `logger=context traceID=98851e40b0d1c0ea804c599bad281f73 userId=789 orgId=1 uname=jane.doe@example.com t=2025-08-17T11:33:40.776603819Z level=info msg="Request Completed" method=GET path=/api/dashboards/uid/unused-dashboard status=200 remote_addr=1.2.3.4 time_ms=80 duration=80.180134ms size=26213 referer="https://grafana.example.com/d/unused-dashboard/unused-dashboard" db_call_count=15 handler=/api/dashboards/uid/:uid status_source=server`,
			Labels: map[string]string{
				"app":     "grafana",
				"level":   "info",
				"path":    "/api/dashboards/uid/unused-dashboard",
				"uname":   "jane.doe@example.com",
				"handler": "/api/dashboards/uid/:uid",
			},
		},
	}

	err = lokiContainer.PushLogs(ctx, logs)
	require.NoError(t, err)

	// Wait for logs to be ingested
	time.Sleep(3 * time.Second)

	// Create clients
	lokiClient := loki.NewClient(loki.ClientOptions{
		Endpoint:   fmt.Sprintf("http://%s", lokiContainer.Host()),
		HTTPClient: http.DefaultClient,
		Logger:     nil,
	})

	testGrafanaClient := &testGrafanaClient{
		grafana:    grafanaContainer,
		apiKey:     apiKey,
		lokiClient: lokiClient,
	}

	// Create dashboard pruner with short period and high interval
	var logBuffer bytes.Buffer
	pruner := grafana.NewDashboardPruner(&grafana.NewDashboardPrunerOptions{
		Grafana:      testGrafanaClient,
		Logger:       nil, // We'll capture logs differently for this test
		Interval:     time.Hour, // High interval to ensure only one prune
		IgnoredUsers: []string{"admin"},
		Period:       10 * time.Minute,
		Labels:       map[string]string{"app": "grafana"},
		Dry:          false, // Actually delete dashboards
	})

	// Run prune once
	err = pruner.Prune(ctx)
	if err != nil {
		t.Logf("Logs from test:\n%s", logBuffer.String())
		require.NoError(t, err, "Dashboard pruning failed")
	}

	// Verify expected results
	// used-dashboard should still exist (read by normal user within period)
	exists, err := grafanaContainer.DashboardExists(ctx, apiKey, "used-dashboard")
	require.NoError(t, err)
	assert.True(t, exists, "used-dashboard should not be deleted")

	// unused-dashboard should be deleted (not read within period)
	exists, err = grafanaContainer.DashboardExists(ctx, apiKey, "unused-dashboard")
	require.NoError(t, err)
	assert.False(t, exists, "unused-dashboard should be deleted")

	// ignored-user-dashboard should be deleted (only read by ignored user)
	exists, err = grafanaContainer.DashboardExists(ctx, apiKey, "ignored-user-dashboard")
	require.NoError(t, err)
	assert.False(t, exists, "ignored-user-dashboard should be deleted")

	// If we got here without failing, don't output logs (test passed)
}

// testGrafanaClient implements the grafana.grafanaClient interface for integration testing
type testGrafanaClient struct {
	grafana    *integrationtest.Grafana
	apiKey     string
	lokiClient *loki.Client
}

// AllDashboards implements the grafanaClient interface by listing dashboards via HTTP API.
func (c *testGrafanaClient) AllDashboards(ctx context.Context) ([]grafana.Dashboard, error) {
	// Use Grafana search API to list dashboards
	url := fmt.Sprintf("http://%s/api/search?type=dash-db", c.grafana.Host())
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code listing dashboards: %d", resp.StatusCode)
	}
	
	var dashboards []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&dashboards); err != nil {
		return nil, err
	}
	
	var result []grafana.Dashboard
	for _, dash := range dashboards {
		uid, _ := dash["uid"].(string)
		title, _ := dash["title"].(string)
		
		// Create a simple spec with just the title
		spec, _ := json.Marshal(map[string]interface{}{
			"title": title,
		})
		
		result = append(result, grafana.Dashboard{
			Name:              title,
			Namespace:         "default",
			UID:               uid,
			CreationTimestamp: time.Now(),
			Spec:              spec,
		})
	}
	
	return result, nil
}

// DeleteDashboard implements the grafanaClient interface.
func (c *testGrafanaClient) DeleteDashboard(ctx context.Context, uid string) error {
	return c.grafana.DeleteDashboard(ctx, c.apiKey, uid)
}

// UsedDashboards implements the grafanaClient interface by querying Loki.
func (c *testGrafanaClient) UsedDashboards(
	ctx context.Context,
	labels map[string]string,
	r time.Duration,
	opts grafana.UsedDashboardsOptions,
) ([]grafana.DashboardReads, error) {
	// Build label selector
	var labelParts []string
	for k, v := range labels {
		labelParts = append(labelParts, fmt.Sprintf(`%s=%q`, k, v))
	}
	labelStr := "{" + strings.Join(labelParts, ", ") + "}"
	
	query := fmt.Sprintf(`%s
|= "/api/dashboards/uid/"
|= "Request Completed"
| logfmt
| handler = "/api/dashboards/uid/:uid"`, labelStr)

	end := time.Now().UTC()
	start := end.Add(-r)
	
	logs, err := c.lokiClient.QueryRange(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	
	// Process logs to extract dashboard usage
	ignoredUsers := make(map[string]struct{})
	for _, user := range opts.IgnoredUsers {
		ignoredUsers[user] = struct{}{}
	}
	
	readsByUID := make(map[string]map[string]struct{})
	readCounts := make(map[string]int)
	
	for _, log := range logs {
		stream := log.Stream()
		
		path, ok := stream["path"]
		if !ok {
			continue
		}
		
		// Extract dashboard UID from path
		pathParts := strings.Split(path, "/")
		if len(pathParts) < 5 || pathParts[3] != "uid" {
			continue
		}
		dashboardUID := pathParts[4]
		
		user, ok := stream["uname"]
		if !ok {
			continue
		}
		
		if _, ignored := ignoredUsers[user]; ignored {
			continue
		}
		
		readCounts[dashboardUID]++
		
		if _, exists := readsByUID[dashboardUID]; !exists {
			readsByUID[dashboardUID] = make(map[string]struct{})
		}
		readsByUID[dashboardUID][user] = struct{}{}
	}
	
	var result []grafana.DashboardReads
	for uid, users := range readsByUID {
		// Create DashboardReads using the constructor function
		dashboardReads := grafana.NewDashboardReads(uid, readCounts[uid], len(users))
		result = append(result, dashboardReads)
	}
	
	return result, nil
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
		_ = resp.Body.Close()
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "URL %q returned incorrect status code", url)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")
	return string(body)
}
