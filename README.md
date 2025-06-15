# Frigg

Frigg analyses Grafana dashboard usage and deletes unused dashboards

## Configuration

Frigg is configured using a YAML configuration file. The path to this file is provided using the `-config.file` flag when starting Frigg:
```bash
frigg -config.file=/path/to/config.yaml
```

### Structure

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
