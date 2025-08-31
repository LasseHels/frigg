package grafana

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type grafanaClient interface {
	UsedDashboards(
		ctx context.Context,
		labels map[string]string,
		r time.Duration,
		opts UsedDashboardsOptions,
	) ([]DashboardReads, error)
	AllDashboards(ctx context.Context) ([]Dashboard, error)
	DeleteDashboard(ctx context.Context, uid string) error
}

type DashboardPruner struct {
	grafana        grafanaClient
	logger         *slog.Logger
	interval       time.Duration
	ignoredUsers   []string
	period         time.Duration
	labels         map[string]string
	dry            bool
	lowerThreshold int
}

type NewDashboardPrunerOptions struct {
	Grafana grafanaClient
	Logger  *slog.Logger
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
}

func NewDashboardPruner(opts *NewDashboardPrunerOptions) *DashboardPruner {
	logger := opts.Logger.With(slog.Bool("dry", opts.Dry))

	return &DashboardPruner{
		grafana:        opts.Grafana,
		logger:         logger,
		interval:       opts.Interval,
		ignoredUsers:   opts.IgnoredUsers,
		period:         opts.Period,
		labels:         opts.Labels,
		dry:            opts.Dry,
		lowerThreshold: opts.LowerThreshold,
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

	all, err := d.grafana.AllDashboards(ctx)
	if err != nil {
		return errors.Wrap(err, "fetching all Grafana dashboards")
	}

	d.logger.Info("Found all Grafana dashboards", slog.Int("count", len(all)))

	opts := UsedDashboardsOptions{
		IgnoredUsers:   d.ignoredUsers,
		LowerThreshold: d.lowerThreshold,
	}
	used, err := d.grafana.UsedDashboards(ctx, d.labels, d.period, opts)
	if err != nil {
		return errors.Wrap(err, "fetching used Grafana dashboards")
	}

	d.logger.Info("Found used Grafana dashboards", slog.Int("count", len(used)))
	usedDashboards := d.usedMap(used)
	var deletedUIDs []string

	for _, dashboard := range all {
		dashboardLogger := d.logger.With(slog.String("uid", dashboard.UID), slog.String("name", dashboard.Name))
		usage, isUsed := usedDashboards[dashboard.UID]
		if isUsed {
			dashboardLogger.Debug(
				"Skipping used dashboard",
				slog.Int("reads", usage.Reads()),
				slog.Int("users", usage.Users()),
				slog.String("range", d.period.String()),
			)
			continue
		}

		if d.dry {
			dashboardLogger.Info("Found unused dashboard, skipping deletion due to dry run")
			continue
		}

		dashboardLogger.Info("Deleting unused dashboard", slog.String("raw_json", string(dashboard.Spec)))
		if err := d.grafana.DeleteDashboard(ctx, dashboard.UID); err != nil {
			return errors.Wrapf(err, "deleting unused dashboard %s", dashboard.UID)
		}
		dashboardLogger.Info("Deleted unused dashboard", slog.String("raw_json", string(dashboard.Spec)))
		deletedUIDs = append(deletedUIDs, dashboard.UID)
	}

	d.logger.Info(
		"Finished pruning Grafana dashboards",
		slog.Int("deleted_count", len(deletedUIDs)),
		slog.String("uids", strings.Join(deletedUIDs, ", ")),
	)

	return nil
}

func (d *DashboardPruner) usedMap(used []DashboardReads) map[string]DashboardReads {
	m := make(map[string]DashboardReads, len(used))

	for _, u := range used {
		m[u.UID()] = u
	}

	return m
}
