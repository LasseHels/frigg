package grafana

type Config struct {
	Endpoint string `yaml:"endpoint" validate:"required,url"`
}

type Secrets struct {
	Token string `yaml:"token" validate:"required"`
}
