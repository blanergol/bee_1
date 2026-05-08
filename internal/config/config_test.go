package config_test

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redmadrobot/topwords/internal/config"
	"github.com/redmadrobot/topwords/internal/output"
)

func TestParseFlags_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want config.Config
	}{
		{
			name: "minimal",
			args: []string{"--dir", "/tmp/x"},
			want: config.Config{
				Dir: "/tmp/x", MinLength: 4, Top: 10,
				Format: output.FormatText, LogLevel: config.LogInfo,
			},
		},
		{
			name: "all flags set",
			args: []string{
				"--dir", "/data",
				"--min-length", "0",
				"--top", "5",
				"--workers", "2",
				"--format", "json",
				"--log-level", "warn",
			},
			want: config.Config{
				Dir: "/data", MinLength: 0, Top: 5, Workers: 2,
				Format: output.FormatJSON, LogLevel: config.LogWarn,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			got, err := config.ParseFlags(tc.args, &stderr)
			require.NoError(t, err)

			if tc.want.Workers == 0 {
				require.Greater(t, got.Workers, 0)
				got.Workers = 0
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseFlags_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{"missing dir", []string{}},
		{"empty dir", []string{"--dir", "  "}},
		{"negative min-length", []string{"--dir", "/x", "--min-length", "-1"}},
		{"zero top", []string{"--dir", "/x", "--top", "0"}},
		{"zero workers", []string{"--dir", "/x", "--workers", "0"}},
		{"unknown format", []string{"--dir", "/x", "--format", "xml"}},
		{"unknown log-level", []string{"--dir", "/x", "--log-level", "trace"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			_, err := config.ParseFlags(tc.args, &stderr)
			require.Error(t, err)
			require.True(t, errors.Is(err, config.ErrInvalidConfig),
				"want ErrInvalidConfig, got %v", err)
			require.NotEmpty(t, stderr.String())
		})
	}
}

func TestParseFlags_Help(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	_, err := config.ParseFlags([]string{"--help"}, &stderr)
	require.True(t, errors.Is(err, flag.ErrHelp))
	require.Contains(t, stderr.String(), "Usage:")
}

func TestBuildLogger(t *testing.T) {
	t.Parallel()

	for _, lvl := range []config.LogLevel{config.LogDebug, config.LogInfo, config.LogWarn, config.LogError} {
		lg, err := config.BuildLogger(lvl, io.Discard)
		require.NoError(t, err, "level %s", lvl)
		require.NotNil(t, lg)
	}

	_, err := config.BuildLogger("nope", io.Discard)
	require.Error(t, err)
	require.True(t, errors.Is(err, config.ErrInvalidConfig))
}
