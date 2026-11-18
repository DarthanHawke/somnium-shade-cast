package storage

import (
	"context"
	"encoding/hex"
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

// GC удаляет старые временные и незарегистрированные файлы блобов.
// Обрабатывает обычные файлы, изменённые не позднее чем minAge назад.
// minAge должен быть неотрицательным.
// Возвращает число удалённых файлов и первую ошибку удаления, если она была.
// Ошибка обхода или проверки метаданных останавливает проход.
// Записывающий код должен защищать запись и регистрацию через ProtectUpload.
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
	var removeErr error

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

			rel, err := gcCandidate(s.root, path, d, cutoff)
			if err != nil || rel == "" {
				return err
			}

			isTemp := strings.HasPrefix(d.Name(), ".upload-")
			if !isTemp {
				// Зарегистрированные файлы составляют основной объём хранилища.
				// Для потенциальной сироты повторно проверяем путь под блокировкой.
				known, err := checker.PathExists(ctx, rel)
				if err != nil {
					return fmt.Errorf("checker %q: %w", rel, err)
				}
				if known {
					return nil
				}
			}

			s.mutations.Lock()
			defer s.mutations.Unlock()

			if err := ctx.Err(); err != nil {
				return err
			}

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
				if removeErr == nil {
					removeErr = fmt.Errorf("remove %q: %v", rel, err)
				}
				return nil
			}

			removed++
			return nil
		},
	)

	if err := errors.Join(removeErr, walkErr); err != nil {
		return removed, fmt.Errorf("%s: %w", op, err)
	}

	return removed, nil
}

// gcCandidate возвращает ключ старого управляемого файла или пустую строку.
func gcCandidate(root, path string, entry fs.DirEntry, cutoff time.Time) (string, error) {
	if entry.IsDir() {
		return "", nil
	}
	info, err := entry.Info()
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
		return "", nil
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("relative path %q: %w", path, err)
	}
	rel = canonicalStoragePath(rel)
	if !isManagedBlobPath(rel) {
		return "", nil
	}
	return rel, nil
}

func isManagedBlobPath(rel string) bool {
	parts := strings.Split(canonicalStoragePath(rel), "/")
	if len(parts) != 3 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return false
	}
	shard := parts[0] + parts[1]
	name := parts[2]
	if strings.HasPrefix(name, ".upload-") {
		_, err := hex.DecodeString(shard)
		return err == nil
	}
	id, ok := strings.CutSuffix(name, ".enc")
	if !ok || len(id) != 64 || !strings.HasPrefix(id, shard) {
		return false
	}
	var decoded [32]byte
	_, err := hex.Decode(decoded[:], []byte(id))
	return err == nil
}
