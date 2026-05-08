package counter_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/redmadrobot/topwords/internal/counter"
	"github.com/redmadrobot/topwords/internal/scanner"
	"github.com/redmadrobot/topwords/internal/tokenizer"
)

// scanWalker — тестовый адаптер, оборачивающий scanner.Walk. Демонстрирует,
// что Counter не зависит напрямую от пакета scanner; адаптер строится
// потребителем.
type scanWalker struct{ logger *slog.Logger }

func (w scanWalker) Walk(ctx context.Context, root string, emit func(string) error) error {
	return scanner.Walk(ctx, root, w.logger, emit)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// sequentialCount — простая последовательная реализация для сравнения.
func sequentialCount(t *testing.T, root string, minLen int) map[string]int {
	t.Helper()
	tk := tokenizer.New(minLen)
	out := make(map[string]int)
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			return oerr
		}
		defer f.Close()
		return tk.Tokenize(context.Background(), f, func(w string) bool {
			out[w]++
			return true
		})
	})
	require.NoError(t, err)
	return out
}

func TestRun_SkipsNonTextFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "text.txt"), "alpha alpha beta")

	binPath := filepath.Join(root, "binary.bin")
	binData := []byte{0x00, 0x01, 0x02, 'z', 'z', 'z', 'z', 0x00}
	require.NoError(t, os.WriteFile(binPath, binData, 0o644))

	logger := quietLogger()
	c := &counter.Counter{
		Workers:   2,
		Tokenizer: tokenizer.New(0),
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	res, err := c.Run(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed)
	require.Equal(t, 1, res.Skipped)
	require.Equal(t, 0, res.Failed)

	_, hasZzzz := res.Counts["zzzz"]
	require.False(t, hasZzzz, "binary content must not contribute to counts")
	require.Equal(t, 2, res.Counts["alpha"])
}

func TestRun_LongLineFile_NoCrash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const repeats = 100_000
	chunk := "alpha beta gamma "
	body := strings.Repeat(chunk, repeats) // ~1.7 MiB, без переводов строк
	writeFile(t, filepath.Join(root, "long.txt"), body)

	logger := quietLogger()
	c := &counter.Counter{
		Workers:   2,
		Tokenizer: tokenizer.New(0),
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	res, err := c.Run(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, repeats, res.Counts["alpha"])
	require.Equal(t, repeats, res.Counts["beta"])
	require.Equal(t, repeats, res.Counts["gamma"])
}

func TestRun_Aggregation_MatchesSequential(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"),
		"The quick brown fox jumps over the lazy dog.\n"+
			"The dog was not amused. The brown fox laughed.\n")
	writeFile(t, filepath.Join(root, "sub", "b.txt"),
		"Hello world! Hello мир! Здравствуй мир.\n")
	writeFile(t, filepath.Join(root, "sub", "deep", "c.txt"),
		strings.Repeat("brown ", 10)+"\n"+strings.Repeat("fox ", 5)+"\n")

	const minLen = 3
	want := sequentialCount(t, root, minLen)

	logger := quietLogger()
	c := &counter.Counter{
		Workers:   4,
		Tokenizer: tokenizer.New(minLen),
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	res, err := c.Run(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, 3, res.Processed)
	require.Equal(t, 0, res.Failed)
	if diff := cmp.Diff(want, res.Counts); diff != "" {
		t.Fatalf("counts diff (-want +got):\n%s", diff)
	}
}

// listWalker — тестовая реализация Walker, отдающая фиксированный список
// путей. Используется, чтобы изолировать Counter от ФС в части тестов.
type listWalker struct{ paths []string }

func (l listWalker) Walk(ctx context.Context, _ string, emit func(string) error) error {
	for _, p := range l.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(p); err != nil {
			return err
		}
	}
	return nil
}

func TestRun_WithListWalker_AggregatesPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pa := filepath.Join(root, "a.txt")
	pb := filepath.Join(root, "b.txt")
	writeFile(t, pa, "alpha alpha beta")
	writeFile(t, pb, "alpha gamma")

	logger := quietLogger()
	c := &counter.Counter{
		Workers:   2,
		Tokenizer: tokenizer.New(0),
		Walker:    listWalker{paths: []string{pa, pb}},
		Logger:    logger,
	}
	res, err := c.Run(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, 2, res.Processed)
	require.Equal(t, 3, res.Counts["alpha"])
	require.Equal(t, 1, res.Counts["beta"])
	require.Equal(t, 1, res.Counts["gamma"])
}

func TestRun_EmptyDir_NoError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logger := quietLogger()
	c := &counter.Counter{
		Workers:   2,
		Tokenizer: tokenizer.New(0),
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	res, err := c.Run(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, 0, res.Processed)
	require.Equal(t, 0, res.Failed)
	require.Empty(t, res.Counts)
}

func TestRun_MissingRoot_ReturnsError(t *testing.T) {
	t.Parallel()

	logger := quietLogger()
	c := &counter.Counter{
		Workers:   2,
		Tokenizer: tokenizer.New(0),
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	_, err := c.Run(context.Background(), filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}

// flakyTokenizer падает на первом файле, чтобы проверить, что одна ошибка не
// валит весь процесс.
type flakyTokenizer struct {
	inner    *tokenizer.Tokenizer
	failOnce atomic.Bool
}

func (f *flakyTokenizer) Tokenize(ctx context.Context, r io.Reader, emit func(string) bool) error {
	if f.failOnce.CompareAndSwap(false, true) {
		return errors.New("synthetic token failure")
	}
	return f.inner.Tokenize(ctx, r, emit)
}

func TestRun_PartialFailure_DoesNotAbort(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, filepath.Join(root, fmt.Sprintf("f%d.txt", i)),
			"alpha beta gamma alpha")
	}

	tk := &flakyTokenizer{inner: tokenizer.New(0)}
	logger := quietLogger()
	c := &counter.Counter{
		Workers:   3,
		Tokenizer: tk,
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	res, err := c.Run(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, 4, res.Processed)
	require.Equal(t, 1, res.Failed)
	require.Equal(t, res.Counts["alpha"], 8)
}

// alwaysFailTokenizer моделирует ситуацию, когда ни один файл не прочитан.
type alwaysFailTokenizer struct{}

func (alwaysFailTokenizer) Tokenize(context.Context, io.Reader, func(string) bool) error {
	return errors.New("synthetic always-fail")
}

func TestRun_AllFiles_Failed_ReturnsEmptyCorpus(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := 0; i < 3; i++ {
		writeFile(t, filepath.Join(root, fmt.Sprintf("f%d.txt", i)), "anything")
	}

	logger := quietLogger()
	c := &counter.Counter{
		Workers:   2,
		Tokenizer: alwaysFailTokenizer{},
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}
	_, err := c.Run(context.Background(), root)
	require.Error(t, err)
	require.True(t, errors.Is(err, counter.ErrEmptyCorpus), "want ErrEmptyCorpus, got %v", err)
}

func TestRun_Cancellation_NoGoroutineLeak(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		writeFile(t, filepath.Join(root, fmt.Sprintf("f%04d.txt", i)),
			strings.Repeat("alpha beta gamma delta\n", 50))
	}

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	logger := quietLogger()
	c := &counter.Counter{
		Workers:   8,
		Tokenizer: tokenizer.New(0),
		Walker:    scanWalker{logger: logger},
		Logger:    logger,
	}

	doneCh := make(chan struct{})
	var runErr error
	go func() {
		_, runErr = c.Run(ctx, root)
		close(doneCh)
	}()

	time.Sleep(5 * time.Millisecond)
	cancel()

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after cancel")
	}

	require.Error(t, runErr)
	require.True(t, errors.Is(runErr, context.Canceled), "want context.Canceled, got %v", runErr)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
}

func BenchmarkRun_Workers(b *testing.B) {
	root := b.TempDir()
	chunk := strings.Repeat(
		"the quick brown fox jumps over the lazy dog well-known phrase\n", 200)
	const fileCount = 64
	var totalBytes int64
	for i := 0; i < fileCount; i++ {
		path := filepath.Join(root, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(path, []byte(chunk), 0o644); err != nil {
			b.Fatal(err)
		}
		totalBytes += int64(len(chunk))
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	for _, k := range []int{1, 2, 4, 8, runtime.NumCPU()} {
		k := k
		b.Run(fmt.Sprintf("workers=%02d", k), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(totalBytes)
			for i := 0; i < b.N; i++ {
				c := &counter.Counter{
					Workers:   k,
					Tokenizer: tokenizer.New(3),
					Walker:    scanWalker{logger: logger},
					Logger:    logger,
				}
				if _, err := c.Run(context.Background(), root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRun_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    *counter.Counter
	}{
		{
			name: "zero workers",
			c:    &counter.Counter{Workers: 0, Tokenizer: tokenizer.New(0), Walker: listWalker{}},
		},
		{
			name: "nil tokenizer",
			c:    &counter.Counter{Workers: 2, Tokenizer: nil, Walker: listWalker{}},
		},
		{
			name: "nil walker",
			c:    &counter.Counter{Workers: 2, Tokenizer: tokenizer.New(0), Walker: nil},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.c.Run(context.Background(), t.TempDir())
			require.Error(t, err)
		})
	}
}
