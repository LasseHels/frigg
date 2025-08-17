package frigg

import (
	"bytes"
	"log/slog"
	"os"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/multierr"
	"gopkg.in/yaml.v3"

	"github.com/LasseHels/frigg/pkg/grafana"
	"github.com/LasseHels/frigg/pkg/log"
	"github.com/LasseHels/frigg/pkg/server"
)

type Config struct {
	Log     log.Config           `yaml:"log"`
	Server  server.Config        `yaml:"server"`
	Grafana grafana.Config       `yaml:"grafana" validate:"required"`
	Prune   grafana.PruneConfig  `yaml:"prune" validate:"required"`
}

// NewConfig creates a new Config with default values and loads configuration from the given path.
func NewConfig(path string) (*Config, error) {
	c := &Config{}
	c.defaults()

	if err := c.load(path); err != nil {
		return nil, errors.Wrap(err, "loading configuration")
	}

	if err := c.validate(); err != nil {
		return nil, errors.Wrap(err, "validating configuration")
	}

	return c, nil
}

// defaults sets default values for the Config.
func (c *Config) defaults() {
	c.Log.Level = slog.LevelInfo
	c.Server.Host = "localhost"
	c.Server.Port = 8080
	
	c.Prune.Dry = true
	c.Prune.Interval = 10 * time.Minute
}

// load configuration from a YAML file at path.
func (c *Config) load(path string) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "reading config file at path %q", path)
	}

	buf = []byte(os.ExpandEnv(string(buf)))

	if len(bytes.TrimSpace(buf)) == 0 {
		return nil
	}

	dec := yaml.NewDecoder(bytes.NewReader(buf))

	if err := dec.Decode(c); err != nil {
		return errors.Wrap(err, "parsing config file")
	}

	return nil
}

// Initialise Frigg from the provided Config.
func (c *Config) Initialise(logger *slog.Logger, gatherer prometheus.Gatherer) *Frigg {
	s := server.New(c.Server, logger)
	return New(logger, s, gatherer)
}

// validate ensures the configuration is valid.
func (c *Config) validate() error {
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
