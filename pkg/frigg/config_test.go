package frigg_test

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/LasseHels/frigg/pkg/frigg"
	"github.com/LasseHels/frigg/pkg/server"
)

//go:embed testdata
var testdataFS embed.FS

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		configPath    string
		expectedError string
	}

	tests := map[string]testCase{
		"missing server host": {
			configPath:    "testdata/missing_host.yaml",
			expectedError: "Key: 'Config.Server.Host' Error:Field validation for 'Host' failed on the 'required' tag",
		},
		"missing server port": {
			configPath:    "testdata/missing_port.yaml",
			expectedError: "Key: 'Config.Server.Port' Error:Field validation for 'Port' failed on the 'required' tag",
		},
		"invalid server port": {
			configPath:    "testdata/invalid_port.yaml",
			expectedError: "Key: 'Config.Server.Port' Error:Field validation for 'Port' failed on the 'min' tag",
		},
		"valid config": {
			configPath:    "testdata/valid_config.yaml",
			expectedError: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configBytes, err := testdataFS.ReadFile(tt.configPath)
			require.NoError(t, err, "failed to read test file")

			var cfg frigg.Config
			err = yaml.Unmarshal(configBytes, &cfg)
			require.NoError(t, err, "failed to unmarshal config")

			err = cfg.Validate()

			if tt.expectedError == "" {
				assert.NoError(t, err, "expected no validation error")
			} else {
				assert.EqualError(t, err, tt.expectedError, "validation error does not match expected")
			}
		})
	}
}

func TestConfig_Load(t *testing.T) {
	type testCase struct {
		configPath    string
		expectedError string
	}

	tests := map[string]testCase{
		"invalid yaml": {
			configPath:    "testdata/invalid_yaml.yaml",
			expectedError: "parsing config file: yaml: line 1: did not find expected key",
		},
		"file does not exist": {
			configPath: "testdata/non_existent.yaml",
			expectedError: `reading config file at path "testdata/non_existent.yaml": open testdata/non_existent.yaml:` +
				` no such file or directory`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var cfg frigg.Config
			err := cfg.Load(tt.configPath)
			assert.ErrorContains(t, err, tt.expectedError, "error does not match expected")
		})
	}

	t.Run("valid config", func(t *testing.T) {
		t.Setenv("FRIGG_HOST", "pineapple")

		var cfg frigg.Config
		err := cfg.Load("testdata/valid_config.yaml")
		require.NoError(t, err)
		expectedConfig := frigg.Config{
			Server: server.Config{
				Host: "pineapple",
				Port: 9898,
			},
		}
		assert.Equal(t, expectedConfig, cfg)
	})
}
