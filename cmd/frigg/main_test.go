package main

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	// Cancel context immediately to avoid tests hanging forever in case of failure.
	cancel()

	t.Run("errors if config path is missing", func(t *testing.T) {
		t.Parallel()

		err := run(ctx, "", io.Discard)
		expectedErr := "required flag -config.file missing"
		require.EqualError(t, err, expectedErr)
	})

	t.Run("errors if config path points to invalid file", func(t *testing.T) {
		t.Parallel()

		err := run(ctx, "does/not/exist", io.Discard)
		expectedErr := `reading configuration: loading configuration: reading config file at path "does/not/exist":` +
			` open does/not/exist: no such file or directory`
		require.EqualError(t, err, expectedErr)
	})
}
