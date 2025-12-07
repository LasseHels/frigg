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
	allDashboards   func(ctx context.Context, namespace string) ([]Dashboard, error)
	deleteDashboard func(ctx context.Context, namespace, name string, dashboardJSON []byte) error
}

func (m *mockGrafanaClient) UsedDashboards(
	ctx context.Context,
	labels map[string]string,
	r time.Duration,
	opts UsedDashboardsOptions,
) ([]DashboardReads, error) {
	return m.usedDashboards(ctx, labels, r, opts)
}

func (m *mockGrafanaClient) AllDashboards(ctx context.Context, namespace string) ([]Dashboard, error) {
	return m.allDashboards(ctx, namespace)
}

func (m *mockGrafanaClient) DeleteDashboard(ctx context.Context, namespace, name string, dashboardJSON []byte) error {
	return m.deleteDashboard(ctx, namespace, name, dashboardJSON)
}

func TestDashboardPruner_Start(t *testing.T) {
	t.Parallel()

	t.Run("ticks immediately", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Immediately cancel the context.

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return nil, errors.New("could not reach Grafana")
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:   mockClient,
			Logger:    l,
			Namespace: "default",
			Interval:  time.Hour,
		})

		pruner.Start(ctx)

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Starting dashboard pruner","dry":false,"namespace":"default","interval":"1h0m0s"}
{"level":"INFO","msg":"Pruning Grafana dashboards","dry":false,"namespace":"default"}
{"level":"ERROR","msg":"Failed to prune dashboards","dry":false,"namespace":"default","error":"fetching all Grafana dashboards: could not reach Grafana"}
{"level":"INFO","msg":"Stopping dashboard pruner","dry":false,"namespace":"default"}
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
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
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

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "default",
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, usedDashboardsCalled)

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards","dry":false,"namespace":"default"}
{"level":"INFO","msg":"Found all Grafana dashboards","dry":false,"namespace":"default","count":0}
{"level":"INFO","msg":"Found used Grafana dashboards","dry":false,"namespace":"default","count":0}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","dry":false,"namespace":"default","deleted_count":0,"deleted_dashboards":""}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("success with used dashboards only", func(t *testing.T) {
		t.Parallel()

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:       "cbf15242-fec5-4272-be50-1f83322ecf2c",
						Name:      "dashboard1",
						Namespace: "default",
						Title:     "Dashboard 1",
						Spec:      json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:       "50c3ea9d-d578-4c0f-a9c4-128577783c03",
						Name:      "dashboard2",
						Namespace: "default",
						Title:     "Dashboard 2",
						Spec:      json.RawMessage(`{"title": "Dashboard 2"}`),
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

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "default",
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards","dry":false,"namespace":"default"}
{"level":"INFO","msg":"Found all Grafana dashboards","dry":false,"namespace":"default","count":2}
{"level":"INFO","msg":"Found used Grafana dashboards","dry":false,"namespace":"default","count":2}
{"level":"DEBUG","msg":"Skipping used dashboard","dry":false,"namespace":"default","uid":"cbf15242-fec5-4272-be50-1f83322ecf2c","name":"dashboard1","title":"Dashboard 1","reads":10,"users":2,"range":"24h0m0s"}
{"level":"DEBUG","msg":"Skipping used dashboard","dry":false,"namespace":"default","uid":"50c3ea9d-d578-4c0f-a9c4-128577783c03","name":"dashboard2","title":"Dashboard 2","reads":5,"users":1,"range":"24h0m0s"}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","dry":false,"namespace":"default","deleted_count":0,"deleted_dashboards":""}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("success with unused dashboards", func(t *testing.T) {
		t.Parallel()

		var deletedNames []string

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:       "a22d74c5-83c5-4cd5-88a9-2af0544bdac2",
						Name:      "dashboard1",
						Namespace: "default",
						Title:     "Dashboard 1",
						Spec:      json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:       "441c13ff-dc1d-4d90-9984-b15532e626ff",
						Name:      "dashboard2",
						Namespace: "default",
						Title:     "Dashboard 2",
						Spec:      json.RawMessage(`{"title": "Dashboard 2"}`),
					},
					{
						UID:       "541517b1-3e42-497b-8038-25905320396e",
						Name:      "dashboard3",
						Namespace: "default",
						Title:     "Dashboard 3",
						Spec:      json.RawMessage(`{"title": "Dashboard 3"}`),
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
			deleteDashboard: func(ctx context.Context, namespace, name string, dashboardJSON []byte) error {
				deletedNames = append(deletedNames, name)
				assert.Equal(t, "default", namespace)

				switch name {
				case "dashboard2":
					assert.JSONEq(t, `{"title": "Dashboard 2"}`, string(dashboardJSON))
				case "dashboard3":
					assert.JSONEq(t, `{"title": "Dashboard 3"}`, string(dashboardJSON))
				default:
					t.Errorf("unexpected dashboard deleted: %s", name)
				}

				return nil
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "default",
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"dashboard2", "dashboard3"}, deletedNames)

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards","dry":false,"namespace":"default"}
{"level":"INFO","msg":"Found all Grafana dashboards","dry":false,"namespace":"default","count":3}
{"level":"INFO","msg":"Found used Grafana dashboards","dry":false,"namespace":"default","count":1}
{"level":"DEBUG","msg":"Skipping used dashboard","dry":false,"namespace":"default","uid":"a22d74c5-83c5-4cd5-88a9-2af0544bdac2","name":"dashboard1","title":"Dashboard 1","reads":10,"users":2,"range":"24h0m0s"}
{"level":"INFO","msg":"Deleting unused dashboard","dry":false,"namespace":"default","uid":"441c13ff-dc1d-4d90-9984-b15532e626ff","name":"dashboard2","title":"Dashboard 2","raw_json":"{\"title\": \"Dashboard 2\"}"}
{"level":"INFO","msg":"Deleted unused dashboard","dry":false,"namespace":"default","uid":"441c13ff-dc1d-4d90-9984-b15532e626ff","name":"dashboard2","title":"Dashboard 2","raw_json":"{\"title\": \"Dashboard 2\"}"}
{"level":"INFO","msg":"Deleting unused dashboard","dry":false,"namespace":"default","uid":"541517b1-3e42-497b-8038-25905320396e","name":"dashboard3","title":"Dashboard 3","raw_json":"{\"title\": \"Dashboard 3\"}"}
{"level":"INFO","msg":"Deleted unused dashboard","dry":false,"namespace":"default","uid":"541517b1-3e42-497b-8038-25905320396e","name":"dashboard3","title":"Dashboard 3","raw_json":"{\"title\": \"Dashboard 3\"}"}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","dry":false,"namespace":"default","deleted_count":2,"deleted_dashboards":"default/dashboard2, default/dashboard3"}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("does not delete unused dashboards in dry mode", func(t *testing.T) {
		t.Parallel()

		deleteDashboardCalled := false

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:       "3f02045e-5d94-4dbe-94e8-1353b1aede29",
						Name:      "dashboard1",
						Namespace: "blueberry",
						Title:     "Dashboard 1",
						Spec:      json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:       "952e2e84-2515-4f7c-b965-151671b3300c",
						Name:      "dashboard2",
						Namespace: "blueberry",
						Title:     "Dashboard 2",
						Spec:      json.RawMessage(`{"title": "Dashboard 2"}`),
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
			deleteDashboard: func(_ context.Context, _ string, _ string, _ []byte) error {
				deleteDashboardCalled = true
				return nil
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "blueberry",
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
			Dry:          true,
		})

		err := pruner.prune(t.Context())
		require.NoError(t, err)
		assert.False(t, deleteDashboardCalled, "deleteDashboard should not be called in dry mode")

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards","dry":true,"namespace":"blueberry"}
{"level":"INFO","msg":"Found all Grafana dashboards","dry":true,"namespace":"blueberry","count":2}
{"level":"INFO","msg":"Found used Grafana dashboards","dry":true,"namespace":"blueberry","count":1}
{"level":"INFO","msg":"Found unused dashboard, skipping deletion due to dry run","dry":true,"namespace":"blueberry","uid":"3f02045e-5d94-4dbe-94e8-1353b1aede29","name":"dashboard1","title":"Dashboard 1"}
{"level":"INFO","msg":"Found unused dashboard, skipping deletion due to dry run","dry":true,"namespace":"blueberry","uid":"952e2e84-2515-4f7c-b965-151671b3300c","name":"dashboard2","title":"Dashboard 2"}
{"level":"INFO","msg":"Finished pruning Grafana dashboards","dry":true,"namespace":"blueberry","deleted_count":0,"deleted_dashboards":""}
`
		assert.Equal(t, expectedLogs, logs.String())
	})

	t.Run("error fetching all dashboards", func(t *testing.T) {
		t.Parallel()

		mockClient := &mockGrafanaClient{
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return nil, errors.New("failed to connect to Grafana API")
			},
		}

		l, _ := logger()

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "default",
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
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:   "dashboard1",
						Name:  "Dashboard 1",
						Title: "Dashboard 1",
						Spec:  json.RawMessage(`{"title": "Dashboard 1"}`),
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

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "default",
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
			allDashboards: func(_ context.Context, _ string) ([]Dashboard, error) {
				return []Dashboard{
					{
						UID:       "dashboard1",
						Name:      "Dashboard 1",
						Namespace: "default",
						Title:     "Dashboard 1",
						Spec:      json.RawMessage(`{"title": "Dashboard 1"}`),
					},
					{
						UID:       "dashboard2",
						Name:      "Dashboard 2",
						Namespace: "default",
						Title:     "Dashboard 2",
						Spec:      json.RawMessage(`{"title": "Dashboard 2"}`),
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
			deleteDashboard: func(_ context.Context, _ string, _ string, _ []byte) error {
				return errors.New("dashboard delete failed")
			},
		}

		l, logs := logger()

		pruner := NewDashboardPruner(&NewDashboardPrunerOptions{
			Grafana:      mockClient,
			Logger:       l,
			Namespace:    "default",
			Interval:     time.Hour,
			IgnoredUsers: []string{"admin"},
			Period:       24 * time.Hour,
			Labels:       map[string]string{"app": "grafana"},
		})

		err := pruner.prune(t.Context())
		require.EqualError(t, err, "deleting unused dashboard dashboard1: dashboard delete failed")

		//nolint:lll
		expectedLogs := `{"level":"INFO","msg":"Pruning Grafana dashboards","dry":false,"namespace":"default"}
{"level":"INFO","msg":"Found all Grafana dashboards","dry":false,"namespace":"default","count":2}
{"level":"INFO","msg":"Found used Grafana dashboards","dry":false,"namespace":"default","count":0}
{"level":"INFO","msg":"Deleting unused dashboard","dry":false,"namespace":"default","uid":"dashboard1","name":"Dashboard 1","title":"Dashboard 1","raw_json":"{\"title\": \"Dashboard 1\"}"}
`
		assert.Equal(t, expectedLogs, logs.String())
	})
}

func newMockDashboardReads(name string, reads, users int) DashboardReads {
	return DashboardReads{
		name:      name,
		namespace: "default",
		reads:     reads,
		users:     users,
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
