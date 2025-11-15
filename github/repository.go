package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Secrets struct {
	Token string `yaml:"token" json:"token" validate:"required"`
}

type Config struct {
	Repository Repository `yaml:"repository" json:"repository" validate:"required"`
	Branch     string     `yaml:"branch" json:"branch" validate:"required"`
	Directory  string     `yaml:"directory" json:"directory" validate:"required"`
	APIURL     string     `yaml:"api_url" json:"api_url" validate:"omitempty,url"` //nolint:lll,revive // omitempty is valid for validator
}

// Repository represents a GitHub repository in "owner/repo" format.
type Repository struct {
	owner string
	repo  string
}

// NewRepository creates a new Repository with the given owner and repo. NewRepository returns an error if owner or repo
// is empty.
func NewRepository(owner, repo string) (*Repository, error) {
	if owner == "" {
		return nil, errors.New("owner cannot be empty")
	}
	if repo == "" {
		return nil, errors.New("repo cannot be empty")
	}
	return &Repository{
		owner: owner,
		repo:  repo,
	}, nil
}

// Owner returns the repository owner.
func (r *Repository) Owner() string {
	return r.owner
}

// Repo returns the repository name.
func (r *Repository) Repo() string {
	return r.repo
}

// Name returns the full repository name in "owner/repo" format.
func (r *Repository) Name() string {
	return r.owner + "/" + r.repo
}

// UnmarshalYAML implements yaml.Unmarshaler for parsing "owner/repo" format.
func (r *Repository) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return r.unmarshal(s)
}

// UnmarshalJSON implements json.Unmarshaler for parsing "owner/repo" format.
func (r *Repository) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return r.unmarshal(s)
}

func (r *Repository) unmarshal(s string) error {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return fmt.Errorf("repository must be in format 'owner/repo', got %q", s)
	}

	if parts[0] == "" {
		return errors.New("owner cannot be empty")
	}
	if parts[1] == "" {
		return errors.New("repo cannot be empty")
	}

	r.owner = parts[0]
	r.repo = parts[1]
	return nil
}
