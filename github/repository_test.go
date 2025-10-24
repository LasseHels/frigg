package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewRepository(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		owner   string
		repo    string
		wantErr string
	}{
		"valid repository": {
			owner: "octocat",
			repo:  "hello-world",
		},
		"empty owner": {
			owner:   "",
			repo:    "hello-world",
			wantErr: "owner cannot be empty",
		},
		"empty repo": {
			owner:   "octocat",
			repo:    "",
			wantErr: "repo cannot be empty",
		},
		"both empty": {
			owner:   "",
			repo:    "",
			wantErr: "owner cannot be empty",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, err := NewRepository(tc.owner, tc.repo)

			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				assert.Nil(t, r)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, tc.owner, r.Owner())
			assert.Equal(t, tc.repo, r.Repo())
		})
	}
}

func TestRepository_UnmarshalYAML(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input     string
		wantOwner string
		wantRepo  string
		wantErr   string
	}{
		"valid repository": {
			input:     "repository: octocat/hello-world",
			wantOwner: "octocat",
			wantRepo:  "hello-world",
		},
		"repository with hyphen": {
			input:     "repository: my-org/my-repo",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		"repository with underscore": {
			input:     "repository: my_org/my_repo",
			wantOwner: "my_org",
			wantRepo:  "my_repo",
		},
		"missing slash": {
			input:   "repository: octocat-hello-world",
			wantErr: `repository must be in format 'owner/repo', got "octocat-hello-world"`,
		},
		"multiple slashes": {
			input:   "repository: octocat/hello/world",
			wantErr: `repository must be in format 'owner/repo', got "octocat/hello/world"`,
		},
		"empty owner": {
			input:   "repository: /hello-world",
			wantErr: "owner cannot be empty",
		},
		"empty repo": {
			input:   "repository: octocat/",
			wantErr: "repo cannot be empty",
		},
		"only slash": {
			input:   "repository: /",
			wantErr: "owner cannot be empty",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var config struct {
				Repository Repository `yaml:"repository"`
			}

			err := yaml.Unmarshal([]byte(tc.input), &config)

			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantOwner, config.Repository.Owner())
			assert.Equal(t, tc.wantRepo, config.Repository.Repo())
		})
	}
}
