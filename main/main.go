package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/LazyEasyDev/EasyLog"
)

func getcwd() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return dir
}

func main() {
	if err := EasyLog.Init(EasyLog.InitOptions{
		Runtime:  EasyLog.Options{Level: slog.LevelDebug},
		Terminal: &EasyLog.TerminalOptions{Writer: os.Stdout},
		File:     &EasyLog.FileOptions{BaseDirectory: getcwd()},
	}); err != nil {
		panic(err)
	}
	defer func() {
		if err := EasyLog.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	slog.Info("application started", "address", ":8080")
	log2 := slog.With("service", "checkout").With("cc", "bbbb2")

	log2.Warn("request retrying", "attempt", 2)
	log2.Debug("request retrying", "attempt", 3)
	log2.Error("request retrying", "attempt", 3)
}
