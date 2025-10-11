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
}

type Secrets struct {
	// Tokens used to authenticate with Grafana's API for specific namespaces. This field is a map where keys are
	// namespace names and values are the token used to authenticate with Grafana's API for that namespace. A namespace's
	// token is expected to have permissions to list and delete dashboards in that namespace.
	//
	// This field also controls which namespaces Frigg will prune and which it will ignore; Frigg will only prune
	// namespaces that have an entry in this map.
	//
	// See https://grafana.com/docs/grafana/v12.0/developers/http_api/apis/#namespace-namespace.
	//
	// The map must contain at least one namespace.
	//
	// Required.
	Tokens map[string]string `yaml:"tokens" validate:"required,min=1,dive,keys,required,endkeys,required"`
}
