package main

import (
	"log/slog"
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func main() {
	logDirectory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	if err := easylog.Init(
		easylog.Options{},
		&easylog.FileOptions{Directory: logDirectory},
		os.Stdout,
	); err != nil {
		panic(err)
	}
	defer easylog.Close()

	slog.Info("application started", "address", ":8080")
	slog.Warn("request retrying", "attempt", 2)
	slog.Error("request failed", "status", 500)
}
