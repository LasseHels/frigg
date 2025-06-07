package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"golang.org/x/sync/errgroup"

	"github.com/LasseHels/frigg/pkg/frigg"
	"github.com/LasseHels/frigg/pkg/server"
)

func main() {
	os.Exit(start())
}

func start() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := run(ctx, os.Stdout); err != nil {
		fmt.Println(err.Error())
		return 1
	}

	return 0
}

func run(ctx context.Context, w io.Writer) error {
	l := logger(w)
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	registry.MustRegister(collectors.NewGoCollector())

	// TODO: Load configuration from file.
	cfg := frigg.Config{
		Server: server.Config{
			Host: "localhost",
			Port: 8080,
		},
	}

	f := cfg.Initialise(l, registry)

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		if err := f.Start(); err != nil {
			return errors.Wrap(err, "starting Frigg")
		}

		return nil
	})

	<-ctx.Done()

	eg.Go(func() error {
		if err := f.Stop(); err != nil {
			return errors.Wrap(err, "stopping Frigg")
		}

		return nil
	})

	if err := eg.Wait(); err != nil {
		return errors.Wrap(err, "waiting for errgroup")
	}

	return nil
}

func logger(w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo, // TODO: Read from configuration.
	})
	return slog.New(handler)
}
