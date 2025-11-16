package log

import "log/slog"

type Config struct {
	Level slog.Level `yaml:"level"`
}
