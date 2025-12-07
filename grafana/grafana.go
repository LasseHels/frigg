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

	"github.com/LasseHels/frigg/loki"
)

type client interface {
	QueryRange(ctx context.Context, query string, start, end time.Time) ([]loki.Log, error)
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type storage interface {
	BackUpDashboard(ctx context.Context, namespace, name string, dashboardJSON []byte) error
}

type Client struct {
	logger     *slog.Logger
	client     client
	httpClient httpClient
	endpoint   url.URL
	token      string
	storage    storage
}

type NewClientOptions struct {
	Logger     *slog.Logger
	Client     client
	HTTPClient httpClient
	Endpoint   url.URL // Endpoint where Grafana can be reached, e.g., "https://grafana.example.com".
	// Token used when authenticating with Grafana's HTTP API.
	// Token is expected to have permissions to:
	// - List dashboards in the Grafana instance. Frigg can only evaluate usage of dashboards that it can list.
	//   Dashboards that Token cannot list are not evaluated.
	// - Delete dashboards.
	Token   string
	Storage storage
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
		storage:    opts.Storage,
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
	name      string
	namespace string
	reads     int
	users     int
}

// Name of the dashboard.
func (d *DashboardReads) Name() string {
	return d.name
}

// Namespace of the dashboard.
func (d *DashboardReads) Namespace() string {
	return d.namespace
}

// Reads is the number of times the dashboard has been read.
func (d *DashboardReads) Reads() int {
	return d.reads
}

// Users is the number of unique users that have read the dashboard.
func (d *DashboardReads) Users() int {
	return d.users
}

func (d *DashboardReads) Key() DashboardKey {
	return DashboardKey{
		name:      d.name,
		namespace: d.namespace,
	}
}

// DashboardKey uniquely identifies a dashboard in Grafana by its combined name and namespace.
type DashboardKey struct {
	name      string
	namespace string
}

// extractPathVariables from a string in the format
// "/apis/dashboard.grafana.app/v1beta1/namespaces/:namespace/dashboards/:uid" or
// "/apis/dashboard.grafana.app/v1beta1/namespaces/:namespace/dashboards/:uid/dto".
//
// The former path comes from requests to Grafana's [Get Dashboard] API endpoint. The latter path comes from users
// viewing dashboards in the Grafana UI, which [makes a request to Grafana's internal /dto endpoint] to fetch dashboard
// data.
//
// For some reason that is not fully clear to me, the path parameter is called :uid, but it actually refers to the
// dashboard's name. See [Name].
//
// A dashboard in Grafana v12 is uniquely identified by its combined name and namespace.
//
// See also [API Path Structure].
//
// [Get Dashboard]: https://grafana.com/docs/grafana/v12.2/developer-resources/api-reference/http-api/dashboard/#get-dashboard
// [makes a request to Grafana's internal /dto endpoint]: https://github.com/grafana/grafana/blob/v12.2.0/public/app/features/dashboard/api/v2.ts#L46
// [Name]: https://grafana.com/docs/grafana/v12.0/developers/http_api/apis/#name-
// [API Path Structure]: https://grafana.com/docs/grafana/v12.0/developers/http_api/apis/#api-path-structure
//
//nolint:lll
func extractPathVariables(path string) (DashboardKey, error) {
	path = strings.TrimSuffix(path, "/dto")

	pathParts := strings.Split(path, "/")
	expectedFormat := "/apis/dashboard.grafana.app/v1beta1/namespaces/:namespace/dashboards/:uid"
	err := fmt.Errorf("unexpected path format: %q, expected format %q", path, expectedFormat)

	expectedPartCount := 8
	actualPartCount := len(pathParts)
	if actualPartCount != expectedPartCount {
		return DashboardKey{}, errors.Wrapf(err, "expected part count %d but got %d", expectedPartCount, actualPartCount)
	}

	expectedThirdPart := "dashboard.grafana.app"
	actualThirdPart := pathParts[2]
	if actualThirdPart != expectedThirdPart {
		return DashboardKey{}, errors.Wrapf(err, "expected third part %q but got %q", expectedThirdPart, actualThirdPart)
	}

	expectedFifthPart := "namespaces"
	actualFifthPart := pathParts[4]
	if actualFifthPart != expectedFifthPart {
		return DashboardKey{}, errors.Wrapf(err, "expected fifth part %q but got %q", expectedFifthPart, actualFifthPart)
	}

	expectedSeventhPart := "dashboards"
	actualSeventhPart := pathParts[6]
	if actualSeventhPart != expectedSeventhPart {
		return DashboardKey{}, errors.Wrapf(
			err,
			"expected seventh part %q but got %q",
			expectedSeventhPart,
			actualSeventhPart,
		)
	}

	name := pathParts[7]
	namespace := pathParts[5]

	return DashboardKey{
		name:      name,
		namespace: namespace,
	}, nil
}

// UsedDashboards returns information about dashboard usage in range (now() - r) to now().
//
// A used dashboard is one that has been read by an un-ignored user (see UsedDashboardsOptions.IgnoredUsers) in the
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
|= "/apis/dashboard.grafana.app/"
|= "/namespaces/"
|= "/dashboards/"
|= "Request Completed"
| logfmt
| method = "GET"
| handler = "/apis/*"`, labelStr)

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

	readsByUID := make(map[DashboardKey]map[string]struct{})
	readCounts := make(map[DashboardKey]int)

	for _, log := range logs {
		stream := log.Stream()

		path, ok := stream["path"]
		if !ok {
			return nil, fmt.Errorf("could not find path in stream labels: %v", stream)
		}

		key, err := extractPathVariables(path)
		if err != nil {
			return nil, errors.Wrapf(err, "extracting variables from path %q", path)
		}

		user, ok := stream["uname"]
		if !ok {
			return nil, fmt.Errorf("could not find uname in stream labels: %v", stream)
		}

		if _, ignored := ignoredUsers[user]; ignored {
			continue
		}

		readCounts[key]++

		if _, exists := readsByUID[key]; !exists {
			readsByUID[key] = make(map[string]struct{})
		}
		readsByUID[key][user] = struct{}{}
	}

	result := make([]DashboardReads, 0, len(readsByUID))
	for vars, users := range readsByUID {
		result = append(result, DashboardReads{
			name:      vars.name,
			namespace: vars.namespace,
			reads:     readCounts[vars],
			users:     len(users),
		})
	}

	// The result slice is created from a map with no guaranteed order, so we sort it by dashboard name for consistency.
	sort.Slice(result, func(i, j int) bool {
		// Dashboard name are unique only within their namespace. If two dashboards have the same name,
		// we sort by namespace.
		if result[i].name == result[j].name {
			return result[i].namespace < result[j].namespace
		}

		return result[i].name < result[j].name
	})

	return result, nil
}

type Dashboard struct {
	Name              string          `json:"name"`
	Namespace         string          `json:"namespace"`
	UID               string          `json:"uid"`
	CreationTimestamp time.Time       `json:"creationTimestamp"`
	Title             string          `json:"title"`
	Spec              json.RawMessage `json:"spec"`
	ManagedBy         *string         `json:"managedBy,omitempty"`
}

func (d *Dashboard) Key() DashboardKey {
	return DashboardKey{
		name:      d.Name,
		namespace: d.Namespace,
	}
}

// Provisioned returns true if the dashboard is [provisioned].
//
// [provisioned]: https://grafana.com/docs/grafana/v12.2/administration/provisioning
func (d *Dashboard) Provisioned() bool {
	return d.ManagedBy != nil
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
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	Annotations       map[string]string `json:"annotations"`
}

type dashboardSpec struct {
	Title string `json:"title"`
}

// AllDashboards returns all dashboards from the specified namespace in the Grafana instance.
//
// AllDashboards uses the Grafana HTTP API endpoint "List dashboards" to fetch all dashboards.
// See https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#list-dashboards.
//
// AllDashboards handles pagination automatically and fetches all pages.
func (c *Client) AllDashboards(ctx context.Context, namespace string) ([]Dashboard, error) {
	var allDashboards []Dashboard
	pageSize := 500
	continueToken := ""

	for {
		dashboards, nextContinueToken, err := c.dashboardsPage(ctx, namespace, pageSize, continueToken)
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
func (c *Client) dashboardsPage(
	ctx context.Context,
	namespace string,
	limit int,
	continueToken string,
) ([]Dashboard, string, error) {
	u := c.endpoint.JoinPath("apis", "dashboard.grafana.app", "v1beta1", "namespaces", namespace, "dashboards")

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

		var spec dashboardSpec
		title := ""
		if err := json.Unmarshal(item.Spec, &spec); err == nil {
			title = spec.Title
		}

		var managedBy *string
		if value, ok := item.Metadata.Annotations["grafana.app/managedBy"]; ok {
			managedBy = &value
		}

		dashboards = append(dashboards, Dashboard{
			Name:              item.Metadata.Name,
			Namespace:         item.Metadata.Namespace,
			UID:               item.Metadata.UID,
			CreationTimestamp: item.Metadata.CreationTimestamp,
			Title:             title,
			Spec:              item.Spec,
			ManagedBy:         managedBy,
		})
	}

	return dashboards, response.Metadata.Continue, nil
}

type deleteDashboardResponse struct {
	Status string `json:"status"`
}

// DeleteDashboard backs up and then deletes a dashboard.
//
// The dashboard JSON is backed up using the configured storage before deletion. If the backup fails, the dashboard is
// not deleted and an error is returned.
//
// DeleteDashboard uses the Grafana HTTP API endpoint DELETE
// /apis/dashboard.grafana.app/v1beta1/namespaces/:namespace/dashboards/:uid to delete a dashboard in Grafana v12.
//
// See [documentation].
//
// [documentation]: https://grafana.com/docs/grafana/v12.0/developers/http_api/dashboard/#delete-dashboard
func (c *Client) DeleteDashboard(ctx context.Context, namespace, name string, dashboardJSON []byte) error {
	if name == "" {
		return errors.New("dashboard name must not be empty")
	}

	if err := c.storage.BackUpDashboard(ctx, namespace, name, dashboardJSON); err != nil {
		return errors.Wrap(err, "backing up dashboard")
	}

	u := c.endpoint.JoinPath("apis", "dashboard.grafana.app", "v1beta1", "namespaces", namespace, "dashboards", name)

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
