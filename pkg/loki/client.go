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

	"github.com/pkg/errors"
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientOptions struct {
	Endpoint   string
	HTTPClient httpClient
	Logger     *slog.Logger
}

type Client struct {
	endpoint string
	client   httpClient
	logger   *slog.Logger
}

func NewClient(opts ClientOptions) *Client {
	return &Client{
		endpoint: opts.Endpoint,
		client:   opts.HTTPClient,
		logger:   opts.Logger,
	}
}

type Log struct {
	timestamp time.Time
	message   string
}

func (l *Log) Timestamp() time.Time {
	return l.timestamp
}

func (l *Log) Message() string {
	return l.message
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

// QueryRange queries Loki logs over a range of time.
//
// See https://grafana.com/docs/loki/v2.9.x/reference/api/#query-loki-over-a-range-of-time.
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time) ([]Log, error) {
	u, err := url.Parse(fmt.Sprintf("%s/loki/api/v1/query_range", c.endpoint))
	if err != nil {
		return nil, errors.Wrap(err, "parsing URL")
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	q.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, errors.Wrap(err, "creating request")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "executing request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading response body")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var response queryRangeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshalling response")
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", response.Status)
	}

	var logs []Log
	for _, stream := range response.Data.Result {
		for _, value := range stream.Values {
			if len(value) != 2 {
				return nil, fmt.Errorf("invalid value format in Loki response: %v", value)
			}

			nanoseconds, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("parsing timestamp %q", value[0]))
			}

			timestamp := time.Unix(0, nanoseconds).UTC()
			message := value[1]

			logs = append(logs, Log{
				timestamp: timestamp,
				message:   message,
			})
		}
	}

	return logs, nil
}
