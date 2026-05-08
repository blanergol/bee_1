package scanner_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/redmadrobot/topwords/internal/scanner"
)

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestWalk_NestedDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "alpha")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "beta")
	writeFile(t, filepath.Join(root, "sub", "deeper", "c.txt"), "gamma")

	var got []string
	err := scanner.Walk(context.Background(), root, nil, func(p string) error {
		rel, _ := filepath.Rel(root, p)
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)

	sort.Strings(got)
	want := []string{"a.txt", "sub/b.txt", "sub/deeper/c.txt"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("paths diff (-want +got):\n%s", diff)
	}
}

func TestWalk_EmptyDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var n int
	err := scanner.Walk(context.Background(), root, nil, func(string) error {
		n++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestWalk_MissingPath(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	err := scanner.Walk(context.Background(), missing, nil, func(string) error { return nil })
	require.Error(t, err)
	require.True(t, errors.Is(err, fs.ErrNotExist), "want fs.ErrNotExist, got %v", err)
}

func TestWalk_NilEmit(t *testing.T) {
	t.Parallel()

	require.Error(t, scanner.Walk(context.Background(), t.TempDir(), nil, nil))
}

func TestWalk_ContextCancelled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(root, "f", "f", "f", string(rune('a'+i%26))+".txt"), "x")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := scanner.Walk(ctx, root, nil, func(string) error { return nil })
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "want context.Canceled, got %v", err)
}

func TestWalk_EmitError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "x")
	writeFile(t, filepath.Join(root, "b.txt"), "y")

	sentinel := errors.New("boom")
	err := scanner.Walk(context.Background(), root, nil, func(string) error { return sentinel })
	require.Error(t, err)
	require.True(t, errors.Is(err, sentinel))
}
