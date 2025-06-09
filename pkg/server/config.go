package server

type Config struct {
	Host string `yaml:"host" validate:"required"`       // Host name of the Server.
	Port int    `yaml:"port" validate:"required,min=1"` // Port for the Server to listen on.
}
