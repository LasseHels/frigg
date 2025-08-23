package loki

type Config struct {
	Endpoint string `yaml:"endpoint" validate:"required,url"`
}