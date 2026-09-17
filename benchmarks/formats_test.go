package benchmarks

import (
	"io"
	"log/slog"
	"testing"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func BenchmarkFormats(bench *testing.B) {
	for _, work := range workloads {
		bench.Run("workload="+work.name, func(bench *testing.B) {
			for _, format := range []string{"JSON", "Text"} {
				bench.Run("format="+format, func(bench *testing.B) {
					var output easylog.Output = terminal.NewJSON(io.Discard)
					if format == "Text" {
						output = terminal.New(io.Discard, &terminal.TextFormatter{DisableColors: true, ShowLevel: true})
					}
					runtime := easylog.New(easylog.Options{Level: slog.LevelInfo}, []easylog.Output{output})
					bench.Cleanup(func() {
						if err := runtime.Close(); err != nil {
							bench.Error(err)
						}
					})
					log := work.slog(runtime.Logger())
					bench.ReportAllocs()
					for bench.Loop() {
						log()
					}
				})
			}
		})
	}
}
