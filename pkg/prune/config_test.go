package prune_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"

	"github.com/LasseHels/frigg/pkg/prune"
)

func TestDuration_UnmarshalYAML(t *testing.T) {
	type testCase struct {
		input       string
		expected    time.Duration
		expectError bool
	}

	tests := map[string]testCase{
		"standard go duration minutes": {
			input:    "5m",
			expected: 5 * time.Minute,
		},
		"standard go duration hours": {
			input:    "2h",
			expected: 2 * time.Hour,
		},
		"days format": {
			input:    "30d",
			expected: 30 * 24 * time.Hour,
		},
		"single day": {
			input:    "1d",
			expected: 24 * time.Hour,
		},
		"invalid duration": {
			input:       "invalid-duration",
			expectError: true,
		},
		"empty string": {
			input:    "",
			expected: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var d prune.Duration
			err := yaml.Unmarshal([]byte(tt.input), &d)

			if tt.expectError {
				assert.Error(t, err, name)
			} else {
				assert.NoError(t, err, name)
				assert.Equal(t, tt.expected, d.ToDuration(), name)
			}
		})
	}
}

func TestConfig_YAMLUnmarshaling(t *testing.T) {
	yamlData := `
dry: false
interval: "5m"
ignored_users:
  - "admin"
  - "service"
period: "30d"
labels:
  app: "grafana"
  env: "test"
`

	var config prune.Config
	err := yaml.Unmarshal([]byte(yamlData), &config)
	assert.NoError(t, err)

	assert.False(t, config.Dry)
	assert.Equal(t, 5*time.Minute, config.Interval.ToDuration())
	assert.Equal(t, []string{"admin", "service"}, config.IgnoredUsers)
	assert.Equal(t, 30*24*time.Hour, config.Period.ToDuration())
	assert.Equal(t, map[string]string{"app": "grafana", "env": "test"}, config.Labels)
}