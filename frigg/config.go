package frigg

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-playground/validator/v10"
	gogithub "github.com/google/go-github/v73/github"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/multierr"
	"gopkg.in/yaml.v3"

	"github.com/LasseHels/frigg/github"
	"github.com/LasseHels/frigg/grafana"
	"github.com/LasseHels/frigg/log"
	"github.com/LasseHels/frigg/loki"
	"github.com/LasseHels/frigg/server"
)

type Secrets struct {
	Grafana grafana.Secrets `yaml:"grafana" validate:"required"`
	Backup  BackupSecrets   `yaml:"backup" validate:"required"`
}

type BackupSecrets struct {
	GitHub github.Secrets `yaml:"github" validate:"required"`
}

type Config struct {
	Log     log.Config          `yaml:"log"`
	Server  server.Config       `yaml:"server"`
	Loki    loki.Config         `yaml:"loki" validate:"required"`
	Grafana grafana.Config      `yaml:"grafana" validate:"required"`
	Prune   grafana.PruneConfig `yaml:"prune" validate:"required"`
	Backup  BackupConfig        `yaml:"backup" validate:"required"`
}

type BackupConfig struct {
	GitHub github.Config `yaml:"github" validate:"required"`
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

// NewSecrets creates a new Secrets from the given path.
func NewSecrets(path string) (*Secrets, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "reading secrets file at path %q", path)
	}

	var secrets *Secrets
	dec := yaml.NewDecoder(bytes.NewReader(buf))

	if err := dec.Decode(&secrets); err != nil {
		return nil, errors.Wrap(err, "parsing secrets file")
	}

	if err := secrets.validate(); err != nil {
		return nil, errors.Wrap(err, "validating secrets")
	}

	return secrets, nil
}

// defaults sets default values for the Config.
func (c *Config) defaults() {
	c.Log.Level = slog.LevelInfo
	c.Server.Host = "localhost"
	c.Server.Port = 8080
	c.Prune.Dry = true
	c.Prune.Interval = 10 * time.Minute
	c.Prune.LowerThreshold = 10
	c.Backup.GitHub.Branch = "main"
	c.Backup.GitHub.Directory = "deleted-dashboards"
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

// newGitHubClient creates a new GitHub client with the provided configuration and secrets.
func (c *Config) newGitHubClient(
	httpClient *http.Client,
	secrets *Secrets,
	logger *slog.Logger,
) (*github.Client, error) {
	client := gogithub.NewClient(httpClient).WithAuthToken(secrets.Backup.GitHub.Token)
	if githubAPIURL := os.Getenv("GITHUB_API_URL"); githubAPIURL != "" {
		var err error
		client, err = client.WithEnterpriseURLs(githubAPIURL, githubAPIURL)
		if err != nil {
			return nil, errors.Wrap(err, "setting GitHub API URL")
		}
	}

	return github.NewClient(&github.ClientOptions{
		Client:     client,
		Repository: c.Backup.GitHub.Repository,
		Branch:     c.Backup.GitHub.Branch,
		Directory:  c.Backup.GitHub.Directory,
		Logger:     logger,
	}), nil
}

// mustParseURL parses a URL and panics if it cannot be parsed.
// This should only be used when the URL has already been validated.
func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(errors.Wrapf(err, "parsing URL %q", rawURL))
	}
	return u
}

// Initialise Frigg from the provided Config.
// This assumes that the provided Config has already been validated and might panic if not.
func (c *Config) Initialise(logger *slog.Logger, gatherer prometheus.Gatherer, secrets *Secrets) (*Frigg, error) {
	s := server.New(c.Server, logger)

	httpClient := &http.Client{}

	lokiClient := loki.NewClient(loki.ClientOptions{
		Endpoint:   c.Loki.Endpoint,
		HTTPClient: httpClient,
		Logger:     logger,
	})

	grafanaURL := mustParseURL(c.Grafana.Endpoint)

	githubClient, err := c.newGitHubClient(httpClient, secrets, logger)
	if err != nil {
		return nil, errors.Wrap(err, "creating GitHub client")
	}

	var pruners []dashboardPruner
	for namespace, token := range secrets.Grafana.Tokens {
		grafanaClient, err := grafana.NewClient(&grafana.NewClientOptions{
			Logger:     logger,
			Client:     lokiClient,
			HTTPClient: httpClient,
			Endpoint:   *grafanaURL,
			Token:      token,
			Storage:    githubClient,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "creating Grafana client for namespace %s", namespace)
		}

		pruner := grafana.NewDashboardPruner(&grafana.NewDashboardPrunerOptions{
			Grafana:        grafanaClient,
			Logger:         logger,
			Namespace:      namespace,
			Interval:       c.Prune.Interval,
			IgnoredUsers:   c.Prune.IgnoredUsers,
			Period:         c.Prune.Period,
			Labels:         c.Prune.Labels,
			Dry:            c.Prune.Dry,
			LowerThreshold: c.Prune.LowerThreshold,
		})
		pruners = append(pruners, pruner)
	}

	return New(logger, s, gatherer, pruners), nil
}

// validate ensures the configuration is valid.
func (c *Config) validate() error {
	return validate(c)
}

// validate ensures the secrets configuration is valid.
func (s *Secrets) validate() error {
	return validate(s)
}

// validate performs validation on any struct using the validator package.
func validate(s any) error {
	v := validator.New(validator.WithRequiredStructEnabled())

	if err := v.Struct(s); err != nil {
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
