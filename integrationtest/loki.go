package integrationtest

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/docker/go-connections/nat"
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

func NewLoki(ctx context.Context) *Loki {
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

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          false,
	})
	if err != nil {
		panic(err)
	}

	if err := container.CopyToContainer(ctx, lokiConfig, "/etc/loki.yaml", int64(os.ModePerm)); err != nil {
		panic(err)
	}

	if err := container.Start(ctx); err != nil {
		panic(err)
	}

	return &Loki{
		container: container,
		host:      mustGetHost(ctx, container, lokiDefaultPort),
	}
}

func (l *Loki) Stop() error {
	// Stopping Loki takes ~40 seconds without a timeout. I'm not sure why this is, and we deliberately set an
	// extremely low timeout to make the SDK forcefully kill the container immediately. This speeds up our integration
	// tests quite a bit, and shouldn't cause problems as this container is used exclusively for testing.
	timeout := time.Millisecond
	return l.container.Stop(context.Background(), &timeout)
}

func (l *Loki) Host() string {
	return l.host
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
