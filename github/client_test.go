package github_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"testing"

	gogithub "github.com/google/go-github/v73/github"
	"github.com/migueleliasweb/go-github-mock/src/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/github"
)

func TestClient_BackUpDashboard(t *testing.T) {
	t.Parallel()

	dashboardJSON := []byte(`{"dashboard": "test"}`)
	owner := "test-owner"
	repo := "test-repo"
	repository, err := github.NewRepository(owner, repo)
	require.NoError(t, err)
	branch := "main"
	directory := "deleted-dashboards"
	namespace := "test-namespace"
	dashboardName := "test-dashboard"
	expectedPath := "/repos/test-owner/test-repo/contents/deleted-dashboards/test-namespace/test-dashboard.json"

	tests := map[string]struct {
		setupMockHandler func(t *testing.T) http.HandlerFunc
		wantErr          string
	}{
		"creates new file when file does not exist": {
			setupMockHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPut {
						w.WriteHeader(http.StatusNotFound)
						return
					}

					expectedBody := `{"message":"Back up deleted Grafana dashboard test-namespace/test-dashboard",` +
						`"content":"eyJkYXNoYm9hcmQiOiAidGVzdCJ9","branch":"main"}
`
					body := readBody(t, r)
					assert.Equal(t, expectedBody, body)
					assert.Equal(t, expectedPath, r.URL.Path)

					w.WriteHeader(http.StatusCreated)
					writeResponse(t, w, []byte(`{"content":{}}`))
				}
			},
		},
		"updates existing file when file exists": {
			setupMockHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodGet:
						w.WriteHeader(http.StatusOK)
						writeResponse(t, w, []byte(`{"sha":"existing-sha"}`))
					case http.MethodPut:
						body := readBody(t, r)
						expectedBody := `{"message":"Back up deleted Grafana dashboard test-namespace/test-dashboard",` +
							`"content":"eyJkYXNoYm9hcmQiOiAidGVzdCJ9","sha":"existing-sha","branch":"main"}
`
						assert.Equal(t, expectedBody, body)
						assert.Equal(t, expectedPath, r.URL.Path)

						w.WriteHeader(http.StatusOK)
						writeResponse(t, w, []byte(`{"content":{}}`))
					default:
						w.WriteHeader(http.StatusMethodNotAllowed)
					}
				}
			},
		},
		"returns error when GetContents fails with non-404": {
			setupMockHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			wantErr: "checking if file exists: GET",
		},
		"returns error when CreateFile fails": {
			setupMockHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPut {
						w.WriteHeader(http.StatusInternalServerError)
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}
			},
			wantErr: "creating file: PUT",
		},
		"returns error when UpdateFile fails": {
			setupMockHandler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodGet:
						w.WriteHeader(http.StatusOK)
						writeResponse(t, w, []byte(`{"sha":"existing-sha"}`))
					case http.MethodPut:
						w.WriteHeader(http.StatusInternalServerError)
					default:
						w.WriteHeader(http.StatusMethodNotAllowed)
					}
				}
			},
			wantErr: "updating file: PUT",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := tc.setupMockHandler(t)
			mockedHTTPClient := mock.NewMockedHTTPClient(
				mock.WithRequestMatchHandler(
					mock.GetReposContentsByOwnerByRepoByPath,
					handler,
				),
				mock.WithRequestMatchHandler(
					mock.PutReposContentsByOwnerByRepoByPath,
					handler,
				),
			)

			logger, _ := testLogger()
			client := github.NewClient(&github.ClientOptions{
				Client:     gogithub.NewClient(mockedHTTPClient).WithAuthToken("test-token"),
				Repository: *repository,
				Branch:     branch,
				Directory:  directory,
				Logger:     logger,
			})

			err := client.BackUpDashboard(t.Context(), namespace, dashboardName, dashboardJSON)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	replaceTime := func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}

		return a
	}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: replaceTime,
		Level:       slog.LevelDebug,
	})
	l := slog.New(handler)

	return l, buf
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return string(body)
}

func writeResponse(t *testing.T, w http.ResponseWriter, data []byte) {
	t.Helper()
	_, err := w.Write(data)
	assert.NoError(t, err)
}
