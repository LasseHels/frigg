package handlers_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LasseHels/frigg/frigg/handlers"
)

func TestHealth(t *testing.T) {
	t.Parallel()

	t.Run("successful write response", func(t *testing.T) {
		t.Parallel()

		l, _ := logger()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)

		handler := handlers.Health(l)
		handler(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "healthy", recorder.Body.String())
	})

	t.Run("unsuccessful write response", func(t *testing.T) {
		t.Parallel()

		l, logs := logger()
		failingWriter := &mockResponseWriter{
			writeError: errors.New("write error"),
		}
		req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)

		handler := handlers.Health(l)
		handler(failingWriter, req)

		assert.Contains(
			t,
			logs.String(),
			`{"level":"ERROR","msg":"Failed to write health response","error":"write error"}`,
		)
		// No assertions on response as the test is checking that the error is logged.
	})
}

// mockResponseWriter implements http.ResponseWriter and returns an error on Write.
type mockResponseWriter struct {
	writeError error
}

func (m *mockResponseWriter) Header() http.Header {
	return http.Header{}
}

func (m *mockResponseWriter) Write(_ []byte) (int, error) {
	return 0, m.writeError
}

func (m *mockResponseWriter) WriteHeader(_ int) {}

func logger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	replaceTime := func(_ []string, a slog.Attr) slog.Attr {
		// The time field of a log line is often the only variable value; we replace it to get deterministic output that
		// is easier to test.
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}

		return a
	}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: replaceTime,
	})
	l := slog.New(handler)

	return l, buf
}
