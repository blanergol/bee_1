package tokenizer_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/redmadrobot/topwords/internal/tokenizer"
)

func collect(t *testing.T, tk *tokenizer.Tokenizer, in string) []string {
	t.Helper()
	var got []string
	err := tk.Tokenize(context.Background(), strings.NewReader(in), func(w string) bool {
		got = append(got, w)
		return true
	})
	require.NoError(t, err)
	return got
}

func TestTokenize_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		minLength int
		want      []string
	}{
		{
			name:      "ascii basic",
			input:     "Hello, world!",
			minLength: 0,
			want:      []string{"hello", "world"},
		},
		{
			name:      "cyrillic",
			input:     "Здравствуй, мир!",
			minLength: 0,
			want:      []string{"здравствуй", "мир"},
		},
		{
			name:      "mixed cyrillic and latin",
			input:     "Hello, мир!",
			minLength: 0,
			want:      []string{"hello", "мир"},
		},
		{
			name:      "hyphenated words",
			input:     "well-known multi-word",
			minLength: 0,
			want:      []string{"well-known", "multi-word"},
		},
		{
			name:      "apostrophe contraction",
			input:     "It's isn't",
			minLength: 0,
			want:      []string{"it's", "isn't"},
		},
		{
			name:      "pure numbers are NOT words",
			input:     "в 2024 году. 42 раза.",
			minLength: 0,
			want:      []string{"в", "году", "раза"},
		},
		{
			name:      "alphanumeric tokens kept",
			input:     "H2O is mp3 ipv4",
			minLength: 0,
			want:      []string{"h2o", "is", "mp3", "ipv4"},
		},
		{
			name:      "case folding",
			input:     "Hello HELLO hello",
			minLength: 0,
			want:      []string{"hello", "hello", "hello"},
		},
		{
			name:      "filter shorter or equal",
			input:     "code coder coding",
			minLength: 4,
			want:      []string{"coder", "coding"},
		},
		{
			name:      "filter multibyte runes by codepoints",
			input:     "мы они да",
			minLength: 2,
			want:      []string{"они"},
		},
		{
			name:      "empty input",
			input:     "",
			minLength: 0,
			want:      nil,
		},
		{
			name:      "punctuation only",
			input:     "...,,, !?",
			minLength: 0,
			want:      nil,
		},
		{
			name:      "multiline",
			input:     "first line\nsecond line\nthird",
			minLength: 0,
			want:      []string{"first", "line", "second", "line", "third"},
		},
		{
			name:      "filter all out",
			input:     "a b c",
			minLength: 5,
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tk := tokenizer.New(tc.minLength)
			got := collect(t, tk, tc.input)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("token diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokenize_NilArgs(t *testing.T) {
	t.Parallel()

	tk := tokenizer.New(0)
	require.Error(t, tk.Tokenize(context.Background(), nil, func(string) bool { return true }))
	require.Error(t, tk.Tokenize(context.Background(), strings.NewReader("x"), nil))
}

func TestTokenize_NegativeMinLength(t *testing.T) {
	t.Parallel()

	tk := tokenizer.New(-5)
	require.Equal(t, 0, tk.MinLength)
}

func TestTokenize_EmitStop(t *testing.T) {
	t.Parallel()

	tk := tokenizer.New(0)
	var got []string
	err := tk.Tokenize(context.Background(), strings.NewReader("one two three four"),
		func(w string) bool {
			got = append(got, w)
			return len(got) < 2
		})
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, got)
}

func TestTokenize_ContextCancelled(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 0; i < 10_000; i++ {
		sb.WriteString("alpha beta gamma\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tk := tokenizer.New(0)
	err := tk.Tokenize(ctx, strings.NewReader(sb.String()), func(string) bool { return true })
	require.ErrorIs(t, err, context.Canceled)
}

func TestTokenize_ContextDeadline(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		for {
			if _, err := pw.Write([]byte("line of text here\n")); err != nil {
				return
			}
		}
	}()

	tk := tokenizer.New(0)
	err := tk.Tokenize(ctx, pr, func(string) bool { return true })
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTokenize_LongStreamWithoutNewlines(t *testing.T) {
	t.Parallel()

	const repeats = 100_000
	chunk := "alpha beta gamma "
	var sb strings.Builder
	sb.Grow(len(chunk) * repeats)
	for i := 0; i < repeats; i++ {
		sb.WriteString(chunk)
	}

	tk := tokenizer.New(0)
	counts := map[string]int{}
	err := tk.Tokenize(context.Background(), strings.NewReader(sb.String()), func(w string) bool {
		counts[w]++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, repeats, counts["alpha"])
	require.Equal(t, repeats, counts["beta"])
	require.Equal(t, repeats, counts["gamma"])
}

func TestTokenize_TokenSplitOnChunkBoundary(t *testing.T) {
	t.Parallel()

	// Слово 'alphabet' длиннее чем ChunkSize=4, должно собраться через carry.
	tk := &tokenizer.Tokenizer{ChunkSize: 4}
	got := collect(t, tk, "alphabet zoo")
	require.Equal(t, []string{"alphabet", "zoo"}, got)
}

func TestTokenize_CyrillicAcrossChunkBoundary(t *testing.T) {
	t.Parallel()

	// «привет мир» содержит руны по 2 байта в UTF-8. Маленький ChunkSize
	// гарантирует, что граница chunk'а попадёт ВНУТРЬ многобайтовой руны.
	// Регрессия: ранее обрезанные continuation-байты терялись, давая
	// токены вроде "пр", "прив", "вет".
	for _, chunkSize := range []int{1, 2, 3, 4, 5, 7, 11, 13} {
		chunkSize := chunkSize
		t.Run(fmt.Sprintf("chunk=%d", chunkSize), func(t *testing.T) {
			t.Parallel()

			tk := &tokenizer.Tokenizer{ChunkSize: chunkSize}
			got := collect(t, tk, "привет мир привет мир")
			require.Equal(t, []string{"привет", "мир", "привет", "мир"}, got)
		})
	}
}

func TestTokenize_LongCyrillicStream(t *testing.T) {
	t.Parallel()

	const repeats = 20_000
	body := strings.Repeat("привет мир ", repeats)

	tk := tokenizer.New(0)
	counts := map[string]int{}
	err := tk.Tokenize(context.Background(), strings.NewReader(body), func(w string) bool {
		counts[w]++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, repeats, counts["привет"], "got counts: %v", counts)
	require.Equal(t, repeats, counts["мир"])
	require.Len(t, counts, 2, "spurious tokens leaked across chunk boundary: %v", counts)
}

func TestTokenize_EmojiAcrossChunkBoundary(t *testing.T) {
	t.Parallel()

	// Смешанный текст: слово + 4-байтовая руна (emoji вне BMP) + слово.
	// Emoji U+1F600 = 0xF0 0x9F 0x98 0x80. Регэкс не должен ловить его как
	// слово, но и не должен ломать соседние слова на границах chunk'а.
	tk := &tokenizer.Tokenizer{ChunkSize: 3}
	got := collect(t, tk, "alpha\U0001F600beta")
	require.Equal(t, []string{"alpha", "beta"}, got)
}

func TestTokenize_HugeTokenTriggersTruncation(t *testing.T) {
	t.Parallel()

	// MaxTokenSize=32: токен из 100 байт без разделителей будет
	// разрезан, но обработка завершится без ошибки.
	tk := &tokenizer.Tokenizer{ChunkSize: 16, MaxTokenSize: 32}
	huge := strings.Repeat("a", 100)
	var got []string
	err := tk.Tokenize(context.Background(), strings.NewReader(huge), func(w string) bool {
		got = append(got, w)
		return true
	})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	totalLen := 0
	for _, w := range got {
		totalLen += len(w)
	}
	require.Equal(t, 100, totalLen)
}

func TestTokenize_BinaryUTF8Garbage(t *testing.T) {
	t.Parallel()

	tk := tokenizer.New(0)
	garbage := []byte{0xff, 0xfe, 0x00, 0x01, 'a', 'b', 'c', 0xff}
	err := tk.Tokenize(context.Background(), bytes.NewReader(garbage), func(string) bool { return true })
	require.NoError(t, err)
}

func FuzzTokenize(f *testing.F) {
	seeds := []string{
		"",
		"hello world",
		"Здравствуй, мир!",
		"well-known it's",
		"\x00\x01\x02 abc",
		"line\nanother line",
		strings.Repeat("a", 1<<10),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		tk := tokenizer.New(0)
		_ = tk.Tokenize(context.Background(), strings.NewReader(in), func(string) bool { return true })

		tk2 := tokenizer.New(3)
		_ = tk2.Tokenize(context.Background(), strings.NewReader(in), func(w string) bool {
			n := 0
			for range w {
				n++
			}
			if n <= 3 {
				t.Fatalf("token %q has %d runes, want > 3", w, n)
			}
			return true
		})
	})
}

func BenchmarkTokenize_1MiB(b *testing.B) {
	chunk := "the quick brown fox jumps over the lazy dog well-known phrase здравствуй мир\n"
	var sb strings.Builder
	for sb.Len() < 1<<20 {
		sb.WriteString(chunk)
	}
	data := sb.String()

	tk := tokenizer.New(3)
	ctx := context.Background()

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		count := 0
		err := tk.Tokenize(ctx, strings.NewReader(data), func(string) bool {
			count++
			return true
		})
		if err != nil {
			b.Fatalf("tokenize: %v", err)
		}
		if count == 0 {
			b.Fatal("no tokens")
		}
	}
}
