package loki_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/loki"
)

type mockHTTPClient struct {
	responses   []*http.Response
	err         error
	lastRequest *http.Request
	callCount   int
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.lastRequest = req
	if m.err != nil {
		return nil, m.err
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func TestClient_QueryRange(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		endpoint        string
		query           string
		start           time.Time
		end             time.Time
		clientResponses []*http.Response
		clientErr       error
		expectedErr     string
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
			clientResponses: []*http.Response{
				{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("internal server error")),
				},
			},
			expectedErr: "unexpected status code: 500, body: internal server error",
		},
		"error reading response body": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResponses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(&errorReader{errors.New("read error")}),
				},
			},
			expectedErr: "reading response body: read error",
		},
		"invalid json response": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResponses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("not json")),
				},
			},
			expectedErr: "unmarshalling response: invalid character 'o' in literal null (expecting 'u')",
		},
		"non-success status in response": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResponses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"status":"error","error":"query timeout"}`)),
				},
			},
			expectedErr: "query failed with status: error",
		},
		"invalid value format": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResponses: []*http.Response{
				{
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
			},
			expectedErr: "invalid value format in Loki response: [only one value]",
		},
		"invalid timestamp": {
			endpoint: "http://localhost:1234",
			query:    `{app="test"}`,
			start:    time.Now().Add(-1 * time.Hour),
			end:      time.Now(),
			clientResponses: []*http.Response{
				{
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
			},
			expectedErr: "parsing timestamp \"not-a-timestamp\": strconv.ParseInt: parsing \"not-a-timestamp\": invalid syntax",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := loki.NewClient(loki.ClientOptions{
				Endpoint:   tc.endpoint,
				HTTPClient: &mockHTTPClient{responses: tc.clientResponses, err: tc.clientErr},
				Logger:     slog.Default(),
				Limit:      100,
			})

			logs, err := client.QueryRange(t.Context(), tc.query, tc.start, tc.end)
			require.EqualError(t, err, tc.expectedErr)
			assert.Nil(t, logs)
		})
	}

	t.Run("successful request", func(t *testing.T) {
		t.Parallel()

		mock := &mockHTTPClient{
			responses: []*http.Response{
				{
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
		}

		client := loki.NewClient(loki.ClientOptions{
			Endpoint:   "http://localhost:1234",
			HTTPClient: mock,
			Logger:     slog.Default(),
			Limit:      100,
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

		require.NotNil(t, mock.lastRequest)
		assert.Equal(t, "100", mock.lastRequest.URL.Query().Get("limit"))
		assert.Equal(t, "forward", mock.lastRequest.URL.Query().Get("direction"))
	})

	t.Run("sets X-Scope-OrgID header when tenant ID is configured", func(t *testing.T) {
		t.Parallel()

		mock := &mockHTTPClient{
			responses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
					"status": "success",
					"data": {
						"resultType": "streams",
						"result": []
					}
				}`)),
				},
			},
		}

		client := loki.NewClient(loki.ClientOptions{
			Endpoint:   "http://localhost:1234",
			TenantID:   "my-tenant",
			HTTPClient: mock,
			Logger:     slog.Default(),
			Limit:      100,
		})

		_, err := client.QueryRange(t.Context(), `{app="test"}`, time.Now().Add(-1*time.Hour), time.Now())
		require.NoError(t, err)

		require.NotNil(t, mock.lastRequest)
		assert.Equal(t, "my-tenant", mock.lastRequest.Header.Get("X-Scope-OrgID"))
	})

	t.Run("does not set X-Scope-OrgID header when tenant ID is empty", func(t *testing.T) {
		t.Parallel()

		mock := &mockHTTPClient{
			responses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
					"status": "success",
					"data": {
						"resultType": "streams",
						"result": []
					}
				}`)),
				},
			},
		}

		client := loki.NewClient(loki.ClientOptions{
			Endpoint:   "http://localhost:1234",
			HTTPClient: mock,
			Logger:     slog.Default(),
			Limit:      100,
		})

		_, err := client.QueryRange(t.Context(), `{app="test"}`, time.Now().Add(-1*time.Hour), time.Now())
		require.NoError(t, err)

		require.NotNil(t, mock.lastRequest)
		assert.Empty(t, mock.lastRequest.Header.Get("X-Scope-OrgID"))
	})

	t.Run("paginates through multiple pages of results", func(t *testing.T) {
		t.Parallel()

		// First page returns exactly limit (2) results, second page returns fewer.
		mock := &mockHTTPClient{
			responses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"status": "success",
						"data": {
							"resultType": "streams",
							"result": [
								{
									"stream": {"app": "test"},
									"values": [
										["1609459200000000000", "log 1"],
										["1609459201000000000", "log 2"]
									]
								}
							]
						}
					}`)),
				},
				{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"status": "success",
						"data": {
							"resultType": "streams",
							"result": [
								{
									"stream": {"app": "test"},
									"values": [
										["1609459202000000000", "log 3"]
									]
								}
							]
						}
					}`)),
				},
			},
		}

		client := loki.NewClient(loki.ClientOptions{
			Endpoint:   "http://localhost:1234",
			HTTPClient: mock,
			Logger:     slog.Default(),
			Limit:      2,
		})

		start := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2021, 1, 1, 1, 0, 0, 0, time.UTC)

		logs, err := client.QueryRange(t.Context(), `{app="test"}`, start, end)
		require.NoError(t, err)
		assert.Len(t, logs, 3)
		assert.Equal(t, 2, mock.callCount)

		assert.Equal(t, "log 1", logs[0].Message())
		assert.Equal(t, "log 2", logs[1].Message())
		assert.Equal(t, "log 3", logs[2].Message())

		// Verify the second request had an updated start time (1 nanosecond after log 2).
		expectedSecondStart := time.Date(2021, 1, 1, 0, 0, 1, 1, time.UTC)
		actualStart := mock.lastRequest.URL.Query().Get("start")
		assert.Equal(t, expectedSecondStart.UnixNano(), mustParseInt64(t, actualStart))
	})
}

func mustParseInt64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	require.NoError(t, err)
	return v
}

// errorReader is a simple io.Reader that always returns an error.
type errorReader struct {
	err error
}

func (e *errorReader) Read(_ []byte) (int, error) {
	return 0, e.err
}
