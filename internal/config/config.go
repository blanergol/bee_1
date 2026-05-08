// Package config парсит и валидирует флаги CLI и собирает структурированный
// логгер. Знание о том, КАК форматируется вывод — в пакете output;
// знание о том, КАК работает счётчик — в пакете counter.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"

	"github.com/redmadrobot/topwords/internal/output"
)

// ErrInvalidConfig — ошибка валидации флагов.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// LogLevel описывает уровень логирования.
type LogLevel string

// Допустимые уровни логирования.
const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

// Config — нормализованный набор параметров запуска.
type Config struct {
	Dir       string
	MinLength int
	Top       int
	Workers   int
	Format    output.Format
	LogLevel  LogLevel
}

// ParseFlags парсит args (без имени программы) и возвращает валидный
// [Config]. При --help возвращается [flag.ErrHelp]. Сообщения об ошибках
// пишутся в stderr.
func ParseFlags(args []string, stderr io.Writer) (Config, error) {
	fs := flag.NewFlagSet("topwords", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dir       = fs.String("dir", "", "path to directory with text files (required)")
		minLength = fs.Int("min-length", 4, "minimum word length; only words longer than this are counted")
		top       = fs.Int("top", 10, "how many top words to print")
		workers   = fs.Int("workers", runtime.NumCPU(), "number of concurrent file workers")
		format    = fs.String("format", "text", "output format: text|json")
		logLevel  = fs.String("log-level", "info", "log level: debug|info|warn|error")
	)

	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: topwords [flags]")
		fmt.Fprintln(stderr, "Counts top-N most frequent words longer than --min-length across files in --dir.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Dir:       *dir,
		MinLength: *minLength,
		Top:       *top,
		Workers:   *workers,
		Format:    output.Format(*format),
		LogLevel:  LogLevel(*logLevel),
	}

	if err := cfg.validate(); err != nil {
		fmt.Fprintln(stderr, "topwords:", err)
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Dir) == "" {
		return fmt.Errorf("%w: --dir is required", ErrInvalidConfig)
	}
	if c.MinLength < 0 {
		return fmt.Errorf("%w: --min-length must be >= 0, got %d", ErrInvalidConfig, c.MinLength)
	}
	if c.Top < 1 {
		return fmt.Errorf("%w: --top must be >= 1, got %d", ErrInvalidConfig, c.Top)
	}
	if c.Workers < 1 {
		return fmt.Errorf("%w: --workers must be >= 1, got %d", ErrInvalidConfig, c.Workers)
	}
	if err := output.Validate(c.Format); err != nil {
		return fmt.Errorf("%w: --format: %w", ErrInvalidConfig, err)
	}
	switch c.LogLevel {
	case LogDebug, LogInfo, LogWarn, LogError:
	default:
		return fmt.Errorf("%w: --log-level must be debug|info|warn|error, got %q", ErrInvalidConfig, c.LogLevel)
	}
	return nil
}

// BuildLogger собирает [*slog.Logger] поверх [slog.NewTextHandler] с заданным
// уровнем.
func BuildLogger(level LogLevel, w io.Writer) (*slog.Logger, error) {
	var lvl slog.Level
	switch level {
	case LogDebug:
		lvl = slog.LevelDebug
	case LogInfo:
		lvl = slog.LevelInfo
	case LogWarn:
		lvl = slog.LevelWarn
	case LogError:
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("%w: unknown log level %q", ErrInvalidConfig, level)
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(h), nil
}
