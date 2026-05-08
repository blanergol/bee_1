// Package output форматирует ранжированные записи в выбранном формате.
// Расширяется через интерфейс [EntryWriter]: новый формат — это новый тип
// + регистрация в [WriterFor], без правки потребителя.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/redmadrobot/topwords/internal/topk"
)

// Format — идентификатор формата вывода.
type Format string

const (
	// FormatText — текстовый ранжированный список.
	FormatText Format = "text"
	// FormatJSON — JSON-массив с полями rank/word/count.
	FormatJSON Format = "json"
)

// ErrUnknownFormat возвращается [WriterFor], если запрошенный формат не
// зарегистрирован.
var ErrUnknownFormat = errors.New("output: unknown format")

// EntryWriter сериализует список записей в произвольный writer.
// Реализации stateless и потокобезопасны.
type EntryWriter interface {
	Write(w io.Writer, entries []topk.Entry) error
}

// TextWriter пишет ranked-list строками `<rank>\t<word>\t<count>\n`.
type TextWriter struct{}

// Write реализует [EntryWriter].
func (TextWriter) Write(w io.Writer, entries []topk.Entry) error {
	for i, e := range entries {
		if _, err := fmt.Fprintf(w, "%d\t%s\t%d\n", i+1, e.Word, e.Count); err != nil {
			return fmt.Errorf("output: write text: %w", err)
		}
	}
	return nil
}

// JSONWriter пишет ranked-list как JSON-массив с indent=2 и без HTML-эскейпинга.
type JSONWriter struct{}

type jsonRow struct {
	Rank  int    `json:"rank"`
	Word  string `json:"word"`
	Count int    `json:"count"`
}

// Write реализует [EntryWriter].
func (JSONWriter) Write(w io.Writer, entries []topk.Entry) error {
	rows := make([]jsonRow, len(entries))
	for i, e := range entries {
		rows[i] = jsonRow{Rank: i + 1, Word: e.Word, Count: e.Count}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		return fmt.Errorf("output: write json: %w", err)
	}
	return nil
}

// WriterFor возвращает реализацию [EntryWriter] для запрошенного формата.
// При неизвестном формате возвращает [ErrUnknownFormat].
func WriterFor(f Format) (EntryWriter, error) {
	switch f {
	case FormatText:
		return TextWriter{}, nil
	case FormatJSON:
		return JSONWriter{}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, f)
	}
}

// Validate возвращает nil, если формат поддерживается.
func Validate(f Format) error {
	_, err := WriterFor(f)
	return err
}
