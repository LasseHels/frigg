package handlers

import (
	"log/slog"
	"net/http"
)

func Health(l *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("healthy")); err != nil {
			l.Error("Failed to write health response", slog.String("error", err.Error()))
		}
	}
}
