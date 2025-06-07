package frigg

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/LasseHels/frigg/pkg/server"
)

type Config struct {
	// TODO: Logger config.
	Server server.Config `yaml:"server"`
}

func (c *Config) Initialise(logger *slog.Logger, gatherer prometheus.Gatherer) *Frigg {
	s := server.New(c.Server, logger)
	return New(logger, s, gatherer)
}
