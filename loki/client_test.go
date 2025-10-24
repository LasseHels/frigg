package loki_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/loki"
)

type mockHTTPClient struct {
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return m.response, m.err
}

func TestClient_QueryRange(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		endpoint    string
		query       string
		start       time.Time
		end         time.Time
		clientResp  *http.Response
		clientErr   error
		expectedErr string
	}{
		"invalid endpoint url": {
			endpoint: "http://localhost:1234\t\n",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			expectedErr: `parsing URL: parse "http://localhost:1234\t\n/loki/api/v1/query_range":` +
				` net/url: invalid control character in URL`,
		},
		"http client error": {
			endpoint:    "http://localhost:1234",
			query:       `{app="test"}`,
			start:       time.Now().Add(-1 * time.Hour),
			end:         time.Now(),
			clientErr:   errors.New("connection refused"),
			expectedErr: "executing request: connection refused",
		},
		"non-200 status code": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResp: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("internal server error")),
			},
			expectedErr: "unexpected status code: 500, body: internal server error",
		},
		"error reading response body": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&errorReader{errors.New("read error")}),
			},
			expectedErr: "reading response body: read error",
		},
		"invalid json response": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not json")),
			},
			expectedErr: "unmarshalling response: invalid character 'o' in literal null (expecting 'u')",
		},
		"non-success status in response": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"error","error":"query timeout"}`)),
			},
			expectedErr: "query failed with status: error",
		},
		"invalid value format": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResp: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"status": "success",
					"data": {
						"resultType": "streams",
						"result": [
							{
								"stream": {"app": "test"},
								"values": [["only one value"]]
							}
						]
					}
				}`)),
			},
			expectedErr: "invalid value format in Loki response: [only one value]",
		},
		"invalid timestamp": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResp: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"status": "success",
					"data": {
						"resultType": "streams",
						"result": [
							{
								"stream": {"app": "test"},
								"values": [["not-a-timestamp", "log message"]]
							}
						]
					}
				}`)),
			},
			expectedErr: "parsing timestamp \"not-a-timestamp\": strconv.ParseInt: parsing \"not-a-timestamp\": invalid syntax",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := loki.NewClient(loki.ClientOptions{
				Endpoint:   tc.endpoint,
				HTTPClient: &mockHTTPClient{response: tc.clientResp, err: tc.clientErr},
				Logger:     slog.Default(),
			})

			logs, err := client.QueryRange(t.Context(), tc.query, tc.start, tc.end)
			require.EqualError(t, err, tc.expectedErr)
			assert.Nil(t, logs)
		})
	}

	t.Run("successful request", func(t *testing.T) {
		t.Parallel()

		client := loki.NewClient(loki.ClientOptions{
			Endpoint: "http://localhost:1234",
			HTTPClient: &mockHTTPClient{
				response: &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"status": "success",
						"data": {
							"resultType": "streams",
							"result": [
								{
									"stream": {"app": "test", "env": "prod"},
									"values": [
										["1609459200000000000", "log message 1"],
										["1609459201000000000", "log message 2"]
									]
								}
							]
						}
					}`)),
				},
			},
			Logger: slog.Default(),
		})

		logs, err := client.QueryRange(t.Context(), `{app="test"}`, time.Now().Add(-1*time.Hour), time.Now())
		require.NoError(t, err)
		assert.Len(t, logs, 2)

		assert.Equal(t, "log message 1", logs[0].Message())
		assert.Equal(t, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), logs[0].Timestamp())
		assert.Equal(t, map[string]string{"app": "test", "env": "prod"}, logs[0].Stream())

		assert.Equal(t, "log message 2", logs[1].Message())
		assert.Equal(t, time.Date(2021, 1, 1, 0, 0, 1, 0, time.UTC), logs[1].Timestamp())
		assert.Equal(t, map[string]string{"app": "test", "env": "prod"}, logs[1].Stream())
	})
}

// errorReader is a simple io.Reader that always returns an error.
type errorReader struct {
	err error
}

func (e *errorReader) Read(_ []byte) (int, error) {
	return 0, e.err
}
