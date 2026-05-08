// Package scanner обходит дерево каталогов и эмитит пути к регулярным файлам.
package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
)

// Walk рекурсивно обходит root и вызывает emit для каждого регулярного файла.
// Каталоги не следуют по символическим ссылкам (использует [filepath.WalkDir]).
// Если emit возвращает ошибку, обход прекращается и эта ошибка возвращается
// наверх. Если ctx отменён, возвращает ctx.Err().
//
// logger используется только для отладочных сообщений о пропуске
// нерегулярных файлов; nil-логгер допустим (тогда используется
// [slog.Default]).
func Walk(ctx context.Context, root string, logger *slog.Logger, emit func(path string) error) error {
	if emit == nil {
		return fmt.Errorf("scanner: nil emit")
	}
	if logger == nil {
		logger = slog.Default()
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			logger.Debug("scanner: skipping non-regular file", "path", path, "mode", d.Type())
			return nil
		}
		return emit(path)
	})
	if err != nil {
		return fmt.Errorf("scanner: walk %q: %w", root, err)
	}
	return nil
}
