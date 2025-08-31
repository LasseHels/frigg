package integrationtest

import (
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// Logger is a testcontainers.LogConsumer that emits logs via a testing.T instance.
//
// https://golang.testcontainers.org/features/follow_logs.
type Logger struct {
	t *testing.T
}

func NewLogger(t *testing.T) *Logger {
	return &Logger{t: t}
}

func (lc *Logger) Accept(l testcontainers.Log) {
	lc.t.Log(string(l.Content))
}
