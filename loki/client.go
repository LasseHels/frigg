package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientOptions struct {
	Endpoint   string
	TenantID   string
	HTTPClient httpClient
	Logger     *slog.Logger
	Limit      int
}

type Client struct {
	endpoint string
	tenantID string
	client   httpClient
	logger   *slog.Logger
	limit    int
}

func NewClient(opts ClientOptions) *Client {
	return &Client{
		endpoint: opts.Endpoint,
		tenantID: opts.TenantID,
		client:   opts.HTTPClient,
		logger:   opts.Logger,
		limit:    opts.Limit,
	}
}

type Log struct {
	timestamp time.Time
	message   string
	stream    map[string]string
}

func NewLog(timestamp time.Time, message string, stream map[string]string) Log {
	return Log{
		timestamp: timestamp,
		message:   message,
		stream:    stream,
	}
}

func (l *Log) Timestamp() time.Time {
	return l.timestamp
}

func (l *Log) Message() string {
	return l.message
}

func (l *Log) Stream() map[string]string {
	return l.stream
}

// queryRangeResult represents a single stream result in the Loki query response.
type queryRangeResult struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// queryRangeData represents the data portion of the Loki query response.
type queryRangeData struct {
	ResultType string             `json:"resultType"`
	Result     []queryRangeResult `json:"result"`
}

// queryRangeResponse represents the complete response from a Loki query_range API call.
type queryRangeResponse struct {
	Status string         `json:"status"`
	Data   queryRangeData `json:"data"`
}

// QueryRange queries Loki logs over a range of time and automatically paginates through all results.
//
// See [Loki API documentation].
//
// [Loki API documentation]: https://grafana.com/docs/loki/v2.9.x/reference/api/#query-loki-over-a-range-of-time
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time) ([]Log, error) {
	var allLogs []Log
	currentStart := start

	for {
		logs, err := c.queryRangePage(ctx, query, currentStart, end)
		if err != nil {
			return nil, err
		}

		allLogs = append(allLogs, logs...)

		done := len(logs) < c.limit
		if done {
			break
		}

		// Advance start time to 1 nanosecond after the last log's timestamp.
		//
		// Unlikely edge case: a count of logs greater than the configured limit share the exact same nanosecond
		// timestamp. For example, if the limit is 1000 and there are 1500 logs with timestamp T, the first query will
		// return 1000 logs with timestamp T, and the next query will start at T + 1 nanosecond, missing the remaining
		// 500 logs with timestamp T.
		lastLog := logs[len(logs)-1]
		currentStart = lastLog.Timestamp().Add(time.Nanosecond)
	}

	return allLogs, nil
}

// queryRangePage executes a single query_range request to Loki.
func (c *Client) queryRangePage(ctx context.Context, query string, start, end time.Time) ([]Log, error) {
	u, err := url.Parse(fmt.Sprintf("%s/loki/api/v1/query_range", c.endpoint))
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	q.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	q.Set("limit", strconv.Itoa(c.limit))
	q.Set("direction", "forward")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", c.tenantID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var response queryRangeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("unmarshalling response: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", response.Status)
	}

	var logs []Log
	for _, result := range response.Data.Result {
		for _, value := range result.Values {
			if len(value) != 2 {
				return nil, fmt.Errorf("invalid value format in Loki response: %v", value)
			}

			nanoseconds, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parsing timestamp %q: %w", value[0], err)
			}

			timestamp := time.Unix(0, nanoseconds).UTC()
			message := value[1]

			l := NewLog(timestamp, message, result.Stream)
			logs = append(logs, l)
		}
	}

	return logs, nil
}
