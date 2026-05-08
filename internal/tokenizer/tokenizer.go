// Package tokenizer извлекает слова из произвольного потока через
// регулярное выражение и нормализует их к нижнему регистру. Чтение —
// chunked, без лимита на длину «строки»: файлы без переводов строк
// обрабатываются корректно, токен, разрезанный границей chunk'а,
// автоматически собирается из двух частей.
package tokenizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// wordPattern: слово ОБЯЗАНО начинаться с буквы (Unicode), может содержать
// буквы/цифры/дефисы/апострофы внутри, заканчивается буквой или цифрой
// (либо это одиночная буква). Это позволяет "mp3", "H2O", "well-known",
// "it's" — но отбрасывает чистые числовые токены ("2024", "42") по смыслу
// ТЗ.
const wordPattern = `\p{L}[\p{L}\p{N}'\-]*[\p{L}\p{N}]|\p{L}`

var wordRE = regexp.MustCompile(wordPattern)

// Параметры по умолчанию.
const (
	// DefaultChunkSize — размер блока чтения. 64 КиБ хорошо ложится в
	// L1/L2 кеш и совпадает с типичным буфером ОС.
	DefaultChunkSize = 64 * 1024
	// DefaultMaxTokenSize — предел длины токена, переносимого через
	// границу chunk'а. Защищает от неограниченного роста carry-буфера на
	// патологическом вводе (поток без разделителей). При превышении
	// токен эмитится как есть и carry сбрасывается.
	DefaultMaxTokenSize = 1 << 20
)

// Tokenizer извлекает токены из потока и фильтрует их по минимальной длине.
// Stateless: один экземпляр можно использовать конкурентно.
type Tokenizer struct {
	// MinLength — минимальная длина слова в кодпойнтах. В выдачу попадают
	// только токены длиной СТРОГО больше MinLength.
	MinLength int
	// ChunkSize — размер блока чтения. <=0 → DefaultChunkSize.
	ChunkSize int
	// MaxTokenSize — потолок длины токена (см. DefaultMaxTokenSize).
	// <=0 → DefaultMaxTokenSize.
	MaxTokenSize int
}

// New возвращает токенизатор с указанным минимальным порогом длины.
// Отрицательные значения нормализуются к нулю.
func New(minLength int) *Tokenizer {
	if minLength < 0 {
		minLength = 0
	}
	return &Tokenizer{MinLength: minLength}
}

// Tokenize читает r поблочно и для каждого подходящего слова вызывает emit.
// Если emit вернёт false, обработка прекращается. Возвращает ctx.Err() при
// отмене.
func (t *Tokenizer) Tokenize(ctx context.Context, r io.Reader, emit func(word string) bool) error {
	if r == nil {
		return errors.New("tokenizer: nil reader")
	}
	if emit == nil {
		return errors.New("tokenizer: nil emit")
	}

	chunkSize := t.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	maxToken := t.MaxTokenSize
	if maxToken <= 0 {
		maxToken = DefaultMaxTokenSize
	}

	buf := make([]byte, chunkSize)
	var carry []byte

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := r.Read(buf)
		eof := errors.Is(readErr, io.EOF)

		// io.Reader вправе вернуть (0, nil); это «попробуй ещё раз», не EOF.
		if n == 0 && !eof && readErr == nil {
			continue
		}

		var chunk []byte
		switch {
		case len(carry) > 0 && n > 0:
			chunk = make([]byte, len(carry)+n)
			copy(chunk, carry)
			copy(chunk[len(carry):], buf[:n])
			carry = nil
		case len(carry) > 0:
			chunk = carry
			carry = nil
		case n > 0:
			chunk = buf[:n]
		}

		if len(chunk) > 0 {
			// До запуска regex обрезаем хвост, в котором может оказаться
			// неполная UTF-8 последовательность (граница chunk'а попала
			// внутрь многобайтовой руны). Иначе regex видит RuneError на
			// последних байтах и обрезанные continuation-байты теряются.
			safeBoundary := len(chunk)
			if !eof {
				safeBoundary = lastFullRuneEnd(chunk)
			}

			indices := wordRE.FindAllIndex(chunk[:safeBoundary], -1)

			// Если последний матч примыкает к safeBoundary и это НЕ EOF,
			// токен может быть продолжен в следующем chunk'е — переносим
			// в carry вместе с обрезанным UTF-8 хвостом chunk'а.
			safeEnd := safeBoundary
			if !eof && len(indices) > 0 {
				last := indices[len(indices)-1]
				if last[1] == safeBoundary {
					safeEnd = last[0]
				}
			}

			for _, idx := range indices {
				if idx[1] > safeEnd {
					break
				}
				raw := chunk[idx[0]:idx[1]]
				if utf8.RuneCount(raw) <= t.MinLength {
					continue
				}
				word := strings.ToLower(string(raw))
				if !emit(word) {
					return nil
				}
			}

			if safeEnd < len(chunk) {
				tail := chunk[safeEnd:]
				if len(tail) > maxToken {
					// Аварийный сброс: токен слишком велик, эмитим
					// как есть и не накапливаем в carry.
					if utf8.RuneCount(tail) > t.MinLength {
						word := strings.ToLower(string(tail))
						if !emit(word) {
							return nil
						}
					}
				} else {
					carry = make([]byte, len(tail))
					copy(carry, tail)
				}
			}
		}

		if eof {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("tokenizer: read: %w", readErr)
		}
	}
}

// lastFullRuneEnd возвращает индекс конца последней полной UTF-8 руны в b.
// Если b пуст или весь хвост — обрезанная multibyte-последовательность,
// возвращается 0 (но это редкий случай, обычно есть хотя бы одна полная
// руна в начале).
func lastFullRuneEnd(b []byte) int {
	n := len(b)
	if n == 0 {
		return 0
	}
	// Идём с конца через continuation-байты (10xxxxxx), не более 3 шагов.
	cut := n
	for i := 0; i < 3 && cut > 0; i++ {
		if b[cut-1] < 0x80 || b[cut-1] >= 0xC0 {
			break
		}
		cut--
	}
	if cut == 0 {
		return 0
	}
	last := b[cut-1]
	if last < 0x80 {
		return n // ASCII byte перед хвостом — последняя руна полна
	}
	// last — leading byte; считаем, сколько continuation требуется.
	var expected int
	switch {
	case last < 0xC2:
		// Невалидный leading (overlong). Срезаем его.
		return cut - 1
	case last < 0xE0:
		expected = 1
	case last < 0xF0:
		expected = 2
	case last < 0xF8:
		expected = 3
	default:
		return cut - 1
	}
	if n-cut >= expected {
		return n // полная multibyte руна
	}
	return cut - 1
}
