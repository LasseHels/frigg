# Frigg

Frigg analyses Grafana dashboard usage and deletes unused dashboards

## Configuration

Frigg is configured using a configuration file and a secrets file. Both files support both JSON and YAML. The paths to
these files are provided using the `-config.file` and `-secrets.file` flags when starting Frigg:
```bash
frigg -config.file=/path/to/config.yaml -secrets.file=/path/to/secrets.yaml
```

Both flags are required. Frigg will fail to start if either flag is missing or points to a non-existent file.

### Configuration File Structure

Below is a complete example of Frigg's configuration file structure in YAML format:
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

backup:
  github:
    # GitHub repository where deleted dashboards will be backed up. The repository must be in the format 'owner/repo'.
    # Frigg will back up dashboard JSON to this repository before deletion. If backing up a dashboard fails, the
    # dashboard will not be deleted.
    #
    # Required.
    repository: 'octocat/hello-world'
    # Branch to commit deleted dashboards to (default: "main").
    branch: 'main'
    # Directory within the repository where deleted dashboards will be stored (default: "deleted-dashboards").
    # Dashboards will be saved as "{directory}/{dashboard-namespace}/{dashboard-name}.json".
    directory: 'deleted-dashboards'
    # GitHub API URL for GitHub Enterprise Server instances.
    #
    # The value must be a valid URL according to Go's url.Parse() function (default: "https://api.github.com/").
    api_url: 'https://github.example.com/api/v3'
```

### Secrets File Structure

Frigg's secrets file supports both the YAML and JSON formats. The extension of the secrets file must be `.json`, `.yml`
or `.yaml`.

YAML format:
```yaml
grafana:
    # Tokens used to authenticate with Grafana's API for specific namespaces. This field is a map where keys are
    # namespace names and values are the token used to authenticate with Grafana's API for that namespace. A namespace's
    # token is expected to have permissions to list and delete dashboards in that namespace.
    #
    # This field also controls which namespaces Frigg will prune and which it will ignore; Frigg will only prune
    # namespaces that have an entry in this map.
    #
    # See https://grafana.com/docs/grafana/v12.0/developers/http_api/apis/#namespace-namespace.
    #
    # The map must contain at least one namespace.
    #
    # Required.
    tokens:
        default: 'token-for-the-default-namespace'
        org-1: 'token-for-the-org-1-namespace'
        stacks-5: 'token-for-the-stacks-5-namespace'

backup:
    github:
        # GitHub personal access token or fine-grained token for authenticating with the GitHub API.
        #
        # The token must have the following permissions:
        # - Contents: Read and write (to create, read, and update files in the repository)
        #
        # For fine-grained tokens, these permissions should be scoped to the specific repository.
        # For classic tokens, the 'repo' scope is required.
        #
        # See https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens
        #
        # Required.
        token: 'ghp_exampletoken123'
```

The same secrets in JSON format:
```json
{
  "grafana": {
    "tokens": {
      "default": "token-for-the-default-namespace",
      "org-1": "token-for-the-org-1-namespace",
      "stacks-5": "token-for-the-stacks-5-namespace"
    }
  },
  "backup": {
    "github": {
      "token": "ghp_exampletoken123"
    }
  }
}
```

### Environment Variable Expansion

Frigg's YAML configuration file supports [environment variable expansion](https://pkg.go.dev/os#ExpandEnv).
Use `${VAR_NAME}` syntax to include environment variables:

```yaml
server:
  host: ${FRIGG_HOST}
  port: 9000
```

If `FRIGG_HOST` is set to `example.com`, Frigg will use `example.com` as the server host.

> [!NOTE]
> The secrets file does not support environment variable expansion for security reasons.
