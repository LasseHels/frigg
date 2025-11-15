package main

import (
	"context"
	"flag"
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

	"github.com/LasseHels/frigg/frigg"
)

// release is set through the linker at build time, generally from a git sha. Used for logging and error reporting.
var release string

// flagConfigFile is the flag that contains the path to Frigg's configuration file.
const flagConfigFile = "config.file"

// flagSecretsFile is the flag that contains the path to Frigg's secrets file.
const flagSecretsFile = "secrets.file"

func main() {
	os.Exit(start())
}

func start() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var configPath, secretsPath string
	flag.StringVar(
		&configPath,
		flagConfigFile,
		"",
		"Path to Frigg's configuration file (.json, .yml, or .yaml) (required)",
	)
	flag.StringVar(
		&secretsPath,
		flagSecretsFile,
		"",
		"Path to Frigg's secrets file (.json, .yml, or .yaml) (required)",
	)
	flag.Parse()

	if err := run(ctx, configPath, secretsPath, os.Stdout); err != nil {
		fmt.Println(err.Error())
		return 1
	}

	return 0
}

func run(ctx context.Context, configPath, secretsPath string, w io.Writer) error {
	if configPath == "" {
		return errors.Errorf("required flag -%s missing", flagConfigFile)
	}

	if secretsPath == "" {
		return errors.Errorf("required flag -%s missing", flagSecretsFile)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	registry.MustRegister(collectors.NewGoCollector())

	_, _ = fmt.Fprintf(w, "Loading configuration file from path %s\n", configPath)
	cfg, err := frigg.NewConfig(configPath)
	if err != nil {
		return errors.Wrap(err, "reading configuration")
	}

	_, _ = fmt.Fprintf(w, "Loading secrets file from path %s\n", secretsPath)
	secrets, err := frigg.NewSecrets(secretsPath)
	if err != nil {
		return errors.Wrap(err, "reading secrets")
	}
	l := logger(w, cfg.Log.Level)

	f, err := cfg.Initialise(l, registry, secrets)
	if err != nil {
		return errors.Wrap(err, "initialising Frigg")
	}

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		if err := f.Start(ctx); err != nil {
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

func logger(w io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	l := slog.New(handler)
	l = l.With(slog.String("release", release))

	return l
}
