package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestRun_TextOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"),
		"alpha alpha alpha beta beta gamma\n")
	writeFile(t, filepath.Join(root, "sub", "b.txt"),
		"alpha beta beta beta delta\n")

	var stdout, stderr bytes.Buffer
	args := []string{
		"--dir", root,
		"--min-length", "0",
		"--top", "3",
		"--workers", "2",
		"--format", "text",
		"--log-level", "error",
	}
	rc := run(context.Background(), args, &stdout, &stderr)
	require.Equal(t, exitOK, rc, "stderr=%s", stderr.String())

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	// alpha=4, beta=5 → topk: 1=beta(5), 2=alpha(4), 3=gamma|delta тай-брейк
	require.Equal(t, "1\tbeta\t5", lines[0])
	require.Equal(t, "2\talpha\t4", lines[1])
	// при равной частоте 1/1 — сначала delta, потом gamma по лексикографии
	require.Equal(t, "3\tdelta\t1", lines[2])
}

func TestRun_JSONOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "one one two two two three")

	var stdout, stderr bytes.Buffer
	args := []string{
		"--dir", root,
		"--min-length", "0",
		"--top", "5",
		"--format", "json",
		"--log-level", "error",
	}
	rc := run(context.Background(), args, &stdout, &stderr)
	require.Equal(t, exitOK, rc, "stderr=%s", stderr.String())

	var rows []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.Len(t, rows, 3)
	require.Equal(t, "two", rows[0]["word"])
	require.Equal(t, float64(3), rows[0]["count"])
}

func TestRun_MissingDir_UsageError(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "nope")
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"--dir", missing}, &stdout, &stderr)
	require.Equal(t, exitUsageError, rc)
	require.Contains(t, stderr.String(), "topwords:")
}

func TestRun_DirIsFile_UsageError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "regular.txt")
	writeFile(t, file, "x")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"--dir", file}, &stdout, &stderr)
	require.Equal(t, exitUsageError, rc)
	require.Contains(t, stderr.String(), "is not a directory")
}

func TestRun_MissingDirFlag_UsageError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{}, &stdout, &stderr)
	require.Equal(t, exitUsageError, rc)
}

func TestRun_HelpExitsZero(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"--help"}, &stdout, &stderr)
	require.Equal(t, exitOK, rc)
	require.Contains(t, stderr.String(), "Usage:")
}

func TestRun_EmptyDir_NoError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{
		"--dir", root,
		"--log-level", "error",
	}, &stdout, &stderr)
	require.Equal(t, exitOK, rc, "stderr=%s", stderr.String())
	require.Empty(t, stdout.String())
}

func TestRun_Cancellation_ReturnsSignaled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := 0; i < 100; i++ {
		writeFile(t, filepath.Join(root, "f"+string(rune('a'+i%26))+".txt"),
			strings.Repeat("alpha beta\n", 200))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	rc := run(ctx, []string{
		"--dir", root,
		"--log-level", "error",
	}, &stdout, &stderr)
	require.Equal(t, exitSignaled, rc)
}
