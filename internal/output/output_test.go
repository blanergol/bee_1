package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/redmadrobot/topwords/internal/output"
	"github.com/redmadrobot/topwords/internal/topk"
)

func TestTextWriter(t *testing.T) {
	t.Parallel()

	entries := []topk.Entry{
		{Word: "alpha", Count: 5},
		{Word: "beta", Count: 3},
	}
	var buf bytes.Buffer
	require.NoError(t, output.TextWriter{}.Write(&buf, entries))
	want := "1\talpha\t5\n2\tbeta\t3\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("text diff (-want +got):\n%s", diff)
	}
}

func TestTextWriter_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, output.TextWriter{}.Write(&buf, nil))
	require.Empty(t, buf.String())
}

type writeFailer struct{}

func (writeFailer) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestTextWriter_PropagatesError(t *testing.T) {
	t.Parallel()

	err := output.TextWriter{}.Write(writeFailer{}, []topk.Entry{{Word: "x", Count: 1}})
	require.Error(t, err)
}

func TestJSONWriter(t *testing.T) {
	t.Parallel()

	entries := []topk.Entry{
		{Word: "alpha", Count: 5},
		{Word: "<beta>", Count: 3},
	}
	var buf bytes.Buffer
	require.NoError(t, output.JSONWriter{}.Write(&buf, entries))

	require.Contains(t, buf.String(), `"<beta>"`)

	var parsed []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	require.Len(t, parsed, 2)
	require.Equal(t, float64(1), parsed[0]["rank"])
	require.Equal(t, "alpha", parsed[0]["word"])
	require.Equal(t, float64(5), parsed[0]["count"])
}

func TestJSONWriter_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, output.JSONWriter{}.Write(&buf, nil))
	out := strings.TrimSpace(buf.String())
	require.True(t, out == "[]" || out == "null", "unexpected: %q", out)
}

func TestWriterFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format output.Format
		want   output.EntryWriter
		err    bool
	}{
		{"text", output.FormatText, output.TextWriter{}, false},
		{"json", output.FormatJSON, output.JSONWriter{}, false},
		{"unknown", output.Format("yaml"), nil, true},
		{"empty", output.Format(""), nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, err := output.WriterFor(tc.format)
			if tc.err {
				require.Error(t, err)
				require.True(t, errors.Is(err, output.ErrUnknownFormat))
				require.Nil(t, w)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tc.want, w)
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, output.Validate(output.FormatText))
	require.NoError(t, output.Validate(output.FormatJSON))
	require.Error(t, output.Validate(output.Format("xml")))
}
