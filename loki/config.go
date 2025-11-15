package loki

type Config struct {
	Endpoint string `yaml:"endpoint" json:"endpoint" validate:"required,url"`
}
