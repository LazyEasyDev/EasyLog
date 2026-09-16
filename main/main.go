package main

import (
	"log/slog"
	"os"
	"time"

	easylog "github.com/LazyEasyDev/EasyLog"
)

type Student struct {
	Name string `json:"name"`
	Pet  Pet    `json:"pet"`
}

type Pet struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

func main() {

	student := Student{
		Name: "Alice",
		Pet: Pet{
			Name:  "Fluffy",
			Owner: "Alice",
		},
	}

	logDirectory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	if err := easylog.Init(easylog.InitOptions{
		Runtime: easylog.Options{
			Level: slog.LevelDebug,
		},
		File:     &easylog.FileOptions{Directory: logDirectory},
		Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
	}); err != nil {
		panic(err)
	}
	defer easylog.Close()

	slog.Info("application started", "address", ":8080", "student", student)
	slog.Warn("request retrying", "attempt", 2)
	slog.Error("request failed", "status", 500)
	time.Sleep(2 * time.Second)
	slog.Debug("debugging information", "student", student)
}
