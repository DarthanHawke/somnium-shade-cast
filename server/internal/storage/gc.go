package storage

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// BlobPathChecker известен ли БД этот storage_path.
type BlobPathChecker interface {
	PathExists(ctx context.Context, rel string) (bool, error)
}

// GC находит потерянные файлы(кторых нет в бд)
func (s *BlobStore) GC(ctx context.Context, check BlobPathChecker, minAge time.Duration) (removed int, err error) {
	const op = "gc.GC"

	now := time.Now()
	err = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return &domain.WrappedError{
				Op:  op,
				Err: werr,
			}
		}
		if ctx.Err() != nil {
			return &domain.WrappedError{
				Op:  op,
				Err: ctx.Err(),
			}
		}
		base := d.Name()
		info, ierr := d.Info()
		if ierr != nil {
			if errors.Is(ierr, fs.ErrNotExist) {
				return nil // файл с момента чтения каталога не найден - норм
			}
			return &domain.WrappedError{
				Op:  op,
				Err: ierr,
			}
		}
		// незавершённые tmp старше minAge - мусор
		if now.Sub(info.ModTime()) < minAge {
			return nil
		}
		rel, _ := filepath.Rel(s.root, path)
		if strings.HasPrefix(base, ".upload-") {
			_ = s.Remove(rel)
			removed++
			return nil
		}
		known, cerr := check.PathExists(ctx, rel)
		if cerr != nil {
			return &domain.WrappedError{
				Op:  op,
				Err: cerr,
			}
		}
		if !known {
			_ = s.Remove(rel)
			removed++
		}
		return nil
	})
	return removed, &domain.WrappedError{
		Op:  op,
		Err: err,
	}
}
