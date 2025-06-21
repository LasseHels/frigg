package loki_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/integrationtest"
	"github.com/LasseHels/frigg/pkg/loki"
)

type lokiPushStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type lokiPushPayload struct {
	Streams []lokiPushStream `json:"streams"`
}

func TestClient_QueryRange_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	lokiContainer := integrationtest.NewLoki(t.Context())
	t.Cleanup(func() {
		if err := lokiContainer.Stop(); err != nil {
			t.Logf("Failed to terminate Loki container: %v", err)
		}
	})

	now := time.Now().UTC()
	inRange := now.Add(-30 * time.Minute)
	beforeRange := now.Add(-2 * time.Hour)
	queryStart := now.Add(-60 * time.Minute)
	queryEnd := now

	labels := map[string]string{"app": "frigg-test", "env": "integration"}

	pushLogEntry(t, lokiContainer.Host(), beforeRange, "Log entry before query range", labels)

	pushLogEntry(t, lokiContainer.Host(), inRange, "First log entry in range", labels)
	pushLogEntry(t, lokiContainer.Host(), inRange, "Second log entry in range", labels)

	client := loki.NewClient(loki.ClientOptions{
		Endpoint:   fmt.Sprintf("http://%s", lokiContainer.Host()),
		HTTPClient: http.DefaultClient,
		Logger:     slog.Default(),
	})

	query := `{app="frigg-test",env="integration"}`

	// Poll until logs are available or timeout is reached
	var logs []loki.Log
	assert.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		logs, err = client.QueryRange(t.Context(), query, queryStart, queryEnd)
		assert.NoError(collect, err)
		assert.Len(collect, logs, 2, "Expected to get exactly 2 logs within the query range")
	}, 10*time.Second, 50*time.Millisecond, "Timed out waiting for logs to be queryable")

	assert.Equal(t, "Second log entry in range", logs[0].Message())
	assert.Equal(t, "First log entry in range", logs[1].Message())
	assert.Equal(t, inRange, logs[0].Timestamp())
	assert.Equal(t, inRange, logs[1].Timestamp())
}

func pushLogEntry(t *testing.T, lokiAddress string, timestamp time.Time, message string, labels map[string]string) {
	t.Helper()

	payload := lokiPushPayload{
		Streams: []lokiPushStream{
			{
				Stream: labels,
				Values: [][]string{
					{
						fmt.Sprintf("%d", timestamp.UnixNano()),
						message,
					},
				},
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := http.Post(
		fmt.Sprintf("http://%s/loki/api/v1/push", lokiAddress),
		"application/json",
		bytes.NewReader(jsonPayload),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent, resp.StatusCode, "Failed to push log entry to Loki: %s", body)
}
