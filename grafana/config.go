package grafana

import "time"

type Config struct {
	Endpoint string `yaml:"endpoint" json:"endpoint" validate:"required,url"`
}

type PruneConfig struct {
	Dry            bool              `yaml:"dry" json:"dry"`
	Interval       time.Duration     `yaml:"interval" json:"interval"`
	IgnoredUsers   []string          `yaml:"ignored_users" json:"ignored_users"`
	Period         time.Duration     `yaml:"period" json:"period" validate:"required"`
	Labels         map[string]string `yaml:"labels" json:"labels" validate:"required"`
	LowerThreshold int               `yaml:"lower_threshold" json:"lower_threshold" validate:"min=0"`
}

type Secrets struct {
	Tokens map[string]string `yaml:"tokens" json:"tokens" validate:"required,min=1,dive,keys,required,endkeys,required"`
}
