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
	"github.com/LasseHels/frigg/loki"
)

type lokiPushStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type lokiPushPayload struct {
	Streams []lokiPushStream `json:"streams"`
}

func TestClient_QueryRange_Integration(t *testing.T) {
	integrationtest.SkipIfShort(t)

	lokiContainer := integrationtest.NewLoki(t)

	now := time.Now().UTC()
	firstTimestamp := now.Add(-30 * time.Minute)
	secondTimestamp := now.Add(-40 * time.Minute)
	beforeRange := now.Add(-2 * time.Hour)
	queryStart := now.Add(-60 * time.Minute)
	queryEnd := now

	labels := map[string]string{"app": "frigg-test", "env": "integration"}

	pushLogEntry(t, lokiContainer.Host(), beforeRange, "Log entry before query range", labels)

	pushLogEntry(t, lokiContainer.Host(), firstTimestamp, `traceID=123456 msg="First log in range"`, labels)
	pushLogEntry(t, lokiContainer.Host(), secondTimestamp, "Second log entry in range", labels)

	client := loki.NewClient(loki.ClientOptions{
		Endpoint:   fmt.Sprintf("http://%s", lokiContainer.Host()),
		HTTPClient: http.DefaultClient,
		Logger:     slog.Default(),
		Limit:      100,
	})

	query := `{app="frigg-test",env="integration"} | logfmt`

	// Poll until logs are available or timeout is reached
	var logs []loki.Log
	assert.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		logs, err = client.QueryRange(t.Context(), query, queryStart, queryEnd)
		assert.NoError(collect, err)
		assert.Len(collect, logs, 2, "Expected to get exactly 2 logs within the query range")
	}, 10*time.Second, 50*time.Millisecond, "Timed out waiting for logs to be queryable")

	assert.Equal(t, `traceID=123456 msg="First log in range"`, logs[0].Message())
	assert.Equal(t, "Second log entry in range", logs[1].Message())
	assert.Equal(t, firstTimestamp, logs[0].Timestamp())
	assert.Equal(t, secondTimestamp, logs[1].Timestamp())

	assert.Equal(
		t,
		map[string]string{
			"app":     "frigg-test",
			"env":     "integration",
			"traceID": "123456",
			"msg":     "First log in range",
		},
		logs[0].Stream(),
	)
	assert.Equal(t, map[string]string{"app": "frigg-test", "env": "integration"}, logs[1].Stream())
}

// TestClient_QueryRange_SameTimestamp_Integration documents a known pagination limitation: if more
// logs share the exact same nanosecond timestamp than the configured limit, logs beyond the first
// `limit` at that timestamp will be missed.
func TestClient_QueryRange_SameTimestamp_Integration(t *testing.T) {
	integrationtest.SkipIfShort(t)

	lokiContainer := integrationtest.NewLoki(t)

	now := time.Now().UTC()
	sharedTimestamp := now.Add(-30 * time.Minute)
	queryStart := now.Add(-60 * time.Minute)
	queryEnd := now

	labels := map[string]string{"app": "frigg-same-ts", "env": "integration"}

	// Push 5 logs with the exact same timestamp.
	for i := range 5 {
		pushLogEntry(t, lokiContainer.Host(), sharedTimestamp, fmt.Sprintf("log entry %d", i+1), labels)
	}

	// Configure client with a limit lower than the number of logs.
	client := loki.NewClient(loki.ClientOptions{
		Endpoint:   fmt.Sprintf("http://%s", lokiContainer.Host()),
		HTTPClient: http.DefaultClient,
		Logger:     slog.Default(),
		Limit:      2,
	})

	query := `{app="frigg-same-ts",env="integration"}`

	var logs []loki.Log
	assert.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		logs, err = client.QueryRange(t.Context(), query, queryStart, queryEnd)
		assert.NoError(collect, err)
		// We expect to get only 2 logs due to the pagination limitation.
		// All 5 logs share the same timestamp, so after fetching 2, we advance
		// the start time by 1ns and miss the remaining 3.
		assert.Len(collect, logs, 2)
	}, 10*time.Second, 50*time.Millisecond)

	assert.Equal(t, "log entry 1", logs[0].Message())
	assert.Equal(t, "log entry 2", logs[1].Message())
	assert.Equal(t, sharedTimestamp, logs[0].Timestamp())
	assert.Equal(t, sharedTimestamp, logs[1].Timestamp())
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
