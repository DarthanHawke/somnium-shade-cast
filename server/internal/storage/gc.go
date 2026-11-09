package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BlobPathChecker проверяет регистрацию файловых путей в метаданных блобов.
type BlobPathChecker interface {
	// PathExists сообщает, зарегистрирован ли относительный путь rel.
	// Наличие самого файла на диске не проверяет.
	PathExists(ctx context.Context, rel string) (bool, error)
}

// GC удаляет старые временные файлы и файлы, не зарегистрированные в checker.
// Обрабатывает обычные файлы, изменённые не позднее чем minAge назад.
// minAge должен быть неотрицательным.
// При ошибке возвращает число уже удалённых файлов и причину остановки.
// Вызывающий код должен обеспечить безопасность запуска относительно
// незавершённых загрузок: minAge сам по себе не обеспечивает синхронизацию.
func (s *BlobStore) GC(
	ctx context.Context,
	checker BlobPathChecker,
	minAge time.Duration,
) (int, error) {
	const op = "storage.GC"

	if minAge < 0 {
		return 0, fmt.Errorf("%s: minAge must not be negative", op)
	}

	cutoff := time.Now().Add(-minAge)
	removed := 0

	walkErr := filepath.WalkDir(
		s.root,
		func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			if walkErr != nil {
				if errors.Is(walkErr, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("walk %q: %v", path, walkErr)
			}

			if d.IsDir() {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("stat %q: %v", path, err)
			}

			if !info.Mode().IsRegular() {
				return nil
			}

			if info.ModTime().After(cutoff) {
				return nil
			}

			rel, err := filepath.Rel(s.root, path)
			if err != nil {
				return fmt.Errorf("relative path %q: %v", path, err)
			}

			isTemp := strings.HasPrefix(d.Name(), ".upload-")

			if !isTemp {
				known, err := checker.PathExists(ctx, rel)
				if err != nil {
					return fmt.Errorf("checker %q: %w", rel, err)
				}

				if known {
					return nil
				}
			}

			if err := ctx.Err(); err != nil {
				return err
			}

			if err := os.Remove(path); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("remove %q: %v", rel, err)
			}

			removed++
			return nil
		},
	)

	if walkErr != nil {
		return removed, fmt.Errorf("%s: %w", op, walkErr)
	}

	return removed, nil
}
