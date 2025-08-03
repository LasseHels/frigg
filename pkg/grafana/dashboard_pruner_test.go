package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGrafanaClient struct {
	usedDashboards func(
		ctx context.Context,
		labels map[string]string,
		r time.Duration,
		opts UsedDashboardsOptions,
	) ([]DashboardReads, error)
	allDashboards   func(ctx context.Context) ([]Dashboard, error)
	deleteDashboard func(ctx context.Context, uid string) error
}

func (m *mockGrafanaClient) UsedDashboards(
	ctx context.Context,
	labels map[string]string,
	r time.Duration,
	opts UsedDashboardsOptions,
) ([]DashboardReads, error) {
	return m.usedDashboards(ctx, labels, r, opts)
}

func (m *mockGrafanaClient) AllDashboards(ctx context.Context) ([]Dashboard, error) {
	return m.allDashboards(ctx)
}

func (m *mockGrafanaClient) DeleteDashboard(ctx context.Context, uid string) error {
	return m.deleteDashboard(ctx, uid)
}

func TestDashboardPruner_Start(t *testing.T) {
	t.Parallel()

	t.Run("ticks immediately", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Immediately cancel the context.

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return nil, errors.New("could not reach Grafana")
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:  mockClient,
			Logger:   l,
			Interval: time.Hour,
		})

		pruner.Start(ctx)

		expectedLogs := `{"level":"INFO","msg":"Starting dashboard pruner","interval":"1h0m0s"}
{"level":"INFO","msg":"Pruning Grafana dashboards"}
{"level":"ERROR","msg":"Failed to prune dashboards","error":"fetching all Grafana dashboards: could not reach Grafana"}
{"level":"INFO","msg":"Stopping dashboard pruner"}
`
		assert.Equal(t, expectedLogs, logs.String())
	})
}

func TestDashboardPruner_Prune(t *testing.T) {
	t.Parallel()

	t.Run("success with no dashboards", func(t *testing.T) {
		t.Parallel()

		usedDashboardsCalled := 0
		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return nil, nil
			},
			usedDashboards: func(
				_ context.Context,
				labels map[string]string,
				r time.Duration,
				opts UsedDashboardsOptions,
			) ([]DashboardReads, error) {
				usedDashboardsCalled++
				assert.Equal(t, map[string]string{"app": "grafana"}, labels)
				assert.Equal(t, 24*time.Hour, r)
				assert.Equal(t, []string{"admin"}, opts.IgnoredUsers)
				return nil, nil
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, usedDashboardsCalled)

		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards"}
{"level":"INFO","msg":"Found all Grafana dashboards","count":0}
{"level":"INFO","msg":"Found used Grafana dashboards","count":0}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","deleted_count":0,"uids":""}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("success with used dashboards only", func(t *testing.T) {
		t.Parallel()

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:  "dashboard1",
						Name: "Dashboard 1",
						Spec: json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:  "dashboard2",
						Name: "Dashboard 2",
						Spec: json.RawMessage(`{"title": "Dashboard 2"}`),
					},
				}, nil
			},
			usedDashboards: func(
				_ context.Context,
				_ map[string]string,
				_ time.Duration,
				_ UsedDashboardsOptions,
			) ([]DashboardReads, error) {
				return []DashboardReads{
					newMockDashboardReads("dashboard1", 10, 2),
					newMockDashboardReads("dashboard2", 5, 1),
				}, nil
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards"}
{"level":"INFO","msg":"Found all Grafana dashboards","count":2}
{"level":"INFO","msg":"Found used Grafana dashboards","count":2}
{"level":"DEBUG","msg":"Skipping used dashboard","uid":"dashboard1","name":"Dashboard 1","reads":10,"users":2,"range":"24h0m0s"}
{"level":"DEBUG","msg":"Skipping used dashboard","uid":"dashboard2","name":"Dashboard 2","reads":5,"users":1,"range":"24h0m0s"}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","deleted_count":0,"uids":""}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("success with unused dashboards", func(t *testing.T) {
		t.Parallel()

		var deletedUIDs []string

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:  "dashboard1",
						Name: "Dashboard 1",
						Spec: json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:  "dashboard2",
						Name: "Dashboard 2",
						Spec: json.RawMessage(`{"title": "Dashboard 2"}`),
					},
					{
						UID:  "dashboard3",
						Name: "Dashboard 3",
						Spec: json.RawMessage(`{"title": "Dashboard 3"}`),
					},
				}, nil
			},
			usedDashboards: func(
				_ context.Context,
				_ map[string]string,
				_ time.Duration,
				_ UsedDashboardsOptions,
			) ([]DashboardReads, error) {
				return []DashboardReads{
					newMockDashboardReads("dashboard1", 10, 2),
				}, nil
			},
			deleteDashboard: func(_ context.Context, uid string) error {
				deletedUIDs = append(deletedUIDs, uid)
				return nil
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"dashboard2", "dashboard3"}, deletedUIDs)

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards"}
{"level":"INFO","msg":"Found all Grafana dashboards","count":3}
{"level":"INFO","msg":"Found used Grafana dashboards","count":1}
{"level":"DEBUG","msg":"Skipping used dashboard","uid":"dashboard1","name":"Dashboard 1","reads":10,"users":2,"range":"24h0m0s"}
{"level":"INFO","msg":"Deleting unused dashboard","uid":"dashboard2","name":"Dashboard 2","raw_json":"{\"title\": \"Dashboard 2\"}"}
{"level":"INFO","msg":"Deleted unused dashboard","uid":"dashboard2","name":"Dashboard 2","raw_json":"{\"title\": \"Dashboard 2\"}"}
{"level":"INFO","msg":"Deleting unused dashboard","uid":"dashboard3","name":"Dashboard 3","raw_json":"{\"title\": \"Dashboard 3\"}"}
{"level":"INFO","msg":"Deleted unused dashboard","uid":"dashboard3","name":"Dashboard 3","raw_json":"{\"title\": \"Dashboard 3\"}"}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","deleted_count":2,"uids":"dashboard2, dashboard3"}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("error fetching all dashboards", func(t *testing.T) {
		t.Parallel()

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return nil, errors.New("failed to connect to Grafana API")
			},
		}

		l, _ := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.EqualError(t, err, "fetching all Grafana dashboards: failed to connect to Grafana API")
	})

	t.Run("error fetching used dashboards", func(t *testing.T) {
		t.Parallel()

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:  "dashboard1",
						Name: "Dashboard 1",
						Spec: json.RawMessage(`{"title": "Dashboard 1"}`),
					},
				}, nil
			},
			usedDashboards: func(
				_ context.Context,
				_ map[string]string,
				_ time.Duration,
				_ UsedDashboardsOptions,
			) ([]DashboardReads, error) {
				return nil, errors.New("failed to query Loki")
			},
		}

		l, _ := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.EqualError(t, err, "fetching used Grafana dashboards: failed to query Loki")
	})

	t.Run("error deleting dashboard", func(t *testing.T) {
		t.Parallel()

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:  "dashboard1",
						Name: "Dashboard 1",
						Spec: json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:  "dashboard2",
						Name: "Dashboard 2",
						Spec: json.RawMessage(`{"title": "Dashboard 2"}`),
					},
				}, nil
			},
			usedDashboards: func(
				_ context.Context,
				_ map[string]string,
				_ time.Duration,
				_ UsedDashboardsOptions,
			) ([]DashboardReads, error) {
				return nil, nil
			},
			deleteDashboard: func(_ context.Context, _ string) error {
				return errors.New("dashboard delete failed")
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.EqualError(t, err, "deleting unused dashboard dashboard1: dashboard delete failed")

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards"}
{"level":"INFO","msg":"Found all Grafana dashboards","count":2}
{"level":"INFO","msg":"Found used Grafana dashboards","count":0}
{"level":"INFO","msg":"Deleting unused dashboard","uid":"dashboard1","name":"Dashboard 1","raw_json":"{\"title\": \"Dashboard 1\"}"}
`
		assert.Equal(t, expectedLogs, logs.String())
	})
}

func newMockDashboardReads(uid string, reads, users int) DashboardReads {
	return DashboardReads{
		uid:   uid,
		reads: reads,
		users: users,
	}
}

func logger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	replaceTime := func(_ []string, a slog.Attr) slog.Attr {
		// The time field of a log line is often the only variable value; we replace it to get deterministic output that
		// is easier to test.
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}

		return a
	}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: replaceTime,
		Level:       slog.LevelDebug,
	})
	l := slog.New(handler)

	return l, buf
}
