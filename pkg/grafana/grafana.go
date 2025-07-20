package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/LasseHels/frigg/pkg/loki"
)

type client interface {
	QueryRange(ctx context.Context, query string, start, end time.Time) ([]loki.Log, error)
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	logger     *slog.Logger
	client     client
	httpClient httpClient
	endpoint   url.URL
	token      string
}

type NewClientOptions struct {
	Logger     *slog.Logger
	Client     client
	HTTPClient httpClient
	Endpoint   url.URL // Endpoint where Grafana can be reached, e.g. "https://grafana.example.com".
	// Token used when authenticating with Grafana's HTTP API.
	// Token is expected to have permissions to:
	// - List dashboards in the Grafana instance. Frigg can only evaluate usage of dashboards that it can list.
	//   Dashboards that Token cannot list are not evaluated.
	// - Delete dashboards.
	Token string
}

func (n *NewClientOptions) validate() error {
	if n.Token == "" {
		return errors.New("token must not be empty")
	}

	return nil
}

func NewClient(opts *NewClientOptions) (*Client, error) {
	if err := opts.validate(); err != nil {
		return nil, errors.Wrap(err, "validating Grafana client options")
	}

	return &Client{
		logger:     opts.Logger,
		client:     opts.Client,
		httpClient: opts.HTTPClient,
		endpoint:   opts.Endpoint,
		token:      opts.Token,
	}, nil
}

type UsedDashboardsOptions struct {
	// IgnoredUsers whose reads do not count towards dashboard reads.
	// A dashboard read exclusively by ignored users is considered unused.
	//
	// IgnoredUsers is case-sensitive.
	//
	// By default, no users are ignored.
	IgnoredUsers []string
	// ChunkSize used when querying logs. To avoid executing a single large query, Client chunks queries into smaller
	// queries of this size. For example, if ChunkSize is two hours, Client splits a single 10-hour query into five
	// smaller two-hour queries.
	//
	// If ChunkSize is greater than the given range, only a single read query is executed for the entire range.
	//
	// Defaults to four hours.
	ChunkSize time.Duration
	// LowerThreshold, under which the dashboard usage analysis is cancelled. If fewer than LowerThreshold logs are
	// found in the given range, an error is returned.
	//
	// Since Grafana doesn't expose a formal API for dashboard usage, Frigg uses Grafana's logs as an API. This is
	// dubious as Grafana makes no promise that the format of its logs will remain stable. If a Grafana update causes
	// the format of logs upon which Frigg relies to change, then we'd prefer for Frigg to fail fast rather than
	// erroneously consider all dashboards unused.
	//
	// LowerThreshold defaults to 10.
	LowerThreshold int
}

// validate checks that the options are valid.
func (o *UsedDashboardsOptions) validate() error {
	if o.ChunkSize < 0 {
		return fmt.Errorf("chunk size must be zero or greater, got %v", o.ChunkSize)
	}
	if o.LowerThreshold < 0 {
		return fmt.Errorf("lower threshold must be zero or greater, got %d", o.LowerThreshold)
	}
	return nil
}

type DashboardReads struct {
	uid   string
	reads int
	users int
}

// UID of the dashboard.
func (d *DashboardReads) UID() string {
	return d.uid
}

// Reads is the number of times the dashboard has been read.
func (d *DashboardReads) Reads() int {
	return d.reads
}

// Users is the number of unique users that have read the dashboard.
func (d *DashboardReads) Users() int {
	return d.users
}

// extractDashboardUID from a path string.
// Path is expected to be in the format "/api/dashboards/uid/f7fe2e95-f430-4243-a830-a556b515d902".
func extractDashboardUID(path string) (string, error) {
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 5 || pathParts[3] != "uid" {
		return "", fmt.Errorf("unexpected path format: %q, expected format /api/dashboards/uid/:uid", path)
	}
	return pathParts[4], nil
}

// UsedDashboards returns information about dashboard usage in range (now() - r) to now().
//
// A used dashboard is one that has been read by an unignored user (see UsedDashboardsOptions.IgnoredUsers) in the
// given range.
//
// UsedDashboards errors if labels is empty.
//
// UsedDashboards reads Client logs from a Loki instance and determines dashboard usage based on dashboard read logs.
// UsedDashboards chunks large Loki read queries into smaller queries. See UsedDashboardsOptions.ChunkSize.
func (c *Client) UsedDashboards(
	ctx context.Context,
	labels map[string]string,
	r time.Duration,
	opts UsedDashboardsOptions,
) ([]DashboardReads, error) {
	if len(labels) == 0 {
		return nil, errors.New("labels must not be empty")
	}

	if err := opts.validate(); err != nil {
		return nil, errors.Wrap(err, "invalid options")
	}

	if opts.ChunkSize == 0 {
		opts.ChunkSize = 4 * time.Hour
	}
	if opts.LowerThreshold == 0 {
		opts.LowerThreshold = 10
	}

	ignoredUsers := make(map[string]struct{})
	for _, user := range opts.IgnoredUsers {
		ignoredUsers[user] = struct{}{}
	}

	var labelParts []string
	for k, v := range labels {
		labelParts = append(labelParts, fmt.Sprintf(`%s=%q`, k, v))
	}
	labelStr := strings.Join(labelParts, ", ")
	if labelStr != "" {
		labelStr = "{" + labelStr + "}"
	}

	query := fmt.Sprintf(`%s
|= "/api/dashboards/uid/:uid"
|= "Request Completed"
| logfmt
| handler = "/api/dashboards/uid/:uid"`, labelStr)

	end := time.Now().UTC()
	start := end.Add(-r)

	var logs []loki.Log

	for chunkStart := start; chunkStart.Before(end); chunkStart = chunkStart.Add(opts.ChunkSize) {
		chunkEnd := chunkStart.Add(opts.ChunkSize)
		if chunkEnd.After(end) {
			chunkEnd = end
		}

		chunkLogs, err := c.client.QueryRange(ctx, query, chunkStart, chunkEnd)
		if err != nil {
			return nil, fmt.Errorf("querying loki: %w", err)
		}

		logs = append(logs, chunkLogs...)
	}

	if len(logs) < opts.LowerThreshold {
		return nil, fmt.Errorf("found fewer logs (%d) than the lower threshold (%d)", len(logs), opts.LowerThreshold)
	}

	readsByUID := make(map[string]map[string]struct{})
	readCounts := make(map[string]int)

	for _, log := range logs {
		stream := log.Stream()

		path, ok := stream["path"]
		if !ok {
			return nil, fmt.Errorf("could not find path in stream labels: %v", stream)
		}

		dashboardUID, err := extractDashboardUID(path)
		if err != nil {
			return nil, err
		}

		user, ok := stream["uname"]
		if !ok {
			return nil, fmt.Errorf("could not find uname in stream labels: %v", stream)
		}

		if _, ignored := ignoredUsers[user]; ignored {
			continue
		}

		readCounts[dashboardUID]++

		if _, exists := readsByUID[dashboardUID]; !exists {
			readsByUID[dashboardUID] = make(map[string]struct{})
		}
		readsByUID[dashboardUID][user] = struct{}{}
	}

	result := make([]DashboardReads, 0, len(readsByUID))
	for uid, users := range readsByUID {
		result = append(result, DashboardReads{
			uid:   uid,
			reads: readCounts[uid],
			users: len(users),
		})
	}

	// The result slice is created from a map with no guaranteed order, so we sort it by dashboard UID for consistency.
	sort.Slice(result, func(i, j int) bool {
		return result[i].uid < result[j].uid
	})

	return result, nil
}

type Dashboard struct {
	Name              string          `json:"name"`
	Namespace         string          `json:"namespace"`
	UID               string          `json:"uid"`
	CreationTimestamp time.Time       `json:"creationTimestamp"`
	Spec              json.RawMessage `json:"spec"`
}

type dashboardListResponse struct {
	Metadata listMetadata    `json:"metadata"`
	Items    []dashboardItem `json:"items"`
}

type listMetadata struct {
	Continue string `json:"continue"`
}

type dashboardItem struct {
	Kind       string                `json:"kind"`
	APIVersion string                `json:"apiVersion"`
	Metadata   dashboardItemMetadata `json:"metadata"`
	Spec       json.RawMessage       `json:"spec"`
}

type dashboardItemMetadata struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	UID               string    `json:"uid"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
}

// AllDashboards returns all dashboards from the Grafana instance.
//
// AllDashboards uses the Grafana HTTP API endpoint "List dashboards" to fetch all dashboards.
// See https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#list-dashboards.
//
// AllDashboards handles pagination automatically and fetches all pages.
func (c *Client) AllDashboards(ctx context.Context) ([]Dashboard, error) {
	var allDashboards []Dashboard
	pageSize := 500
	continueToken := ""

	for {
		dashboards, nextContinueToken, err := c.dashboardsPage(ctx, pageSize, continueToken)
		if err != nil {
			return nil, errors.Wrap(err, "getting dashboards page")
		}

		allDashboards = append(allDashboards, dashboards...)

		if nextContinueToken == "" {
			break
		}

		continueToken = nextContinueToken
	}

	return allDashboards, nil
}

// dashboardsPage fetches a single page of dashboard results from the Grafana API.
func (c *Client) dashboardsPage(ctx context.Context, limit int, continueToken string) ([]Dashboard, string, error) {
	u := c.endpoint.JoinPath("apis", "dashboard.grafana.app", "v1beta1", "namespaces", "default", "dashboards")

	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", limit))
	if continueToken != "" {
		q.Set("continue", continueToken)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, "", errors.Wrap(err, "creating request")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", errors.Wrap(err, "making request to Grafana")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf(
			"unexpected status code: %d, body: %s",
			resp.StatusCode,
			readResponseBody(resp.Body),
		)
	}

	var response dashboardListResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, "", errors.Wrap(err, "decoding response")
	}

	dashboards := make([]Dashboard, 0, len(response.Items))
	for i := range response.Items {
		item := &response.Items[i]
		dashboards = append(dashboards, Dashboard{
			Name:              item.Metadata.Name,
			Namespace:         item.Metadata.Namespace,
			UID:               item.Metadata.UID,
			CreationTimestamp: item.Metadata.CreationTimestamp,
			Spec:              item.Spec,
		})
	}

	return dashboards, response.Metadata.Continue, nil
}

type deleteDashboardResponse struct {
	Status string `json:"status"`
}

// DeleteDashboard deletes a dashboard by its UID.
//
// DeleteDashboard uses the Grafana HTTP API endpoint DELETE /apis/dashboard.grafana.app/v1beta1/namespaces/:namespace/dashboards/:uid
// to delete a dashboard in Grafana v12.
//
// See https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#delete-dashboard.
func (c *Client) DeleteDashboard(ctx context.Context, uid string) error {
	if uid == "" {
		return errors.New("dashboard UID must not be empty")
	}

	u := c.endpoint.JoinPath("apis", "dashboard.grafana.app", "v1beta1", "namespaces", "default", "dashboards", uid)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), http.NoBody)
	if err != nil {
		return errors.Wrap(err, "creating request")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "making request to Grafana")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, readResponseBody(resp.Body))
	}

	var response deleteDashboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return errors.Wrap(err, "decoding response")
	}

	if response.Status != "Success" {
		return fmt.Errorf(
			"got response code %d from dashboard delete request but response body claims failure, "+
				"dashboard may have been deleted",
			http.StatusOK,
		)
	}

	return nil
}

func readResponseBody(r io.Reader) string {
	body, err := io.ReadAll(r)
	if err != nil {
		body = []byte(errors.Wrap(err, "could not read response body").Error())
	}

	return string(body)
}
