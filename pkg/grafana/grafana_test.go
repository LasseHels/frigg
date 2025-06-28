package grafana_test

import (
	"context"
	"errors"
	"log/slog"
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
