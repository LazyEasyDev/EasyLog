package easylog

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewLevel(t *testing.T) {
	var nilLevel *slog.LevelVar
	tests := []struct {
		name      string
		level     slog.Leveler
		threshold slog.Level
	}{
		{name: "unset", threshold: slog.LevelInfo},
		{name: "nil LevelVar", level: nilLevel, threshold: slog.LevelInfo},
		{name: "explicit debug", level: slog.LevelDebug, threshold: slog.LevelDebug},
		{name: "explicit warn", level: slog.LevelWarn, threshold: slog.LevelWarn},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := New(Options{Level: test.level}, nil)
			for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				want := level >= test.threshold
				if got := runtime.Handler().Enabled(context.Background(), level); got != want {
					t.Errorf("Enabled(%s) = %t, want %t", level, got, want)
				}
			}
		})
	}
}
