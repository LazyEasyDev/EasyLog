package main

import (
	"fmt"
	"log/slog"
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func getcwd() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return dir
}

func main() {
	if err := easylog.Init(easylog.InitOptions{
		Runtime:  easylog.Options{Level: slog.LevelDebug},
		Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
		File:     &easylog.FileOptions{BaseDirectory: getcwd()},
	}); err != nil {
		panic(err)
	}
	defer func() {
		if err := easylog.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	slog.Info("application started", "address", ":8080")
	log2 := slog.With("service", "checkout").With("cc", "bbbb2")

	log2.Warn("request retrying", "attempt", 2)
	log2.Debug("request retrying", "attempt", 3)
	log2.Error("request retrying", "attempt", 3)
}
