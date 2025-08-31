# Frigg

Frigg analyses Grafana dashboard usage and deletes unused dashboards

## Configuration

Frigg is configured using a YAML configuration file and a YAML secrets file. The paths to these files are provided using the `-config.file` and `-secrets.file` flags when starting Frigg:
```bash
frigg -config.file=/path/to/config.yaml -secrets.file=/path/to/secrets.yaml
```

Both flags are required. Frigg will fail to start if either flag is missing or points to a non-existent file.

### Configuration File Structure

Below is a complete example of Frigg's configuration file structure:
```yaml
log:
  # The log level to use (default: "INFO").
  # Valid values: DEBUG, INFO, WARN, ERROR.
  level: INFO

server:
  # The hostname or IP address to bind the server to (default: "localhost").
  host: localhost
  # The port number to listen on (default: 8080).
  port: 8080

grafana:
    # Endpoint where Grafana can be reached. This endpoint is used to read and delete dashboards via Grafana's API.
    # Frigg automatically appends API path elements to this endpoint and expects the value of the configuration option
    # to be the base URL of the Grafana instance. In other words, pass 'https://grafana.example.com' instead of
    # 'https://grafana.example.com/apis'.
    #
    # The value of endpoint must be a valid URL according to Go's url.Parse() function.
    #
    # Required.
    endpoint: 'https://grafana.example.com'

prune:
  # If dry is set to true, the dashboard pruner will only log unused dashboards instead of deleting them (default: true).
  dry: true
  # The interval with which the dashboard pruner will search for unused dashboards.
  # Regardless of the value of interval, the dashboard pruner will always run once immediately after Frigg has started.
  # This value must be a valid Go duration string (default: "10m").
  interval: '10m'
  # Ignored users whose reads do not count toward the usage of a dashboard. This option can be used to ignore reads
  # from service accounts that regularly read many or all dashboards.
  # Values are case-sensitive (default: []).
  ignored_users:
    - 'some-admin'
    - 'a-service-account'
  # The period of time in the past to include reads. For example, when setting period to '720h', only reads from the last
  # 720 hours (30 days) will count towards dashboard usage. IMPORTANT: Frigg does not take into account the retention period of
  # logs in Loki. Setting period to an amount greater than Loki's retention period will not cause an error and is
  # discouraged.
  #
  # This value must be a valid Go duration string.
  #
  # Required.
  period: '1440h'
  # Labels that identify Grafana logs in Loki. For example, if labels are set to app: 'grafana' and env: 'production',
  # then Frigg will query Grafana logs in Loki with the selector {app="grafana", env="production"}.
  #
  # Required.
  labels:
    app: 'grafana'
    env: 'production'
  # Lower threshold under which pruning is cancelled. If fewer than lower_threshold logs are found when pruning
  # dashboards, an error is returned and pruning stops.
  #
  # Since Grafana doesn't expose a formal API for dashboard usage, Frigg uses Grafana's logs as an API. This is
  # dubious as Grafana makes no promise that the format of its logs will remain stable. If a Grafana update causes
  # the format of logs upon which Frigg relies to change, then we'd prefer for Frigg to fail fast rather than
  # erroneously consider all dashboards unused. In other words, lower threshold is a safety mechanism to prevent Frigg
  # from deleting all dashboards.
  #
  # Must be greater than or equal to 0 (default: 10).
  lower_threshold: 10
```

### Secrets File Structure

```yaml
grafana:
    # Token used to authenticate with Grafana's API.
    # This token must have permissions to list and delete dashboards.
    #
    # Required.
    token: 'your-grafana-api-token-here'
```

### Environment Variable Expansion

Frigg's YAML configuration supports [environment variable expansion](https://pkg.go.dev/os#ExpandEnv) in the configuration file.
Use `${VAR_NAME}` syntax to include environment variables:
```yaml
server:
  host: ${FRIGG_HOST}
  port: 9000
```

If `FRIGG_HOST` is set to `example.com`, Frigg will use `example.com` as the server host.

> [!NOTE]
> The secrets file does not support environment variable expansion for security reasons.
