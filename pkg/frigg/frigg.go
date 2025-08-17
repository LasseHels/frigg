package frigg

import (
	"context"
	"log/slog"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.com/LasseHels/frigg/pkg/frigg/handlers"
	"github.com/LasseHels/frigg/pkg/grafana"
	"github.com/LasseHels/frigg/pkg/server"
)

type Frigg struct {
	logger   *slog.Logger
	server   *server.Server
	gatherer prometheus.Gatherer
	pruner   *grafana.DashboardPruner
}

func New(logger *slog.Logger, s *server.Server, gatherer prometheus.Gatherer, pruner *grafana.DashboardPruner) *Frigg {
	return &Frigg{
		logger:   logger,
		server:   s,
		gatherer: gatherer,
		pruner:   pruner,
	}
}

// Start Frigg. Start blocks until the context is cancelled.
func (f *Frigg) Start(ctx context.Context) error {
	f.logger.Info("Starting Frigg")

	f.registerRoutes()

	eg, ctx := errgroup.WithContext(ctx)

	// Start the dashboard pruner
	eg.Go(func() error {
		f.pruner.Start(ctx)
		return nil
	})

	// Start the server
	eg.Go(func() error {
		return f.server.Start()
	})

	// Wait for context cancellation then stop components
	eg.Go(func() error {
		<-ctx.Done()
		return f.server.Stop()
	})

	// Wait for all goroutines to complete
	if err := eg.Wait(); err != nil {
		return errors.Wrap(err, "starting components")
	}

	return nil
}

func (f *Frigg) Stop() error {
	f.logger.Info("Stopping Frigg")

	if err := f.server.Stop(); err != nil {
		return errors.Wrap(err, "stopping server")
	}

	f.logger.Info("Stopped Frigg")
	return nil
}

func (f *Frigg) registerRoutes() {
	f.server.RegisterRoute(server.Route{
		Path:    "/health",
		Methods: []string{"GET"},
		Func:    handlers.Health(f.logger),
	})

	f.server.RegisterRoute(server.Route{
		Path:    "/metrics",
		Methods: []string{"GET"},
		Func:    promhttp.HandlerFor(f.gatherer, promhttp.HandlerOpts{}).ServeHTTP,
	})
}
