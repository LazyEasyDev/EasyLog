package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

type requestIDKey struct{}

type order struct {
	ID    string `json:"id"`
	Items int    `json:"items"`
}

func main() {

	selected := flag.String("example", "all", "Example: all, init, new, context, memory, outputs, format")
	logDirectory := flag.String("log-dir", "", "Optional base directory for the init example's logs subdirectory")
	flag.Parse()
	if err := run(*selected, *logDirectory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(selected, logDirectory string) error {
	examples := []struct {
		name string
		run  func() error
	}{
		{"init", func() error { return initializedLogger(logDirectory) }},
		{"new", independentLogger},
		{"context", contextLogger},
		{"memory", memoryConsumer},
		{"outputs", multipleOutputs},
		{"format", standaloneFormatter},
	}
	matched := false
	for _, example := range examples {
		if selected != "all" && selected != example.name {
			continue
		}
		matched = true
		fmt.Fprintf(os.Stderr, "\n[%s]\n", example.name)
		if err := example.run(); err != nil {
			return fmt.Errorf("%s example: %w", example.name, err)
		}
	}
	if !matched {
		return fmt.Errorf("unknown example %q; use -help to list examples", selected)
	}
	return nil
}

func initializedLogger(logDirectory string) (err error) {
	options := easylog.InitOptions{
		Runtime:  easylog.Options{Level: slog.LevelDebug},
		Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
	}
	if logDirectory != "" {
		absoluteDirectory, err := filepath.Abs(logDirectory)
		if err != nil {
			return err
		}
		options.File = &easylog.FileOptions{Directory: absoluteDirectory}
	}
	if err := easylog.Init(options); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, easylog.Sync(), easylog.Close()) }()

	slog.Info("application started", "address", ":8080")
	slog.With("service", "checkout").Warn("request retrying", "attempt", 2)
	slog.Error("request failed", "status", 503, "error", errors.New("upstream unavailable"))
	slog.Debug("configuration loaded", "environment", "development")
	log.Print("standard log uses the same runtime")
	return nil
}

func independentLogger() (err error) {
	var level slog.LevelVar
	runtime := easylog.New(easylog.Options{
		Level:     &level,
		AddSource: true,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "token" {
				return slog.String(attr.Key, "[REDACTED]")
			}
			return attr
		},
	}, []easylog.Output{terminal.NewJSON(os.Stdout)})
	defer func() { err = errors.Join(err, runtime.Sync(), runtime.Close()) }()

	logger := runtime.Logger().With("service", "orders", "version", "1.0")
	logger.Info("order received", "order", order{ID: "order-42", Items: 3})
	logger.LogAttrs(context.Background(), slog.LevelInfo, "request completed",
		slog.Group("http", slog.String("method", "POST"), slog.Int("status", 201)),
		slog.Int("attempt", 1),
	)
	logger.WithGroup("payment").With("provider", "sandbox").Info("authorized", "amount", 42.50)
	logger.Info("credentials loaded", "token", "demo-token")
	logger.Debug("hidden at the default INFO level")
	level.Set(slog.LevelDebug)
	logger.Debug("debug enabled at runtime", "queue_depth", 2)
	return nil
}

func contextLogger() (err error) {
	runtime := easylog.New(easylog.Options{
		Enrichers: []easylog.Enricher{
			func(ctx context.Context) []slog.Attr {
				requestID, ok := ctx.Value(requestIDKey{}).(string)
				if !ok {
					return nil
				}
				return []slog.Attr{slog.String("request_id", requestID)}
			},
		},
	}, []easylog.Output{terminal.NewJSON(os.Stdout)})
	defer func() { err = errors.Join(err, runtime.Sync(), runtime.Close()) }()

	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-123")
	logger := runtime.Logger().With("service", "api")
	logger.InfoContext(ctx, "request accepted", "path", "/orders")
	logger.LogAttrs(ctx, slog.LevelWarn, "cache miss", slog.String("key", "order-42"))
	return nil
}

func memoryConsumer() (err error) {
	runtime := easylog.New(easylog.Options{
		MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
	}, nil)
	defer func() { err = errors.Join(err, runtime.Sync(), runtime.Close()) }()

	logger := runtime.Logger().With("job", "daily-export")
	logger.Info("job started")
	logger.Info("job completed", "rows", 128)

	consumer := runtime.Consumer()
	fmt.Fprintf(os.Stderr, "retained: %d records, %d bytes\n", consumer.Len(), consumer.Bytes())
	page, err := consumer.Take(1)
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(bytes.Join(page, nil)); err != nil {
		return err
	}
	remaining, err := consumer.Take(0)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(bytes.Join(remaining, nil))
	return err
}

func multipleOutputs() (err error) {
	var jsonOutput bytes.Buffer
	formatter := terminal.GetDefaultTextFormatter()
	formatter.DisableColors = true
	formatter.DisableTimestamp = true
	if err := easylog.InitWithOutputs(easylog.Options{}, []easylog.Output{
		terminal.New(os.Stdout, &formatter),
		terminal.NewJSON(&jsonOutput),
	}); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, easylog.Sync(), easylog.Close()) }()

	slog.Info("inventory updated", "sku", "item-42", "quantity", 12)
	_, err = os.Stdout.Write(jsonOutput.Bytes())
	return err
}

func standaloneFormatter() error {
	formatter := terminal.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
		ShowLevel:        true,
	}
	line, err := formatter.Format([]byte(`{"level":"WARN","msg":"cache miss","key":"item:42"}`))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(line)
	return err
}
