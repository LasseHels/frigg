package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const grafanaDefaultPort = 3000

type Grafana struct {
	container testcontainers.Container
	host      string
}

func NewGrafana(ctx context.Context) *Grafana {
	req := testcontainers.ContainerRequest{
		Image:        "grafana/grafana:11.3.0",
		ExposedPorts: []string{fmt.Sprintf("%d", grafanaDefaultPort)},
		Env: map[string]string{
			"GF_SECURITY_ADMIN_PASSWORD": "admin",
			"GF_INSTALL_PLUGINS":         "",
			// Disable telemetry and analytics
			"GF_ANALYTICS_REPORTING_ENABLED": "false",
			"GF_ANALYTICS_CHECK_FOR_UPDATES": "false",
			// Enable API key auth
			"GF_AUTH_ANONYMOUS_ENABLED": "false",
		},
		WaitingFor: wait.ForAll(
			wait.ForExposedPort(),
			wait.ForHTTP("/api/health"),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	return &Grafana{
		container: container,
		host:      mustGetHost(ctx, container, grafanaDefaultPort),
	}
}

func (g *Grafana) Stop() error {
	timeout := time.Millisecond
	return g.container.Stop(context.Background(), &timeout)
}

func (g *Grafana) Host() string {
	return g.host
}

// CreateAPIKey creates a Grafana API key with admin permissions.
func (g *Grafana) CreateAPIKey(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("http://%s/api/auth/keys", g.host)
	
	payload := map[string]interface{}{
		"name": name,
		"role": "Admin",
	}
	
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "admin")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code creating API key: %d", resp.StatusCode)
	}
	
	var result struct {
		Key string `json:"key"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	
	return result.Key, nil
}

// CreateDashboard creates a dashboard in Grafana using the traditional HTTP API.
func (g *Grafana) CreateDashboard(ctx context.Context, apiKey, uid, title string) error {
	url := fmt.Sprintf("http://%s/api/dashboards/db", g.host)
	
	dashboard := map[string]interface{}{
		"dashboard": map[string]interface{}{
			"uid":           uid,
			"title":         title,
			"tags":          []string{"test"},
			"timezone":      "browser",
			"schemaVersion": 41,
			"version":       1,
			"panels":        []interface{}{},
		},
		"overwrite": true,
	}
	
	payloadBytes, err := json.Marshal(dashboard)
	if err != nil {
		return err
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code creating dashboard: %d", resp.StatusCode)
	}
	
	return nil
}

// GetDashboard retrieves a dashboard by UID using the traditional HTTP API.
func (g *Grafana) GetDashboard(ctx context.Context, apiKey, uid string) (map[string]interface{}, error) {
	url := fmt.Sprintf("http://%s/api/dashboards/uid/%s", g.host, uid)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("dashboard not found")
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code getting dashboard: %d", resp.StatusCode)
	}
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result, nil
}

// DeleteDashboard deletes a dashboard by UID using the traditional HTTP API.
func (g *Grafana) DeleteDashboard(ctx context.Context, apiKey, uid string) error {
	url := fmt.Sprintf("http://%s/api/dashboards/uid/%s", g.host, uid)
	
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, http.NoBody)
	if err != nil {
		return err
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code deleting dashboard: %d", resp.StatusCode)
	}
	
	return nil
}

// DashboardExists checks if a dashboard exists by UID.
func (g *Grafana) DashboardExists(ctx context.Context, apiKey, uid string) (bool, error) {
	_, err := g.GetDashboard(ctx, apiKey, uid)
	if err != nil {
		if err.Error() == "dashboard not found" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}