package grafana

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type grafanaClient interface {
	UsedDashboards(
		ctx context.Context,
		labels map[string]string,
		r time.Duration,
		opts UsedDashboardsOptions,
	) ([]DashboardReads, error)
	AllDashboards(ctx context.Context, namespace string) ([]Dashboard, error)
	DeleteDashboard(ctx context.Context, namespace, name string, dashboardJSON []byte) error
}

type DashboardPruner struct {
	grafana        grafanaClient
	logger         *slog.Logger
	namespace      string
	interval       time.Duration
	ignoredUsers   []string
	period         time.Duration
	labels         map[string]string
	dry            bool
	lowerThreshold int
	skipTags       []string
}

type NewDashboardPrunerOptions struct {
	Grafana grafanaClient
	Logger  *slog.Logger
	// Namespace in which to prune dashboards.
	Namespace string
	// Interval with which to prune dashboards.
	Interval time.Duration
	// IgnoredUsers whose reads do not count toward the usage of a dashboard.
	// See also UsedDashboardsOptions.IgnoredUsers.
	//
	// IgnoredUsers is case-sensitive.
	IgnoredUsers []string
	// Period of time to use when analysing dashboard usage.
	// See also Client.UsedDashboards.
	Period time.Duration
	// Labels with which to identify log lines emitted by Grafana.
	// See also Client.UsedDashboards.
	Labels map[string]string
	// Dry determines whether to actually delete dashboards.
	// If true, DashboardPruner will only log which dashboards would be deleted instead of actually deleting them.
	Dry bool
	// See UsedDashboardsOptions.LowerThreshold.
	LowerThreshold int
	// SkipTags is a list of dashboard tags that cause dashboards to be skipped during pruning. If a dashboard has any
	// of these tags, it will be skipped. If this slice is empty or nil, no dashboards will be skipped based on tags.
	SkipTags []string
}

func NewDashboardPruner(opts *NewDashboardPrunerOptions) *DashboardPruner {
	logger := opts.Logger.With(
		slog.Bool("dry", opts.Dry),
		slog.String("namespace", opts.Namespace),
	)

	return &DashboardPruner{
		grafana:        opts.Grafana,
		logger:         logger,
		namespace:      opts.Namespace,
		interval:       opts.Interval,
		ignoredUsers:   opts.IgnoredUsers,
		period:         opts.Period,
		labels:         opts.Labels,
		dry:            opts.Dry,
		lowerThreshold: opts.LowerThreshold,
		skipTags:       opts.SkipTags,
	}
}

func (d *DashboardPruner) Start(ctx context.Context) {
	d.logger.Info("Starting dashboard pruner", slog.String("interval", d.interval.String()))

	// Ensure immediate tick.
	// https://github.com/golang/go/issues/17601#issuecomment-319105374.
	d.tick(ctx)

	tick := time.Tick(d.interval)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("Stopping dashboard pruner")
			return
		case <-tick:
			d.tick(ctx)
		}
	}
}

func (d *DashboardPruner) tick(ctx context.Context) {
	if err := d.prune(ctx); err != nil {
		d.logger.Error("Failed to prune dashboards", slog.String("error", err.Error()))
	}
}

func (d *DashboardPruner) prune(ctx context.Context) error {
	d.logger.Info("Pruning Grafana dashboards")

	all, err := d.grafana.AllDashboards(ctx, d.namespace)
	if err != nil {
		return fmt.Errorf("fetching all Grafana dashboards: %w", err)
	}

	d.logger.Info("Found all Grafana dashboards", slog.Int("count", len(all)))

	opts := UsedDashboardsOptions{
		IgnoredUsers:   d.ignoredUsers,
		LowerThreshold: d.lowerThreshold,
	}
	used, err := d.grafana.UsedDashboards(ctx, d.labels, d.period, opts)
	if err != nil {
		return fmt.Errorf("fetching used Grafana dashboards: %w", err)
	}

	d.logger.Info("Found used Grafana dashboards", slog.Int("count", len(used)))
	usedDashboards := d.usedMap(used)
	var deleted []string

	for i := range all {
		dashboard := &all[i]
		dashboardLogger := d.logger.With(
			slog.String("uid", dashboard.UID),
			slog.String("name", dashboard.Name),
			slog.String("title", dashboard.Title),
		)
		usage, isUsed := usedDashboards[dashboard.Key()]
		if isUsed {
			dashboardLogger.Debug(
				"Skipping used dashboard",
				slog.Int("reads", usage.Reads()),
				slog.Int("users", usage.Users()),
				slog.String("range", d.period.String()),
			)
			continue
		}

		if dashboard.Provisioned() {
			dashboardLogger.Debug("Skipping provisioned dashboard", slog.String("managed_by", *dashboard.ManagedBy))
			continue
		}

		skip, matchedTag := d.hasSkipTag(dashboard)
		if skip {
			dashboardLogger.Info(
				"Skipping dashboard with skip tag",
				slog.String("tag", matchedTag),
				slog.Any("dashboard_tags", dashboard.Tags),
			)
			continue
		}

		if d.dry {
			dashboardLogger.Info("Found unused dashboard, skipping deletion due to dry run")
			continue
		}

		dashboardLogger.Info("Deleting unused dashboard", slog.String("raw_json", string(dashboard.Spec)))
		if err := d.grafana.DeleteDashboard(ctx, dashboard.Namespace, dashboard.Name, dashboard.Spec); err != nil {
			return fmt.Errorf("deleting unused dashboard %s: %w", dashboard.UID, err)
		}
		dashboardLogger.Info("Deleted unused dashboard", slog.String("raw_json", string(dashboard.Spec)))
		deleted = append(deleted, fmt.Sprintf("%s/%s", dashboard.Namespace, dashboard.Name))
	}

	d.logger.Info(
		"Finished pruning Grafana dashboards",
		slog.Int("deleted_count", len(deleted)),
		slog.String("deleted_dashboards", strings.Join(deleted, ", ")),
	)

	return nil
}

func (d *DashboardPruner) usedMap(used []DashboardReads) map[DashboardKey]DashboardReads {
	m := make(map[DashboardKey]DashboardReads, len(used))

	for _, u := range used {
		m[u.Key()] = u
	}

	return m
}

// hasSkipTag returns true if the dashboard has any tag in the skip list, along with the matched tag name.
func (d *DashboardPruner) hasSkipTag(dashboard *Dashboard) (bool, string) {
	if len(d.skipTags) == 0 {
		return false, ""
	}

	for _, tag := range d.skipTags {
		if dashboard.HasTag(tag) {
			return true, tag
		}
	}

	return false, ""
}
