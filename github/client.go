package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v73/github"
	"github.com/pkg/errors"
)

// Client handles GitHub operations for backing up dashboards.
type Client struct {
	client     *github.Client
	repository Repository
	branch     string
	directory  string
	logger     *slog.Logger
}

// ClientOptions contains options for creating a Client.
type ClientOptions struct {
	Client     *github.Client
	Repository Repository
	Branch     string
	Directory  string
	Logger     *slog.Logger
}

// NewClient creates a new Client with authentication.
func NewClient(opts *ClientOptions) *Client {
	logger := opts.Logger.With(
		slog.String("repository", opts.Repository.Name()),
		slog.String("branch", opts.Branch),
		slog.String("directory", opts.Directory),
	)

	return &Client{
		client:     opts.Client,
		repository: opts.Repository,
		branch:     opts.Branch,
		directory:  opts.Directory,
		logger:     logger,
	}
}

// BackUpDashboard backs up a dashboard to the GitHub repository.
func (c *Client) BackUpDashboard(ctx context.Context, namespace, name string, dashboardJSON []byte) error {
	path := fmt.Sprintf("%s/%s/%s.json", c.directory, namespace, name)
	message := fmt.Sprintf("Back up deleted Grafana dashboard %s/%s", namespace, name)

	c.logger.Info("Backing up dashboard to GitHub",
		slog.String("path", path),
		slog.String("namespace", namespace),
		slog.String("name", name))

	fileContent, _, resp, err := c.client.Repositories.GetContents(
		ctx, c.repository.Owner(), c.repository.Repo(), path, &github.RepositoryContentGetOptions{
			Ref: c.branch,
		},
	)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return c.createFile(ctx, path, message, dashboardJSON)
		}
		return errors.Wrap(err, "checking if file exists")
	}

	return c.updateFile(ctx, path, message, dashboardJSON, fileContent.GetSHA())
}

func (c *Client) createFile(ctx context.Context, path, message string, content []byte) error {
	opts := &github.RepositoryContentFileOptions{
		Message: github.Ptr(message),
		Content: content,
		Branch:  github.Ptr(c.branch),
	}

	_, _, err := c.client.Repositories.CreateFile(ctx, c.repository.Owner(), c.repository.Repo(), path, opts)
	if err != nil {
		return errors.Wrap(err, "creating file")
	}

	c.logger.Info("Created dashboard backup file", slog.String("path", path))
	return nil
}

func (c *Client) updateFile(ctx context.Context, path, message string, content []byte, sha string) error {
	opts := &github.RepositoryContentFileOptions{
		Message: github.Ptr(message),
		Content: content,
		Branch:  github.Ptr(c.branch),
		SHA:     github.Ptr(sha),
	}

	_, _, err := c.client.Repositories.UpdateFile(ctx, c.repository.Owner(), c.repository.Repo(), path, opts)
	if err != nil {
		return errors.Wrap(err, "updating file")
	}

	c.logger.Info("Updated dashboard backup file", slog.String("path", path))
	return nil
}
