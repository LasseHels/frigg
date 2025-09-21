package grafana_test

import (
	"context"
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

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("errors if token is empty", func(t *testing.T) {
		t.Parallel()

		_, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     nil,
			Client:     nil,
			HTTPClient: nil,
			Endpoint:   mustParseURL(t, "https://grafana.example.com"),
			Token:      "",
		})
		require.EqualError(t, err, "validating Grafana client options: token must not be empty")
	})
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
						"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
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
			mockErr:        nil,
			lowerThreshold: 1,
			labels:         map[string]string{"app": "grafana"},
			expectedErrText: `extracting variables from path "/invalid/path": expected part count 8 but got 3: ` +
				`unexpected path format: "/invalid/path", expected format ` +
				`"/apis/dashboard.grafana.app/v1beta1/namespaces/:namespace/dashboards/:uid"`,
		},
		"missing uname in stream labels": {
			mockLogs: []loki.Log{
				loki.NewLog(
					time.Now(),
					"log message",
					map[string]string{
						"path": "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
					},
				),
			},
			mockErr:        nil,
			lowerThreshold: 1,
			labels:         map[string]string{"app": "grafana"},
			expectedErrText: "could not find uname in stream labels: " +
				"map[path:/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1]",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := &mockClient{
				logs: tc.mockLogs,
				err:  tc.mockErr,
			}

			g, err := grafana.NewClient(&grafana.NewClientOptions{
				Logger: slog.Default(),
				Client: client,
				Token:  "banana",
			})
			require.NoError(t, err)

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
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 2",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
					"uname": "user2",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 3",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard2",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 4",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
					"uname": "user1",
				},
			),
		}

		client := &mockClient{
			logs: logs,
			err:  nil,
		}

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
			Token:  "apple",
		})
		require.NoError(t, err)

		opts := grafana.UsedDashboardsOptions{
			LowerThreshold: 1,
		}

		results, err := g.UsedDashboards(t.Context(), map[string]string{"app": "grafana"}, time.Hour, opts)
		require.NoError(t, err)
		require.Len(t, results, 2)

		assert.Equal(t, "dashboard1", results[0].UID())
		assert.Equal(t, "default", results[0].Namespace())
		assert.Equal(t, 3, results[0].Reads())
		assert.Equal(t, 2, results[0].Users())

		assert.Equal(t, "dashboard2", results[1].UID())
		assert.Equal(t, "default", results[1].Namespace())
		assert.Equal(t, 1, results[1].Reads())
		assert.Equal(t, 1, results[1].Users())
	})

	t.Run("identically named dashboards in two different namespaces", func(t *testing.T) {
		t.Parallel()

		logs := []loki.Log{
			loki.NewLog(
				time.Now(),
				"log message 1",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/orange/dashboards/dashboard1",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 2",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/orange/dashboards/dashboard1",
					"uname": "user2",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 2",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/blue/dashboards/dashboard1",
					"uname": "user2",
				},
			),
		}

		client := &mockClient{
			logs: logs,
			err:  nil,
		}

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
			Token:  "apple",
		})
		require.NoError(t, err)

		opts := grafana.UsedDashboardsOptions{
			LowerThreshold: 1,
		}

		results, err := g.UsedDashboards(t.Context(), map[string]string{"app": "grafana"}, time.Hour, opts)
		require.NoError(t, err)
		require.Len(t, results, 2)

		assert.Equal(t, "dashboard1", results[0].UID())
		assert.Equal(t, "blue", results[0].Namespace())
		assert.Equal(t, 1, results[0].Reads())
		assert.Equal(t, 1, results[0].Users())

		assert.Equal(t, "dashboard1", results[1].UID())
		assert.Equal(t, "orange", results[1].Namespace())
		assert.Equal(t, 2, results[1].Reads())
		assert.Equal(t, 2, results[1].Users())
	})

	t.Run("ignores specified users", func(t *testing.T) {
		t.Parallel()

		logs := []loki.Log{
			loki.NewLog(
				time.Now(),
				"log message 1",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
					"uname": "user1",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 2",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
					"uname": "ignoredUser",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 3",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard2",
					"uname": "ignoredUser",
				},
			),
			loki.NewLog(
				time.Now(),
				"log message 4",
				map[string]string{
					"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard3",
					"uname": "user2",
				},
			),
		}

		client := &mockClient{
			logs: logs,
			err:  nil,
		}

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
			Token:  "pineapple",
		})
		require.NoError(t, err)

		opts := grafana.UsedDashboardsOptions{
			LowerThreshold: 1,
			IgnoredUsers:   []string{"ignoredUser"},
		}

		results, err := g.UsedDashboards(t.Context(), map[string]string{"app": "grafana"}, time.Hour, opts)
		require.NoError(t, err)
		require.Len(t, results, 2) // dashboard2 should be excluded as it's only accessed by ignored user.

		assert.Equal(t, "dashboard1", results[0].UID())
		assert.Equal(t, "default", results[0].Namespace())
		assert.Equal(t, 1, results[0].Reads()) // Only 1 read from non-ignored user.
		assert.Equal(t, 1, results[0].Users()) // Only 1 unique non-ignored user.

		assert.Equal(t, "dashboard3", results[1].UID())
		assert.Equal(t, "default", results[1].Namespace())
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
						"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard1",
						"uname": "user1",
					},
				),
				loki.NewLog(
					time.Now(),
					"log message 2",
					map[string]string{
						"path":  "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard2",
						"uname": "user2",
					},
				),
			},
			err: nil,
		}

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger: slog.Default(),
			Client: client,
			Token:  "pomelo",
		})
		require.NoError(t, err)

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
		assert.Equal(t, "default", results[0].Namespace())
		assert.Equal(t, 5, results[0].Reads())
		assert.Equal(t, 1, results[0].Users())

		assert.Equal(t, "dashboard2", results[1].UID())
		assert.Equal(t, "default", results[1].Namespace())
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
			assert.Equal(t, "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards", r.URL.Path)
			assert.Equal(t, "Bearer abc123", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			assert.Equal(t, "500", r.URL.Query().Get("limit"))

			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte("the server is down"))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		dashboards, err := g.AllDashboards(t.Context())
		require.EqualError(t, err, "getting dashboards page: unexpected status code: 500, body: the server is down")
		assert.Nil(t, dashboards)
		assert.Equal(t, 1, requestCount)
	})

	t.Run("invalid json response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte("!!!"))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

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

		now := time.Now()
		formattedTime := now.Format(time.RFC3339)

		rawJSON := `{
			"kind": "DashboardList",
			"apiVersion": "dashboard.grafana.app/v1beta1",
			"metadata": {
				"resourceVersion": "1741315830000",
				"continue": ""
			},
			"items": [
				{
					"kind": "Dashboard",
					"apiVersion": "dashboard.grafana.app/v1beta1",
					"metadata": {
						"name": "dashboard1",
						"namespace": "default",
						"uid": "VQyL7pNTpfGPNlPM6HRJSePrBg5dXmxr4iPQL7txLtwX",
						"resourceVersion": "1",
						"generation": 1,
						"creationTimestamp": "` + formattedTime + `",
						"labels": {
							"grafana.app/deprecatedInternalID": "11"
						},
						"annotations": {
							"grafana.app/createdBy": "service-account:dejwtrofg77y8d",
							"grafana.app/folder": "fef30w4jaxla8b",
							"grafana.app/updatedBy": "service-account:dejwtrofg77y8d",
							"grafana.app/updatedTimestamp": "` + formattedTime + `"
						}
					},
					"spec": {
						"editable": true,
						"fiscalYearStartMonth": 0,
						"graphTooltip": 0,
						"time": {
							"from": "now-6h",
							"to": "now"
						},
						"timepicker": {},
						"timezone": "browser",
						"title": "Dashboard 1"
					}
				},
				{
					"kind": "Dashboard",
					"apiVersion": "dashboard.grafana.app/v1beta1",
					"metadata": {
						"name": "dashboard2",
						"namespace": "default",
						"uid": "uid2",
						"resourceVersion": "2",
						"generation": 2,
						"creationTimestamp": "` + formattedTime + `",
						"annotations": {
							"grafana.app/createdBy": "service-account:cef2t2rfm73lsb",
							"grafana.app/updatedBy": "service-account:cef2t2rfm73lsb",
							"grafana.app/updatedTimestamp": "` + formattedTime + `"
						}
					},
					"spec": {
						"schemaVersion": 41,
						"title": "Dashboard 2"
					}
				}
			]
		}`

		var requestCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(rawJSON))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		dashboards, err := g.AllDashboards(t.Context())
		require.NoError(t, err)
		require.Len(t, dashboards, 2)

		assert.Equal(t, "dashboard1", dashboards[0].Name)
		assert.Equal(t, "default", dashboards[0].Namespace)
		assert.Equal(t, "VQyL7pNTpfGPNlPM6HRJSePrBg5dXmxr4iPQL7txLtwX", dashboards[0].UID)
		assert.Equal(t, formattedTime, dashboards[0].CreationTimestamp.Format(time.RFC3339))
		assert.JSONEq(
			t,
			`{"editable": true,"fiscalYearStartMonth": 0,"graphTooltip": 0,"time": {"from":`+
				` "now-6h","to": "now"},"timepicker": {},"timezone": "browser","title": "Dashboard 1"}`,
			string(dashboards[0].Spec),
		)

		assert.Equal(t, "dashboard2", dashboards[1].Name)
		assert.Equal(t, "default", dashboards[1].Namespace)
		assert.Equal(t, "uid2", dashboards[1].UID)
		assert.Equal(t, formattedTime, dashboards[1].CreationTimestamp.Format(time.RFC3339))
		assert.JSONEq(t, `{"schemaVersion": 41,"title": "Dashboard 2"}`, string(dashboards[1].Spec))

		assert.Equal(t, 1, requestCount)
	})

	t.Run("multiple pages", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		formattedTime := now.Format(time.RFC3339)

		firstPageJSON := `{
			"kind": "DashboardList",
			"apiVersion": "dashboard.grafana.app/v1beta1",
			"metadata": {
				"resourceVersion": "12345",
				"continue": "next-page-token"
			},
			"items": [
				{
					"kind": "Dashboard",
					"apiVersion": "dashboard.grafana.app/v1beta1",
					"metadata": {
						"name": "dashboard1",
						"namespace": "default",
						"uid": "uid1",
						"resourceVersion": "1",
						"generation": 1,
						"creationTimestamp": "` + formattedTime + `",
						"annotations": {
							"grafana.app/createdBy": "service-account:cef2t2rfm73lsb"
						}
					},
					"spec": {
						"title": "Dashboard 1"
					}
				}
			]
		}`

		secondPageJSON := `{
			"kind": "DashboardList",
			"apiVersion": "dashboard.grafana.app/v1beta1",
			"metadata": {
				"resourceVersion": "12345",
				"continue": ""
			},
			"items": [
				{
					"kind": "Dashboard",
					"apiVersion": "dashboard.grafana.app/v1beta1",
					"metadata": {
						"name": "dashboard2",
						"namespace": "default",
						"uid": "uid2",
						"resourceVersion": "2",
						"generation": 2,
						"creationTimestamp": "` + formattedTime + `",
						"annotations": {
							"grafana.app/createdBy": "service-account:cef2t2rfm73lsb",
							"grafana.app/updatedBy": "service-account:cef2t2rfm73lsb",
							"grafana.app/updatedTimestamp": "` + formattedTime + `"
						}
					},
					"spec": {
						"title": "Dashboard 2"
					}
				}
			]
		}`

		var requestCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			continueToken := r.URL.Query().Get("continue")
			responseBody := firstPageJSON

			if continueToken == "next-page-token" {
				responseBody = secondPageJSON
			}

			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(responseBody))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		dashboards, err := g.AllDashboards(t.Context())
		require.NoError(t, err)
		require.Len(t, dashboards, 2)

		assert.Equal(t, "dashboard1", dashboards[0].Name)
		assert.Equal(t, "default", dashboards[0].Namespace)
		assert.Equal(t, "uid1", dashboards[0].UID)

		assert.Equal(t, "dashboard2", dashboards[1].Name)
		assert.Equal(t, "default", dashboards[1].Namespace)
		assert.Equal(t, "uid2", dashboards[1].UID)

		assert.Equal(t, 2, requestCount)
	})
}

func TestClient_DeleteDashboard(t *testing.T) {
	t.Parallel()

	t.Run("empty uid", func(t *testing.T) {
		t.Parallel()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, "https://grafana.example.com"),
			Token:      "abc123",
		})
		require.NoError(t, err)

		err = g.DeleteDashboard(t.Context(), "")
		require.EqualError(t, err, "dashboard UID must not be empty")
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte("server error"))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		err = g.DeleteDashboard(t.Context(), "dashboard-uid")
		require.EqualError(t, err, "unexpected status code: 500, body: server error")
	})

	t.Run("invalid json response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte("!!!"))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		err = g.DeleteDashboard(t.Context(), "dashboard-uid")
		require.EqualError(
			t,
			err,
			`decoding response: invalid character '!' looking for beginning of value`,
		)
	})

	t.Run("non-success status in response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{
				"kind": "Status",
				"apiVersion": "v1",
				"metadata": {},
				"status": "Failed",
				"details": {
					"name": "dashboard-uid",
					"group": "dashboard.grafana.app",
					"kind": "dashboards",
					"uid": "dashboard-uid"
				}
			}`))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		err = g.DeleteDashboard(t.Context(), "dashboard-uid")
		require.EqualError(
			t,
			err,
			"got response code 200 from dashboard delete request but response body claims failure, "+
				"dashboard may have been deleted",
		)
	})

	t.Run("successful deletion", func(t *testing.T) {
		t.Parallel()
		var request *http.Request

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = r

			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{
				"kind": "Status",
				"apiVersion": "v1",
				"metadata": {},
				"status": "Success",
				"details": {
					"name": "dashboard-uid",
					"group": "dashboard.grafana.app",
					"kind": "dashboards",
					"uid": "dashboard-uid"
				}
			}`))
			assert.NoError(t, err)
		}))
		defer server.Close()

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: http.DefaultClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		err = g.DeleteDashboard(t.Context(), "dashboard-uid")
		require.NoError(t, err)
		assert.Equal(t, "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/dashboard-uid", request.URL.Path)
		assert.Equal(t, "Bearer abc123", request.Header.Get("Authorization"))
		assert.Equal(t, "application/json", request.Header.Get("Accept"))
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
		assert.Equal(t, http.MethodDelete, request.Method)
	})

	t.Run("http client error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// This should never be called as we're using a failing client.
			assert.Fail(t, "Server should not be called")
		}))
		defer server.Close()

		errorClient := &http.Client{
			Transport: &errorTransport{},
		}

		g, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     slog.Default(),
			HTTPClient: errorClient,
			Endpoint:   mustParseURL(t, server.URL),
			Token:      "abc123",
		})
		require.NoError(t, err)

		err = g.DeleteDashboard(t.Context(), "dashboard-uid")
		require.ErrorContains(t, err, "making request to Grafana")
		require.ErrorContains(t, err, "simulated client error")
	})
}

// errorTransport is an http.RoundTripper that always returns an error.
type errorTransport struct{}

func (e *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("simulated client error")
}

func mustParseURL(t *testing.T, input string) url.URL {
	t.Helper()
	parsed, err := url.Parse(input)
	if err != nil {
		require.NoError(t, err, "failed to parse URL: %s", input)
	}
	return *parsed
}
