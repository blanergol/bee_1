// Команда topwords печатает топ-N самых частых слов длиной больше заданной
// в каталоге текстовых файлов. Подсчёт ведётся в несколько потоков.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/redmadrobot/topwords/internal/config"
	"github.com/redmadrobot/topwords/internal/counter"
	"github.com/redmadrobot/topwords/internal/output"
	"github.com/redmadrobot/topwords/internal/scanner"
	"github.com/redmadrobot/topwords/internal/tokenizer"
	"github.com/redmadrobot/topwords/internal/topk"
)

const (
	exitOK         = 0
	exitFailure    = 1
	exitUsageError = 2
	exitSignaled   = 130
)

// fsWalker склеивает [scanner.Walk] с интерфейсом [counter.Walker].
// Адаптер живёт в main, чтобы пакет counter не зависел от scanner (DIP).
type fsWalker struct{ logger *slog.Logger }

func (w fsWalker) Walk(ctx context.Context, root string, emit func(path string) error) error {
	return scanner.Walk(ctx, root, w.logger, emit)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// После первого сигнала снимаем нашу подписку: повторный SIGINT/
	// SIGTERM пойдёт по дефолтному обработчику Go runtime и завершит
	// процесс немедленно.
	go func() {
		<-ctx.Done()
		stop()
	}()

	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run выполняет одну итерацию утилиты и возвращает код возврата процесса.
// Изолирован от main для тестируемости.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cfg, err := config.ParseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsageError
	}

	logger, err := config.BuildLogger(cfg.LogLevel, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "topwords:", err)
		return exitUsageError
	}

	if info, err := os.Stat(cfg.Dir); err != nil {
		fmt.Fprintln(stderr, "topwords: stat:", err)
		return exitUsageError
	} else if !info.IsDir() {
		fmt.Fprintf(stderr, "topwords: %q is not a directory\n", cfg.Dir)
		return exitUsageError
	}

	writer, err := output.WriterFor(cfg.Format)
	if err != nil {
		fmt.Fprintln(stderr, "topwords:", err)
		return exitUsageError
	}

	c := &counter.Counter{
		Workers:   cfg.Workers,
		Tokenizer: tokenizer.New(cfg.MinLength),
		Walker:    fsWalker{logger: logger},
		Logger:    logger,
	}

	res, err := c.Run(ctx, cfg.Dir)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return exitSignaled
	case errors.Is(err, counter.ErrEmptyCorpus):
		fmt.Fprintln(stderr, "topwords:", err)
		return exitFailure
	case err != nil:
		fmt.Fprintln(stderr, "topwords:", err)
		return exitFailure
	}

	logger.Info("topwords: done",
		"processed", res.Processed,
		"failed", res.Failed,
		"unique_words", len(res.Counts),
	)

	entries := topk.Select(res.Counts, cfg.Top)
	if err := writer.Write(stdout, entries); err != nil {
		fmt.Fprintln(stderr, "topwords:", err)
		return exitFailure
	}
	return exitOK
}
