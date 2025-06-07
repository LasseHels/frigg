package frigg

import (
	"log/slog"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/LasseHels/frigg/pkg/frigg/handlers"
	"github.com/LasseHels/frigg/pkg/server"
)

type Frigg struct {
	logger   *slog.Logger
	server   *server.Server
	gatherer prometheus.Gatherer
}

func New(logger *slog.Logger, s *server.Server, gatherer prometheus.Gatherer) *Frigg {
	return &Frigg{
		logger:   logger,
		server:   s,
		gatherer: gatherer,
	}
}

// Start Frigg. Start blocks until Stop is called.
func (f *Frigg) Start() error {
	f.logger.Info("Starting Frigg")

	f.registerRoutes()

	return f.server.Start()
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
