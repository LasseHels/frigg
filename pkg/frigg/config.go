package frigg

import (
	"bytes"
	"log/slog"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/multierr"
	"gopkg.in/yaml.v3"

	"github.com/LasseHels/frigg/pkg/server"
)

type Config struct {
	// TODO: Logger config.
	Server server.Config `yaml:"server"`
}

// NewConfig creates a new Config with default values.
func NewConfig() *Config {
	c := &Config{}
	c.defaults()
	return c
}

// defaults sets default values for the Config.
func (c *Config) defaults() {
	c.Server.Host = "localhost"
	c.Server.Port = 8080
}

// Load configuration from a YAML file at path.
func (c *Config) Load(path string) error {
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
