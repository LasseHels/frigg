package grafana

import "time"

type Config struct {
	Endpoint string `yaml:"endpoint" validate:"required,url"`
}

type PruneConfig struct {
	Dry          bool              `yaml:"dry"`
	Interval     time.Duration     `yaml:"interval"`
	IgnoredUsers []string          `yaml:"ignored_users"`
	Period       time.Duration     `yaml:"period" validate:"required"`
	Labels       map[string]string `yaml:"labels" validate:"required"`
}
