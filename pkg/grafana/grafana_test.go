package grafana_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/pkg/grafana"
	"github.com/LasseHels/frigg/pkg/loki"
)

type mockClient struct {
	logs []loki.Log
	err  error
}

func (m *mockClient) QueryRange(_ context.Context, _ string, _, _ time.Time) ([]loki.Log, error) {
	return m.logs, m.err
}

func TestClient_UsedDashboards(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mockLogs        []loki.Log
		mockErr         error
		chunkSize       time.Duration
		lowerThreshold  int
		labels          map[string]string
		expectedErrText string
	}{
		"empty labels": {
			mockLogs:        nil,
			mockErr:         nil,
			lowerThreshold:  10,
			labels:          map[string]string{},
			expectedErrText: "labels must not be empty",
		},
		"negative chunk size": {
			mockLogs:        nil,
			mockErr:         nil,
			chunkSize:       -1 * time.Hour,
			lowerThreshold:  10,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: "invalid options: chunk size must be zero or greater, got -1h0m0s",
		},
		"negative lower threshold": {
			mockLogs:        nil,
			mockErr:         nil,
			lowerThreshold:  -5,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: "invalid options: lower threshold must be zero or greater, got -5",
		},
		"client query error": {
			mockLogs:        nil,
			mockErr:         errors.New("connection refused"),
			lowerThreshold:  10,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: "querying loki: connection refused",
		},
		"not enough logs": {
			mockLogs: []loki.Log{
				loki.NewLog(
					time.Now(),
					"log message",
					map[string]string{
						"path":  "/api/dashboards/uid/dashboard1",
						"uname": "user1",
					},
				),
			},
			mockErr:         nil,
			lowerThreshold:  10,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: "found fewer logs (1) than the lower threshold (10)",
		},
		"missing path in stream labels": {
			mockLogs: []loki.Log{
				loki.NewLog(
					time.Now(),
					"log message",
					map[string]string{
						"uname": "user1",
					},
				),
			},
			mockErr:         nil,
			lowerThreshold:  1,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: "could not find path in stream labels: map[uname:user1]",
		},
		"invalid path format": {
			mockLogs: []loki.Log{
				loki.NewLog(
					time.Now(),
					"log message",
					map[string]string{
						"path":  "/invalid/path",
						"uname": "user1",
					},
				),
			},
			mockErr:         nil,
			lowerThreshold:  1,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: `unexpected path format: "/invalid/path", expected format /api/dashboards/uid/:uid`,
		},
		"missing uname in stream labels": {
			mockLogs: []loki.Log{
				loki.NewLog(
					time.Now(),
					"log message",
					map[string]string{
						"path": "/api/dashboards/uid/dashboard1",
					},
				),
			},
			mockErr:         nil,
			lowerThreshold:  1,
			labels:          map[string]string{"app": "grafana"},
			expectedErrText: "could not find uname in stream labels: map[path:/api/dashboards/uid/dashboard1]",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := &mockClient{
				logs: tc.mockLogs,
				err:  tc.mockErr,
			}

			g := grafana.NewClient(grafana.NewClientOptions{
				Logger: slog.Default(),
				Client: client,
			})

			opts := grafana.UsedDashboardsOptions{
				LowerThreshold: tc.lowerThreshold,
				ChunkSize:      tc.chunkSize,
			}

			reads, err := g.UsedDashboards(t.Context(), tc.labels, time.Hour, opts)
			require.EqualError(t, err, tc.expectedErrText)
			assert.Nil(t, reads)
		})
	}

	t.Run("basic successful case", func(t *testing.T) {
		t.Parallel()

		logs := []loki.Log{
			loki.NewLog(
				time.Now(),
				"log message 1",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard1",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 2",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard1",
					"uname": "user2",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 3",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard2",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 4",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard1",
					"uname": "user1",
				},
			),
		}

		client := &mockClient{
			logs: logs,
			err:  nil,
		}

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
		})

		opts := grafana.UsedDashboardsOptions{
			LowerThreshold: 1,
		}

		results, err := g.UsedDashboards(t.Context(), map[string]string{"app": "grafana"}, time.Hour, opts)
		require.NoError(t, err)
		require.Len(t, results, 2)

		assert.Equal(t, "dashboard1", results[0].UID())
		assert.Equal(t, 3, results[0].Reads())
		assert.Equal(t, 2, results[0].Users())

		assert.Equal(t, "dashboard2", results[1].UID())
		assert.Equal(t, 1, results[1].Reads())
		assert.Equal(t, 1, results[1].Users())
	})

	t.Run("ignores specified users", func(t *testing.T) {
		t.Parallel()

		logs := []loki.Log{
			loki.NewLog(
				time.Now(),
				"log message 1",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard1",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 2",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard1",
					"uname": "ignoredUser",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 3",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard2",
					"uname": "ignoredUser",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 4",
				map[string]string{
					"path":  "/api/dashboards/uid/dashboard3",
					"uname": "user2",
				},
			),
		}

		client := &mockClient{
			logs: logs,
			err:  nil,
		}

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
		})

		opts := grafana.UsedDashboardsOptions{
			LowerThreshold: 1,
			IgnoredUsers:   []string{"ignoredUser"},
		}

		results, err := g.UsedDashboards(t.Context(), map[string]string{"app": "grafana"}, time.Hour, opts)
		require.NoError(t, err)
		require.Len(t, results, 2) // dashboard2 should be excluded as it's only accessed by ignored user.

		assert.Equal(t, "dashboard1", results[0].UID())
		assert.Equal(t, 1, results[0].Reads()) // Only 1 read from non-ignored user.
		assert.Equal(t, 1, results[0].Users()) // Only 1 unique non-ignored user.

		assert.Equal(t, "dashboard3", results[1].UID())
		assert.Equal(t, 1, results[1].Reads())
		assert.Equal(t, 1, results[1].Users())
	})

	t.Run("with multiple chunks", func(t *testing.T) {
		t.Parallel()

		client := &mockClient{
			logs: []loki.Log{
				loki.NewLog(
					time.Now(),
					"log message 1",
					map[string]string{
						"path":  "/api/dashboards/uid/dashboard1",
						"uname": "user1",
					},
				),
				loki.NewLog(
					time.Now(),
					"log message 2",
					map[string]string{
						"path":  "/api/dashboards/uid/dashboard2",
						"uname": "user2",
					},
				),
			},
			err: nil,
		}

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
		})

		opts := grafana.UsedDashboardsOptions{
			LowerThreshold: 1,
			ChunkSize:      time.Minute, // Small chunk size to force multiple chunks.
		}

		// Use a 5-minute duration to ensure multiple chunks are created.
		results, err := g.UsedDashboards(t.Context(), map[string]string{"app": "grafana"}, 5*time.Minute, opts)
		require.NoError(t, err)
		require.Len(t, results, 2)

		// In our mock, each chunk returns the same 2 logs, so with 5 chunks:
		// - dashboard1 should have 5 reads from user1.
		// - dashboard2 should have 5 reads from user2.
		assert.Equal(t, "dashboard1", results[0].UID())
		assert.Equal(t, 5, results[0].Reads())
		assert.Equal(t, 1, results[0].Users())

		assert.Equal(t, "dashboard2", results[1].UID())
		assert.Equal(t, 5, results[1].Reads())
		assert.Equal(t, 1, results[1].Users())
	})
}

func TestClient_AllDashboards(t *testing.T) {
	t.Parallel()

	t.Run("non-200 response", func(t *testing.T) {
		t.Parallel()

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			assert.Equal(t, "/api/search", r.URL.Path)
			assert.Equal(t, "Bearer abc123", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			assert.Equal(t, "500", r.URL.Query().Get("limit"))
			assert.Equal(t, "1", r.URL.Query().Get("page"))

			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("the server is down"))
		}))
		defer server.Close()

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})

		dashboards, err := g.AllDashboards(t.Context())
		require.EqualError(t, err, "getting dashboards page: unexpected status code: 500, body: the server is down")
		assert.Nil(t, dashboards)
		assert.Equal(t, 1, requestCount)
	})

	t.Run("empty response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		}))
		defer server.Close()

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})

		dashboards, err := g.AllDashboards(t.Context())
		require.NoError(t, err)
		assert.Empty(t, dashboards)
	})

	t.Run("invalid json response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("!!!"))
		}))
		defer server.Close()

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})

		dashboards, err := g.AllDashboards(t.Context())
		require.EqualError(
			t,
			err,
			`getting dashboards page: decoding response: invalid character '!' looking for beginning of value`,
		)
		assert.Nil(t, dashboards)
	})

	t.Run("successful single page", func(t *testing.T) {
		t.Parallel()

		expectedDashboards := []grafana.Dashboard{
			{
				ID:    1,
				UID:   "dashboard1",
				Title: "Dashboard 1",
				URL:   "/d/dashboard1/dashboard-1",
				URI:   "db/dashboard-1",
				Type:  "dash-db",
			},
			{
				ID:    2,
				UID:   "dashboard2",
				Title: "Dashboard 2",
				URL:   "/d/dashboard2/dashboard-2",
				URI:   "db/dashboard-2",
				Type:  "dash-db",
			},
		}

		dashboardsJSON, err := json.Marshal(expectedDashboards)
		require.NoError(t, err)

		var requestCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dashboardsJSON)
		}))
		defer server.Close()

		g := grafana.NewClient(grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})

		dashboards, err := g.AllDashboards(t.Context())
		require.NoError(t, err)
		require.Len(t, dashboards, 2)
		assert.Equal(t, expectedDashboards, dashboards)
		assert.Equal(t, 1, requestCount)
	})
}

func mustParseURL(t *testing.T, input string) url.URL {
	t.Helper()
	parsed, err := url.Parse(input)
	if err != nil {
		require.NoError(t, err, "failed to parse URL: %s", input)
	}
	return *parsed
}
