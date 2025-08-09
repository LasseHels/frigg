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
```

### Secrets File Structure

The secrets file contains sensitive information that should not be stored in the main configuration file. Currently, the secrets file is used to store the Grafana API token:

```yaml
grafana:
    # Token used to authenticate with Grafana's API.
    # This token must have permissions to list and delete dashboards.
    # Required.
    token: 'your-grafana-api-token-here'
```

The secrets file must:
- Exist and be readable by Frigg
- Contain valid YAML
- Include the `grafana.token` field
- Have a non-empty value for `grafana.token`

Frigg will fail to start if any of these requirements are not met.

### Environment Variable Expansion

Frigg's YAML configuration supports [environment variable expansion](https://pkg.go.dev/os#ExpandEnv) in the configuration file.
Use `${VAR_NAME}` syntax to include environment variables:
```yaml
server:
  host: ${FRIGG_HOST}
  port: 9000
```

If `FRIGG_HOST` is set to `example.com`, Frigg will use `example.com` as the server host.
