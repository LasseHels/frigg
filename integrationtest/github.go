package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// GitHub starts an httptest.Server that emulates the GitHub API. The server responds only to requests for:
//   - GET /repos/:owner/:repo/contents/:path - Returns file metadata or 404 if file doesn't exist.
//   - PUT /repos/:owner/:repo/contents/:path - Creates or updates files.
//
// The server is automatically stopped by t.Cleanup().
//
// See [documentation].
//
// [documentation]: https://docs.github.com/en/rest/repos/contents
type GitHub struct {
	url      string
	t        testing.TB
	requests []*http.Request
}

// NewGitHub creates a new faux GitHub API server.
func NewGitHub(t testing.TB) *GitHub {
	t.Helper()

	g := &GitHub{
		t: t,
	}

	server := httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(server.Close)

	g.url = server.URL
	return g
}

// URL returns the base URL of the faux GitHub API server.
func (g *GitHub) URL() string {
	return g.url
}

// Requests returns all HTTP requests received by the faux GitHub API server.
func (g *GitHub) Requests() []*http.Request {
	return g.requests
}

func (g *GitHub) handle(w http.ResponseWriter, r *http.Request) {
	g.requests = append(g.requests, r)

	path := strings.TrimPrefix(r.URL.Path, "/api/v3")
	if path == r.URL.Path {
		path = r.URL.Path
	}

	if !strings.HasPrefix(path, "/repos/") {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(strings.TrimPrefix(path, "/repos/"), "/")
	if len(parts) < 4 || parts[2] != "contents" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		g.handleGetContents(w, r)
	case http.MethodPut:
		g.handlePutContents(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (g *GitHub) handleGetContents(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	response := map[string]string{
		"message":           "Not Found",
		"documentation_url": "https://docs.github.com/rest/repos/contents#get-repository-content",
	}
	err := json.NewEncoder(w).Encode(response)
	assert.NoError(g.t, err)
}

func (g *GitHub) handlePutContents(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_, err := w.Write([]byte("{}"))
	assert.NoError(g.t, err)
}
