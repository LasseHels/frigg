package frigg_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/pkg/frigg"
	"github.com/LasseHels/frigg/pkg/grafana"
	"github.com/LasseHels/frigg/pkg/log"
	"github.com/LasseHels/frigg/pkg/prune"
	"github.com/LasseHels/frigg/pkg/server"
)

func TestNewConfig(t *testing.T) {
	type testCase struct {
		configPath     string
		expectedConfig *frigg.Config
		expectedError  string
	}

	tests := map[string]testCase{
		"empty config file": {
			configPath:     "testdata/empty_config.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Grafana.Endpoint' Error:" +
				"Field validation for 'Endpoint' failed on the 'required' tag",
		},
		"valid config": {
			configPath: "testdata/valid_config.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelError,
				},
				Server: server.Config{
					Host: "pomelo.com",
					Port: 9898,
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: prune.Config{
					Dry:      true,
					Interval: prune.Duration(10 * time.Minute),
				},
			},
			expectedError: "",
		},
		"missing host": {
			configPath: "testdata/missing_host.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelInfo,
				},
				Server: server.Config{
					Host: "localhost",
					Port: 9876,
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: prune.Config{
					Dry:      true,
					Interval: prune.Duration(10 * time.Minute),
				},
			},
			expectedError: "",
		},
		"missing port": {
			configPath: "testdata/missing_port.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelInfo,
				},
				Server: server.Config{
					Host: "pineapple.com",
					Port: 8080,
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: prune.Config{
					Dry:      true,
					Interval: prune.Duration(10 * time.Minute),
				},
			},
			expectedError: "",
		},
		"invalid port": {
			configPath:     "testdata/invalid_port.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Server.Port' Error:" +
				"Field validation for 'Port' failed on the 'min' tag",
		},
		"invalid log level": {
			configPath:     "testdata/invalid_log_level.yaml",
			expectedConfig: nil,
			expectedError:  `loading configuration: parsing config file: slog: level string "BOGUS": unknown name`,
		},
		"invalid yaml": {
			configPath:     "testdata/invalid_yaml.yaml",
			expectedConfig: nil,
			expectedError:  "loading configuration: parsing config file: yaml: line 1: did not find expected key",
		},
		"empty path": {
			configPath:     "",
			expectedConfig: nil,
			expectedError:  `loading configuration: reading config file at path "": open : no such file or directory`,
		},
		"file not found": {
			configPath:     "testdata/nonexistent.yaml",
			expectedConfig: nil,
			expectedError: `loading configuration: reading config file at path "testdata/nonexistent.yaml":` +
				` open testdata/nonexistent.yaml: no such file or directory`,
		},
		"missing grafana endpoint": {
			configPath:     "testdata/missing_grafana_endpoint.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Grafana.Endpoint' Error:" +
				"Field validation for 'Endpoint' failed on the 'required' tag",
		},
		"invalid grafana endpoint url": {
			configPath:     "testdata/invalid_grafana_endpoint.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Grafana.Endpoint' Error:" +
				"Field validation for 'Endpoint' failed on the 'url' tag",
		},
		"valid prune config": {
			configPath: "testdata/valid_prune_config.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelInfo,
				},
				Server: server.Config{
					Host: "test.com",
					Port: 8080,
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: prune.Config{
					Dry:          false,
					Interval:     prune.Duration(5 * time.Minute),
					IgnoredUsers: []string{"admin"},
					Period:       prune.Duration(30 * 24 * time.Hour),
					Labels: map[string]string{
						"app": "grafana",
						"env": "test",
					},
				},
			},
			expectedError: "",
		},
		"missing prune period": {
			configPath:     "testdata/missing_prune_period.yaml",
			expectedConfig: nil,
			expectedError:  "validating configuration: validating prune configuration: prune.period is required when prune configuration is used",
		},
		"missing prune labels": {
			configPath:     "testdata/missing_prune_labels.yaml",
			expectedConfig: nil,
			expectedError:  "validating configuration: validating prune configuration: prune.labels is required when prune configuration is used",
		},
		"invalid prune interval": {
			configPath:     "testdata/invalid_prune_interval.yaml",
			expectedConfig: nil,
			expectedError:  `loading configuration: parsing config file: invalid duration "invalid-duration": time: invalid duration "invalid-duration"`,
		},
		"invalid prune period": {
			configPath:     "testdata/invalid_prune_period.yaml",
			expectedConfig: nil,
			expectedError:  `loading configuration: parsing config file: invalid duration "invalid-duration": time: invalid duration "invalid-duration"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := frigg.NewConfig(tt.configPath)
			assert.Equal(t, tt.expectedConfig, cfg, name)

			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError, name)
			} else {
				require.NoError(t, err, name)
			}
		})
	}

	t.Run("expands environment variables", func(t *testing.T) {
		t.Setenv("FRIGG_HOST", "banana.com")
		expectedConfig := &frigg.Config{
			Log: log.Config{
				Level: slog.LevelInfo,
			},
			Server: server.Config{
				Host: "banana.com",
				Port: 1111,
			},
			Grafana: grafana.Config{
				Endpoint: "http://example.com",
			},
			Prune: prune.Config{
				Dry:      true,
				Interval: prune.Duration(10 * time.Minute),
			},
		}

		cfg, err := frigg.NewConfig("testdata/env_expansion_config.yaml")
		require.NoError(t, err)
		assert.Equal(t, expectedConfig, cfg)
	})
}
