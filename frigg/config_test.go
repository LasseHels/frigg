package frigg_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LasseHels/frigg/frigg"
	"github.com/LasseHels/frigg/github"
	"github.com/LasseHels/frigg/grafana"
	"github.com/LasseHels/frigg/log"
	"github.com/LasseHels/frigg/loki"
	"github.com/LasseHels/frigg/server"
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
			expectedError: "validating configuration: Key: 'Config.Loki' Error:" +
				"Field validation for 'Loki' failed on the 'required' tag; Key: 'Config.Grafana' Error:" +
				"Field validation for 'Grafana' failed on the 'required' tag; Key: 'Config.Prune.Period' Error:" +
				"Field validation for 'Period' failed on the 'required' tag; Key: 'Config.Prune.Labels' Error:" +
				"Field validation for 'Labels' failed on the 'required' tag; Key: 'Config.Backup.GitHub.Repository' Error:" +
				"Field validation for 'Repository' failed on the 'required' tag",
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
				Loki: loki.Config{
					Endpoint: "http://loki.example.com",
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: grafana.PruneConfig{
					Dry:          false,
					Interval:     5 * time.Minute,
					IgnoredUsers: []string{"admin"},
					Period:       720 * time.Hour,
					Labels: map[string]string{
						"app": "grafana",
						"env": "test",
					},
					LowerThreshold: 50,
					Skip: &grafana.SkipConfig{
						Tags: &grafana.SkipTagsConfig{
							Any: []string{"keep", "safeguard"},
						},
					},
					MaxDeletions: intPtr(25),
					ChunkSize:    4 * time.Hour,
				},
				Backup: frigg.BackupConfig{
					GitHub: github.Config{
						Repository: exampleRepository(t),
						Branch:     "backup-branch",
						Directory:  "archived-dashboards",
						APIURL:     "https://github.example.com/api/v3",
					},
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
				Loki: loki.Config{
					Endpoint: "http://loki.example.com",
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: grafana.PruneConfig{
					Dry:      true,
					Interval: 10 * time.Minute,
					Period:   720 * time.Hour,
					Labels: map[string]string{
						"app": "grafana",
					},
					LowerThreshold: 10,
					ChunkSize:      4 * time.Hour,
				},
				Backup: frigg.BackupConfig{
					GitHub: github.Config{
						Repository: exampleRepository(t),
						Branch:     "main",
						Directory:  "deleted-dashboards",
					},
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
				Loki: loki.Config{
					Endpoint: "http://loki.example.com",
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: grafana.PruneConfig{
					Dry:      true,
					Interval: 10 * time.Minute,
					Period:   720 * time.Hour,
					Labels: map[string]string{
						"app": "grafana",
					},
					LowerThreshold: 10,
					ChunkSize:      4 * time.Hour,
				},
				Backup: frigg.BackupConfig{
					GitHub: github.Config{
						Repository: exampleRepository(t),
						Branch:     "main",
						Directory:  "deleted-dashboards",
					},
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
			expectedError: "validating configuration: Key: 'Config.Grafana' Error:" +
				"Field validation for 'Grafana' failed on the 'required' tag",
		},
		"invalid grafana endpoint url": {
			configPath:     "testdata/invalid_grafana_endpoint.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Grafana.Endpoint' Error:" +
				"Field validation for 'Endpoint' failed on the 'url' tag",
		},
		"missing loki endpoint": {
			configPath:     "testdata/missing_loki_endpoint.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Loki' Error:" +
				"Field validation for 'Loki' failed on the 'required' tag",
		},
		"missing prune period": {
			configPath:     "testdata/missing_prune_period.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.Period' Error:" +
				"Field validation for 'Period' failed on the 'required' tag",
		},
		"missing prune labels": {
			configPath:     "testdata/missing_prune_labels.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.Labels' Error:" +
				"Field validation for 'Labels' failed on the 'required' tag",
		},
		"invalid prune interval": {
			configPath:     "testdata/invalid_prune_interval.yaml",
			expectedConfig: nil,
			expectedError: `loading configuration: parsing config file: yaml: unmarshal errors:` + "\n" +
				`  line 10: cannot unmarshal !!str ` + "`invalid...`" + ` into time.Duration`,
		},
		"invalid prune period": {
			configPath:     "testdata/invalid_prune_period.yaml",
			expectedConfig: nil,
			expectedError: `loading configuration: parsing config file: yaml: unmarshal errors:` + "\n" +
				`  line 13: cannot unmarshal !!str ` + "`invalid...`" + ` into time.Duration`,
		},
		"invalid prune lower threshold": {
			configPath:     "testdata/invalid_prune_lower_threshold.yaml",
			expectedConfig: nil,
			expectedError: "loading configuration: parsing config file: yaml: unmarshal " +
				"errors:\n  line 23: cannot unmarshal !!str `nope` into int",
		},
		"negative prune lower threshold": {
			configPath:     "testdata/negative_prune_lower_threshold.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.LowerThreshold' " +
				"Error:Field validation for 'LowerThreshold' failed on the 'min' tag",
		},
		"invalid backup repository format": {
			configPath:     "testdata/invalid_backup_repository.yaml",
			expectedConfig: nil,
			expectedError: "loading configuration: parsing config file: " +
				`repository must be in format 'owner/repo', got "invalid-format"`,
		},
		"backup with defaults": {
			configPath: "testdata/backup_with_defaults.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelInfo,
				},
				Server: server.Config{
					Host: "localhost",
					Port: 8080,
				},
				Loki: loki.Config{
					Endpoint: "http://loki.example.com",
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: grafana.PruneConfig{
					Dry:      true,
					Interval: 10 * time.Minute,
					Period:   720 * time.Hour,
					Labels: map[string]string{
						"app": "grafana",
					},
					LowerThreshold: 10,
					ChunkSize:      4 * time.Hour,
				},
				Backup: frigg.BackupConfig{
					GitHub: github.Config{
						Repository: exampleRepository(t),
						Branch:     "main",
						Directory:  "deleted-dashboards",
					},
				},
			},
			expectedError: "",
		},
		"missing backup repository": {
			configPath:     "testdata/missing_backup_repository.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Backup.GitHub.Repository' Error:" +
				"Field validation for 'Repository' failed on the 'required' tag",
		},
		"empty backup repository": {
			configPath:     "testdata/empty_backup_repository.yaml",
			expectedConfig: nil,
			expectedError:  "loading configuration: parsing config file: repository must be in format 'owner/repo', got \"\"",
		},
		"invalid github api url": {
			configPath:     "testdata/invalid_github_api_url.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Backup.GitHub.APIURL' Error:" +
				"Field validation for 'APIURL' failed on the 'url' tag",
		},
		"empty skip config": {
			configPath:     "testdata/empty_skip_config.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.Skip.Tags' Error:" +
				"Field validation for 'Tags' failed on the 'required' tag",
		},
		"empty skip tags config": {
			configPath:     "testdata/empty_skip_tags_config.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.Skip.Tags.Any' Error:" +
				"Field validation for 'Any' failed on the 'required' tag",
		},
		"empty skip tags any config": {
			configPath:     "testdata/empty_skip_tags_any_config.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.Skip.Tags.Any' Error:" +
				"Field validation for 'Any' failed on the 'min' tag",
		},
		"zero max deletions": {
			configPath:     "testdata/zero_max_deletions.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.MaxDeletions' Error:" +
				"Field validation for 'MaxDeletions' failed on the 'min' tag",
		},
		"negative max deletions": {
			configPath:     "testdata/negative_max_deletions.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.MaxDeletions' Error:" +
				"Field validation for 'MaxDeletions' failed on the 'min' tag",
		},
		"chunk size below minimum": {
			configPath:     "testdata/chunk_size_below_minimum.yaml",
			expectedConfig: nil,
			expectedError: "validating configuration: Key: 'Config.Prune.ChunkSize' Error:" +
				"Field validation for 'ChunkSize' failed on the 'min' tag",
		},
		"chunk size at minimum": {
			configPath: "testdata/chunk_size_at_minimum.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelInfo,
				},
				Server: server.Config{
					Host: "localhost",
					Port: 8080,
				},
				Loki: loki.Config{
					Endpoint: "http://loki.example.com",
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: grafana.PruneConfig{
					Dry:            true,
					Interval:       10 * time.Minute,
					Period:         720 * time.Hour,
					Labels:         map[string]string{"app": "grafana"},
					LowerThreshold: 10,
					ChunkSize:      10 * time.Minute,
				},
				Backup: frigg.BackupConfig{
					GitHub: github.Config{
						Repository: exampleRepository(t),
						Branch:     "main",
						Directory:  "deleted-dashboards",
					},
				},
			},
			expectedError: "",
		},
		"chunk size custom value": {
			configPath: "testdata/chunk_size_custom.yaml",
			expectedConfig: &frigg.Config{
				Log: log.Config{
					Level: slog.LevelInfo,
				},
				Server: server.Config{
					Host: "localhost",
					Port: 8080,
				},
				Loki: loki.Config{
					Endpoint: "http://loki.example.com",
				},
				Grafana: grafana.Config{
					Endpoint: "http://example.com",
				},
				Prune: grafana.PruneConfig{
					Dry:            true,
					Interval:       10 * time.Minute,
					Period:         720 * time.Hour,
					Labels:         map[string]string{"app": "grafana"},
					LowerThreshold: 10,
					ChunkSize:      2 * time.Hour,
				},
				Backup: frigg.BackupConfig{
					GitHub: github.Config{
						Repository: exampleRepository(t),
						Branch:     "main",
						Directory:  "deleted-dashboards",
					},
				},
			},
			expectedError: "",
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
			Loki: loki.Config{
				Endpoint: "http://loki.example.com",
			},
			Grafana: grafana.Config{
				Endpoint: "http://example.com",
			},
			Prune: grafana.PruneConfig{
				Dry:      true,
				Interval: 10 * time.Minute,
				Period:   720 * time.Hour,
				Labels: map[string]string{
					"app": "grafana",
				},
				LowerThreshold: 10,
				ChunkSize:      4 * time.Hour,
			},
			Backup: frigg.BackupConfig{
				GitHub: github.Config{
					Repository: exampleRepository(t),
					Branch:     "main",
					Directory:  "deleted-dashboards",
				},
			},
		}

		cfg, err := frigg.NewConfig("testdata/env_expansion_config.yaml")
		require.NoError(t, err)
		assert.Equal(t, expectedConfig, cfg)
	})
}

func TestNewSecrets(t *testing.T) {
	type testCase struct {
		secretsPath     string
		expectedSecrets *frigg.Secrets
		expectedError   string
	}

	tests := map[string]testCase{
		"valid secrets": {
			secretsPath: "testdata/valid_secrets.yaml",
			expectedSecrets: &frigg.Secrets{
				Grafana: grafana.Secrets{
					Tokens: map[string]string{
						"default": "example-valid-token",
					},
				},
				Backup: frigg.BackupSecrets{
					GitHub: github.Secrets{
						Token: "ghp_exampletoken123",
					},
				},
			},
			expectedError: "",
		},
		"missing secrets file": {
			secretsPath:     "testdata/nonexistent_secrets.yaml",
			expectedSecrets: nil,
			expectedError: `reading secrets file at path "testdata/nonexistent_secrets.yaml": ` +
				`open testdata/nonexistent_secrets.yaml: no such file or directory`,
		},
		"empty secrets file": {
			secretsPath:     "testdata/empty_secrets.yaml",
			expectedSecrets: nil,
			expectedError:   "parsing secrets file: EOF",
		},
		"invalid secrets yaml": {
			secretsPath:     "testdata/invalid_secrets.yaml",
			expectedSecrets: nil,
			expectedError:   "parsing secrets file: yaml: line 2: mapping values are not allowed in this context",
		},
		"missing grafana token in secrets": {
			secretsPath:     "testdata/missing_grafana_secrets.yaml",
			expectedSecrets: nil,
			expectedError: "validating secrets: Key: 'Secrets.Grafana.Tokens' Error:" +
				"Field validation for 'Tokens' failed on the 'min' tag",
		},
		"empty grafana token in secrets": {
			secretsPath:     "testdata/empty_token_secrets.yaml",
			expectedSecrets: nil,
			expectedError: "validating secrets: Key: 'Secrets.Grafana.Tokens' Error:" +
				"Field validation for 'Tokens' failed on the 'min' tag",
		},
		"empty key in tokens map": {
			secretsPath:     "testdata/empty_key_secrets.yaml",
			expectedSecrets: nil,
			expectedError: "validating secrets: Key: 'Secrets.Grafana.Tokens[]' Error:" +
				"Field validation for 'Tokens[]' failed on the 'required' tag",
		},
		"empty value in tokens map": {
			secretsPath:     "testdata/empty_value_secrets.yaml",
			expectedSecrets: nil,
			expectedError: "validating secrets: Key: 'Secrets.Grafana.Tokens[default]' Error:" +
				"Field validation for 'Tokens[default]' failed on the 'required' tag",
		},
		"missing backup token in secrets": {
			secretsPath:     "testdata/missing_backup_token_secrets.yaml",
			expectedSecrets: nil,
			expectedError: "validating secrets: Key: 'Secrets.Backup' Error:" +
				"Field validation for 'Backup' failed on the 'required' tag",
		},
		"empty backup token in secrets": {
			secretsPath:     "testdata/empty_backup_token_secrets.yaml",
			expectedSecrets: nil,
			expectedError: "validating secrets: Key: 'Secrets.Backup' Error:" +
				"Field validation for 'Backup' failed on the 'required' tag",
		},
		"invalid extension for secrets": {
			secretsPath:     "testdata/invalid_extension_secrets.xml",
			expectedSecrets: nil,
			expectedError: `parsing secrets file: unsupported file extension ".xml" for file at path` +
				` "testdata/invalid_extension_secrets.xml", supported extensions are .json, .yml and .yaml`,
		},
		"no extension for secrets": {
			secretsPath:     "testdata/no_extension_config",
			expectedSecrets: nil,
			expectedError: `parsing secrets file: unsupported file extension "" for file at path` +
				` "testdata/no_extension_config", supported extensions are .json, .yml and .yaml`,
		},
		"valid json secrets": {
			secretsPath: "testdata/valid_secrets.json",
			expectedSecrets: &frigg.Secrets{
				Grafana: grafana.Secrets{
					Tokens: map[string]string{
						"default": "example-valid-token",
					},
				},
				Backup: frigg.BackupSecrets{
					GitHub: github.Secrets{
						Token: "ghp_exampletoken123",
					},
				},
			},
			expectedError: "",
		},
		"valid yml extension secrets": {
			secretsPath: "testdata/valid_secrets.yaml",
			expectedSecrets: &frigg.Secrets{
				Grafana: grafana.Secrets{
					Tokens: map[string]string{
						"default": "example-valid-token",
					},
				},
				Backup: frigg.BackupSecrets{
					GitHub: github.Secrets{
						Token: "ghp_exampletoken123",
					},
				},
			},
			expectedError: "",
		},
		"malformed json secrets": {
			secretsPath:     "testdata/malformed_secrets.json",
			expectedSecrets: nil,
			expectedError:   "parsing secrets file: invalid character '\"' after object key:value pair",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			secrets, err := frigg.NewSecrets(tt.secretsPath)

			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError, name)
				assert.Nil(t, secrets, name)
			} else {
				require.NoError(t, err, name)
				assert.Equal(t, tt.expectedSecrets, secrets, name)
			}
		})
	}
}

func TestConfig_Initialise_ChunkSizeTruncation(t *testing.T) {
	t.Parallel()

	t.Run("truncates chunk size to period when chunk size exceeds period", func(t *testing.T) {
		t.Parallel()

		cfg, err := frigg.NewConfig("testdata/chunk_size_exceeds_period.yaml")
		require.NoError(t, err)

		assert.Equal(t, 2*time.Hour, cfg.Prune.ChunkSize)
		assert.Equal(t, time.Hour, cfg.Prune.Period)

		var logBuf bytes.Buffer
		handler := slog.NewJSONHandler(&logBuf, nil)
		logger := slog.New(handler)

		secrets := &frigg.Secrets{
			Grafana: grafana.Secrets{
				Tokens: map[string]string{
					"default": "test-token",
				},
			},
			Backup: frigg.BackupSecrets{
				GitHub: github.Secrets{
					Token: "ghp_test",
				},
			},
		}

		_, err = cfg.Initialise(logger, nil, secrets)
		require.NoError(t, err)

		assert.Equal(t, time.Hour, cfg.Prune.ChunkSize)

		logOutput := logBuf.String()
		assert.Contains(t, logOutput, "Chunk size exceeds period, truncating to period")
		assert.Contains(t, logOutput, `"configured_chunk_size":7200000000000`)
		assert.Contains(t, logOutput, `"period":3600000000000`)
	})

	t.Run("does not truncate when chunk size equals period", func(t *testing.T) {
		t.Parallel()

		cfg, err := frigg.NewConfig("testdata/chunk_size_at_minimum.yaml")
		require.NoError(t, err)

		cfg.Prune.ChunkSize = cfg.Prune.Period

		var logBuf bytes.Buffer
		handler := slog.NewJSONHandler(&logBuf, nil)
		logger := slog.New(handler)

		secrets := &frigg.Secrets{
			Grafana: grafana.Secrets{
				Tokens: map[string]string{
					"default": "test-token",
				},
			},
			Backup: frigg.BackupSecrets{
				GitHub: github.Secrets{
					Token: "ghp_test",
				},
			},
		}

		_, err = cfg.Initialise(logger, nil, secrets)
		require.NoError(t, err)

		assert.Equal(t, cfg.Prune.Period, cfg.Prune.ChunkSize)

		logOutput := logBuf.String()
		assert.NotContains(t, logOutput, "Chunk size exceeds period")
	})

	t.Run("does not truncate when chunk size is less than period", func(t *testing.T) {
		t.Parallel()

		cfg, err := frigg.NewConfig("testdata/chunk_size_custom.yaml")
		require.NoError(t, err)

		originalChunkSize := cfg.Prune.ChunkSize

		var logBuf bytes.Buffer
		handler := slog.NewJSONHandler(&logBuf, nil)
		logger := slog.New(handler)

		secrets := &frigg.Secrets{
			Grafana: grafana.Secrets{
				Tokens: map[string]string{
					"default": "test-token",
				},
			},
			Backup: frigg.BackupSecrets{
				GitHub: github.Secrets{
					Token: "ghp_test",
				},
			},
		}

		_, err = cfg.Initialise(logger, nil, secrets)
		require.NoError(t, err)

		assert.Equal(t, originalChunkSize, cfg.Prune.ChunkSize)

		logOutput := logBuf.String()
		assert.NotContains(t, logOutput, "Chunk size exceeds period")
	})
}

func exampleRepository(t testing.TB) github.Repository {
	t.Helper()
	r, err := github.NewRepository("octocat", "hello-world")
	require.NoError(t, err)
	return *r
}

func intPtr(i int) *int {
	return &i
}
