package frigg

import (
	"context"
	"log/slog"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/LasseHels/frigg/pkg/frigg/handlers"
	"github.com/LasseHels/frigg/pkg/server"
)

type dashboardPruner interface {
	Start(ctx context.Context)
}

type Frigg struct {
	logger   *slog.Logger
	server   *server.Server
	gatherer prometheus.Gatherer
	pruner   dashboardPruner
}

func New(logger *slog.Logger, s *server.Server, gatherer prometheus.Gatherer, pruner dashboardPruner) *Frigg {
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

	// Start the dashboard pruner in its own goroutine
	go f.pruner.Start(ctx)

	// Start the server (this blocks until stopped)
	if err := f.server.Start(); err != nil {
		return errors.Wrap(err, "starting server")
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
