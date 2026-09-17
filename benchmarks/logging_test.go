package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	message        = "request completed"
	withFieldsJSON = `{"level":"INFO","msg":"request completed","service":"checkout","request_id":"req-123","method":"GET","path":"/orders","region":"eu-west","attempt":3,"bytes":4096,"cached":true,"elapsed":1500000,"ratio":0.75}`
)

type workload struct {
	name      string
	slog      func(*slog.Logger) func()
	slogAttrs func(*slog.Logger) func()
	zap       func(*zap.Logger) func()
	zapSugar  func(*zap.SugaredLogger) func()
	want      string
}

var workloads = []workload{
	{
		name: "Message",
		slog: func(logger *slog.Logger) func() {
			return func() { logger.Info(message) }
		},
		slogAttrs: func(logger *slog.Logger) func() {
			return func() { logger.LogAttrs(context.Background(), slog.LevelInfo, message) }
		},
		zap: func(logger *zap.Logger) func() {
			return func() { logger.Info(message) }
		},
		zapSugar: func(logger *zap.SugaredLogger) func() {
			return func() { logger.Infow(message) }
		},
		want: `{"level":"INFO","msg":"request completed"}`,
	},
	{
		name: "Fields10",
		slog: func(logger *slog.Logger) func() {
			return func() { logger.Info(message, keyValues()...) }
		},
		slogAttrs: func(logger *slog.Logger) func() {
			return func() { logger.LogAttrs(context.Background(), slog.LevelInfo, message, slogFields()...) }
		},
		zap: func(logger *zap.Logger) func() {
			return func() { logger.Info(message, zapFields()...) }
		},
		zapSugar: func(logger *zap.SugaredLogger) func() {
			return func() { logger.Infow(message, keyValues()...) }
		},
		want: withFieldsJSON,
	},
	{
		name: "Context10",
		slog: func(logger *slog.Logger) func() {
			child := logger.With(keyValues()...)
			return func() { child.Info(message) }
		},
		slogAttrs: func(logger *slog.Logger) func() {
			child := slog.New(logger.Handler().WithAttrs(slogFields()))
			return func() { child.LogAttrs(context.Background(), slog.LevelInfo, message) }
		},
		zap: func(logger *zap.Logger) func() {
			child := logger.With(zapFields()...)
			return func() { child.Info(message) }
		},
		zapSugar: func(logger *zap.SugaredLogger) func() {
			child := logger.With(keyValues()...)
			return func() { child.Infow(message) }
		},
		want: withFieldsJSON,
	},
}

var implementations = []string{"EasyLog", "EasyLogAttrs", "Slog", "SlogAttrs", "Zap", "ZapSugar"}

func newSlogLogger(writer io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func newEasyLogLogger(test testing.TB, writer io.Writer) *slog.Logger {
	runtime := easylog.New(easylog.Options{Level: slog.LevelInfo}, []easylog.Output{terminal.NewJSON(writer)})
	test.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			test.Error(err)
		}
	})
	return runtime.Logger()
}

func newZapLogger(test testing.TB, writer io.Writer) *zap.Logger {
	encoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "time",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.RFC3339NanoTimeEncoder,
		EncodeDuration: zapcore.NanosDurationEncoder,
	})
	logger := zap.New(zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(writer)), zap.InfoLevel))
	test.Cleanup(func() {
		if err := logger.Sync(); err != nil {
			test.Error(err)
		}
	})
	return logger
}

func (work workload) newLogger(test testing.TB, writer io.Writer, implementation string) func() {
	switch implementation {
	case "EasyLog":
		return work.slog(newEasyLogLogger(test, writer))
	case "EasyLogAttrs":
		return work.slogAttrs(newEasyLogLogger(test, writer))
	case "Slog":
		return work.slog(newSlogLogger(writer))
	case "SlogAttrs":
		return work.slogAttrs(newSlogLogger(writer))
	case "Zap":
		return work.zap(newZapLogger(test, writer))
	case "ZapSugar":
		return work.zapSugar(newZapLogger(test, writer).Sugar())
	default:
		test.Fatalf("unknown implementation %q", implementation)
		return nil
	}
}

func BenchmarkJSON(bench *testing.B) {
	for _, work := range workloads {
		bench.Run("workload="+work.name, func(bench *testing.B) {
			for _, implementation := range implementations {
				bench.Run("logger="+implementation, func(bench *testing.B) {
					log := work.newLogger(bench, io.Discard, implementation)
					bench.ReportAllocs()
					for bench.Loop() {
						log()
					}
				})
			}
		})
	}
}

func TestEquivalentJSON(test *testing.T) {
	for _, work := range workloads {
		for _, implementation := range implementations {
			test.Run(work.name+"/"+implementation, func(test *testing.T) {
				var output bytes.Buffer
				log := work.newLogger(test, &output, implementation)
				log()
				if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 || !bytes.HasSuffix(output.Bytes(), []byte{'\n'}) {
					test.Fatalf("expected one NDJSON record, got %q", output.Bytes())
				}
				var got, want map[string]any
				if err := json.Unmarshal(output.Bytes(), &got); err != nil {
					test.Fatal(err)
				}
				timestamp, ok := got["time"].(string)
				if !ok {
					test.Fatalf("timestamp = %v, want an RFC3339 string", got["time"])
				}
				if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
					test.Fatal(err)
				}
				delete(got, "time")
				if err := json.Unmarshal([]byte(work.want), &want); err != nil {
					test.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					test.Fatalf("record = %v, want %v", got, want)
				}
			})
		}
	}
}

func keyValues() []any {
	return []any{
		"service", "checkout",
		"request_id", "req-123",
		"method", "GET",
		"path", "/orders",
		"region", "eu-west",
		"attempt", 3,
		"bytes", int64(4096),
		"cached", true,
		"elapsed", 1500 * time.Microsecond,
		"ratio", 0.75,
	}
}

func slogFields() []slog.Attr {
	return []slog.Attr{
		slog.String("service", "checkout"),
		slog.String("request_id", "req-123"),
		slog.String("method", "GET"),
		slog.String("path", "/orders"),
		slog.String("region", "eu-west"),
		slog.Int("attempt", 3),
		slog.Int64("bytes", 4096),
		slog.Bool("cached", true),
		slog.Duration("elapsed", 1500*time.Microsecond),
		slog.Float64("ratio", 0.75),
	}
}

func zapFields() []zap.Field {
	return []zap.Field{
		zap.String("service", "checkout"),
		zap.String("request_id", "req-123"),
		zap.String("method", "GET"),
		zap.String("path", "/orders"),
		zap.String("region", "eu-west"),
		zap.Int("attempt", 3),
		zap.Int64("bytes", 4096),
		zap.Bool("cached", true),
		zap.Duration("elapsed", 1500*time.Microsecond),
		zap.Float64("ratio", 0.75),
	}
}
