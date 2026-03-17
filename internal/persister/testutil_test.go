package persister

import (
	"log/slog"
	"os"
)

// testLogger returns a slog.Logger that writes to stderr for test output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}
