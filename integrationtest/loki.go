package integrationtest

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const lokiDefaultPort = 3100

//go:embed loki.yaml
var lokiConfig []byte

type Loki struct {
	container testcontainers.Container
	host      string
}

func NewLoki(t *testing.T) *Loki {
	req := testcontainers.ContainerRequest{
		Image:        "grafana/loki:2.9.2",
		ExposedPorts: []string{fmt.Sprintf("%d", lokiDefaultPort)},
		Cmd:          []string{"-config.file=/etc/loki.yaml"},
		WaitingFor: wait.ForAll(
			// See https://github.com/abiosoft/colima/issues/71#issuecomment-979516106.
			wait.ForExposedPort(),
			wait.ForLog("Loki started"),
		),
	}

	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          false,
	})
	require.NoError(t, err)

	err = container.CopyToContainer(t.Context(), lokiConfig, "/etc/loki.yaml", int64(os.ModePerm))
	require.NoError(t, err)

	err = container.Start(t.Context())
	require.NoError(t, err)

	l := &Loki{
		container: container,
		host:      mustGetHost(t.Context(), container, lokiDefaultPort),
	}

	t.Cleanup(func() {
		require.NoError(t, l.stop())
	})

	return l
}

func (l *Loki) stop() error {
	// Stopping Loki takes ~40 seconds without a timeout. I'm not sure why this is, and we deliberately set an
	// extremely low timeout to make the SDK forcefully kill the container immediately. This speeds up our integration
	// tests quite a bit, and shouldn't cause problems as this container is used exclusively for testing.
	timeout := time.Millisecond
	return l.container.Stop(context.Background(), &timeout)
}

func (l *Loki) Host() string {
	return l.host
}

// PushLogs sends logs to Loki via the push API.
func (l *Loki) PushLogs(ctx context.Context, logs []LogEntry) error {
	url := fmt.Sprintf("http://%s/loki/api/v1/push", l.host)

	payload := map[string]any{
		"streams": []map[string]any{
			{
				"stream": logs[0].Labels,
				"values": make([][]string, len(logs)),
			},
		},
	}

	// Convert logs to Loki format
	values := make([][]string, len(logs))
	for i, log := range logs {
		values[i] = []string{
			fmt.Sprintf("%d", log.Timestamp.UnixNano()),
			log.Message,
		}
	}
	payload["streams"].([]map[string]any)[0]["values"] = values

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code pushing logs: %d", resp.StatusCode)
	}

	return nil
}

// LogEntry represents a single log entry to be pushed to Loki.
type LogEntry struct {
	Timestamp time.Time
	Message   string
	Labels    map[string]string
}

func mustGetHost(ctx context.Context, c testcontainers.Container, containerPort int) string {
	port, err := c.MappedPort(ctx, nat.Port(strconv.Itoa(containerPort)))
	if err != nil {
		panic(err)
	}

	host, err := c.Host(ctx)
	if err != nil {
		panic(err)
	}

	return net.JoinHostPort(host, port.Port())
}
