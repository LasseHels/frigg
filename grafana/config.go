package grafana

import "time"

type Config struct {
	Endpoint string `yaml:"endpoint" validate:"required,url"`
}

type PruneConfig struct {
	Dry            bool              `yaml:"dry"`
	Interval       time.Duration     `yaml:"interval"`
	IgnoredUsers   []string          `yaml:"ignored_users"`
	Period         time.Duration     `yaml:"period" validate:"required"`
	Labels         map[string]string `yaml:"labels" validate:"required"`
	LowerThreshold int               `yaml:"lower_threshold" validate:"min=0"`
	Skip           *SkipConfig       `yaml:"skip"`
}

type SkipConfig struct {
	Tags *SkipTagsConfig `yaml:"tags" validate:"required"`
}

type SkipTagsConfig struct {
	Any []string `yaml:"any" validate:"required,min=1,dive,required"`
}

type Secrets struct {
	Tokens map[string]string `yaml:"tokens" json:"tokens" validate:"required,min=1,dive,keys,required,endkeys,required"`
}
