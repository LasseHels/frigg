package frigg

import (
	"log/slog"

	"github.com/go-playground/validator/v10"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/multierr"

	"github.com/LasseHels/frigg/pkg/server"
)

type Config struct {
	// TODO: Logger config.
	Server server.Config `yaml:"server"`
}

// Initialise Frigg from the provided Config.
// Initialise assumes that Config is valid (see Validate).
func (c *Config) Initialise(logger *slog.Logger, gatherer prometheus.Gatherer) *Frigg {
	s := server.New(c.Server, logger)
	return New(logger, s, gatherer)
}

func (c *Config) Validate() error {
	v := validator.New()

	if err := v.Struct(c); err != nil {
		var errs []error
		var valErrs validator.ValidationErrors

		if errors.As(err, &valErrs) {
			for _, e := range valErrs {
				errs = append(errs, e)
			}
		}

		return multierr.Combine(errs...)
	}

	return nil
}
