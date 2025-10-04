package grafana

import "time"

type Config struct {
	Endpoint string `yaml:"endpoint" validate:"required,url"`
}

type PruneConfig struct {
	Dry            bool              `yaml:"dry"`
	Namespaces     []string          `yaml:"namespaces" validate:"required,min=1,dive,required"`
	Interval       time.Duration     `yaml:"interval"`
	IgnoredUsers   []string          `yaml:"ignored_users"`
	Period         time.Duration     `yaml:"period" validate:"required"`
	Labels         map[string]string `yaml:"labels" validate:"required"`
	LowerThreshold int               `yaml:"lower_threshold" validate:"min=0"`
}

type Secrets struct {
	Token string `yaml:"token" validate:"required"`
}
