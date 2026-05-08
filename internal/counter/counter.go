// Package counter параллельно подсчитывает частоты слов в текстовых файлах
// каталога. Бинарные файлы детектируются через sniff на NUL-байт и валидный
// UTF-8 и пропускаются.
package counter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// ErrEmptyCorpus возвращается, когда обработка не дала ни одного успешного
// файла, при этом были ошибки чтения. Пустой каталог без ошибок — не ошибка.
var ErrEmptyCorpus = errors.New("counter: no files processed")

// sniffSize — сколько байт читаем для определения «текстовый ли файл».
const sniffSize = 4096

// fileTokenizer описывает контракт токенизатора, нужный счётчику.
// Интерфейс объявлен на стороне consumer'а (DIP).
type fileTokenizer interface {
	Tokenize(ctx context.Context, r io.Reader, emit func(word string) bool) error
}

// Walker абстрагирует обход дерева файлов. Counter не зависит от
// конкретного источника путей (DIP).
type Walker interface {
	Walk(ctx context.Context, root string, emit func(path string) error) error
}

// Counter оркестрирует параллельный обход каталога и подсчёт слов.
type Counter struct {
	// Workers — количество воркеров, читающих файлы одновременно. >= 1.
	Workers int
	// Tokenizer — токенизатор, применяемый к каждому файлу.
	Tokenizer fileTokenizer
	// Walker — источник путей к файлам.
	Walker Walker
	// Logger — логгер; nil допустим (используется slog.Default).
	Logger *slog.Logger
}

// Result — итог работы [Counter.Run].
type Result struct {
	// Counts — суммарные частоты слов по всем успешно прочитанным файлам.
	Counts map[string]int
	// Processed — количество успешно обработанных текстовых файлов.
	Processed int
	// Skipped — количество файлов, пропущенных как нетекстовые.
	Skipped int
	// Failed — количество файлов с ошибками открытия/токенизации.
	Failed int
}

// fileStatus — итог обработки одного файла.
type fileStatus int

const (
	fileProcessed fileStatus = iota
	fileSkipped
	fileFailed
)

// Run запускает producer, воркеры и агрегатор, обходит root и возвращает
// общий результат. Если ни один файл не обработан успешно И были ошибки —
// возвращается [ErrEmptyCorpus]. Пустой каталог без ошибок — не ошибка.
// Файлы, пропущенные как нетекстовые, не считаются ошибками.
func (c *Counter) Run(ctx context.Context, root string) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	logger := c.logger()

	paths, producerErrCh := c.startProducer(ctx, root)
	partials, processed, skipped, failed := c.startWorkers(ctx, paths, logger)
	total := aggregate(partials)

	producerErr := <-producerErrCh

	res := Result{
		Counts:    total,
		Processed: int(processed.Load()),
		Skipped:   int(skipped.Load()),
		Failed:    int(failed.Load()),
	}

	switch {
	case ctx.Err() != nil:
		return res, ctx.Err()
	case producerErr != nil && !errors.Is(producerErr, context.Canceled):
		return res, producerErr
	case res.Processed == 0 && res.Failed > 0:
		return res, ErrEmptyCorpus
	}
	return res, nil
}

func (c *Counter) validate() error {
	if c.Workers < 1 {
		return fmt.Errorf("counter: workers must be >= 1, got %d", c.Workers)
	}
	if c.Tokenizer == nil {
		return errors.New("counter: tokenizer is nil")
	}
	if c.Walker == nil {
		return errors.New("counter: walker is nil")
	}
	return nil
}

func (c *Counter) logger() *slog.Logger {
	if c.Logger == nil {
		return slog.Default()
	}
	return c.Logger
}

func (c *Counter) startProducer(ctx context.Context, root string) (<-chan string, <-chan error) {
	paths := make(chan string, c.Workers)
	errCh := make(chan error, 1)

	go func() {
		err := c.Walker.Walk(ctx, root, func(p string) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case paths <- p:
				return nil
			}
		})
		close(paths)
		errCh <- err
	}()

	return paths, errCh
}

func (c *Counter) startWorkers(
	ctx context.Context,
	paths <-chan string,
	logger *slog.Logger,
) (<-chan map[string]int, *atomic.Int64, *atomic.Int64, *atomic.Int64) {
	partials := make(chan map[string]int, c.Workers)
	var processed, skipped, failed atomic.Int64

	var wg sync.WaitGroup
	wg.Add(c.Workers)
	for i := 0; i < c.Workers; i++ {
		go func() {
			defer wg.Done()
			local := make(map[string]int)
			for path := range paths {
				if ctx.Err() != nil {
					continue
				}
				switch c.processFile(ctx, path, local, logger) {
				case fileProcessed:
					processed.Add(1)
				case fileSkipped:
					skipped.Add(1)
				case fileFailed:
					failed.Add(1)
				}
			}
			select {
			case partials <- local:
			case <-ctx.Done():
			}
		}()
	}

	go func() {
		wg.Wait()
		close(partials)
	}()

	return partials, &processed, &skipped, &failed
}

func aggregate(partials <-chan map[string]int) map[string]int {
	total := make(map[string]int)
	for partial := range partials {
		for w, n := range partial {
			total[w] += n
		}
	}
	return total
}

// processFile открывает файл, проверяет что он текстовый, и пропускает его
// через токенизатор. Отмена контекста не считается «ошибкой файла».
func (c *Counter) processFile(ctx context.Context, path string, local map[string]int, logger *slog.Logger) fileStatus {
	f, err := os.Open(path)
	if err != nil {
		logger.Warn("counter: open failed", "path", path, "err", err)
		return fileFailed
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			logger.Warn("counter: close failed", "path", path, "err", cerr)
		}
	}()

	sniff := make([]byte, sniffSize)
	n, sniffErr := io.ReadFull(f, sniff)
	if sniffErr != nil && !errors.Is(sniffErr, io.EOF) && !errors.Is(sniffErr, io.ErrUnexpectedEOF) {
		logger.Warn("counter: sniff failed", "path", path, "err", sniffErr)
		return fileFailed
	}
	sniff = sniff[:n]
	if !isLikelyText(sniff) {
		logger.Info("counter: skipping non-text file", "path", path)
		return fileSkipped
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		logger.Warn("counter: seek failed", "path", path, "err", err)
		return fileFailed
	}

	emit := func(w string) bool {
		local[w]++
		return true
	}
	if err := c.Tokenizer.Tokenize(ctx, f, emit); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fileFailed
		}
		logger.Warn("counter: tokenize failed", "path", path, "err", err)
		return fileFailed
	}
	return fileProcessed
}

// isLikelyText эвристически отличает текст от бинаря по первым байтам.
// Правила:
//   - пустой файл считается текстом;
//   - наличие NUL-байта — гарантированный признак бинаря;
//   - содержимое должно быть валидным UTF-8 (с допуском на обрезанную в
//     хвосте многобайтовую руну: пробуем подрезать до 3 последних байт).
func isLikelyText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	for trim := 0; trim < 4 && trim <= len(data); trim++ {
		if utf8.Valid(data[:len(data)-trim]) {
			return true
		}
	}
	return false
}
