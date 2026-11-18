package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// GarbageRepository предоставляет операции обхода и удаления сиротских блобов.
type GarbageRepository interface {
	BlobPathChecker

	// PruneOrphans атомарно удаляет не более limit старых блобов без ссылок.
	// После успешного commit возвращает пути их файлов.
	PruneOrphans(ctx context.Context, before time.Time, limit int) ([]string, error)
}

// Collector удаляет сиротские записи БД и зашифрованные файлы.
type Collector struct {
	store *BlobStore
	blobs GarbageRepository
	log   *slog.Logger
}

// NewCollector создаёт сборщик файлового хранилища.
func NewCollector(store *BlobStore, blobs GarbageRepository, log *slog.Logger) *Collector {
	return &Collector{store: store, blobs: blobs, log: log}
}

// Collect удаляет старые записи без ссылок и старые незарегистрированные файлы.
// После удаления записи её файл может быть удалён повторным запуском Collect.
func (c *Collector) Collect(ctx context.Context, minAge time.Duration) error {
	if minAge < 0 {
		return errors.New("storage.Collect: minAge must not be negative")
	}

	const batchSize = 128
	before := time.Now().Add(-minAge)
	var removed int
	var removeErr error
	for ctx.Err() == nil {
		paths, err := c.prune(ctx, before, batchSize)
		if err != nil {
			return errors.Join(removeErr, err)
		}
		removed += len(paths)
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return errors.Join(removeErr, err)
			}
			if !isManagedBlobPath(path) {
				if removeErr == nil {
					removeErr = fmt.Errorf("storage.Collect: unexpected blob path %q", path)
				}
				continue
			}
			if err := c.store.Remove(path); err != nil && removeErr == nil {
				removeErr = fmt.Errorf("remove pruned blob %q: %w", path, err)
			}
		}
		if len(paths) < batchSize {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(removeErr, err)
	}

	swept, err := c.store.GC(ctx, c.blobs, minAge)
	c.log.InfoContext(ctx, "blob collection finished", "records_pruned", removed, "unregistered_files_removed", swept)

	return errors.Join(removeErr, err)
}

func (c *Collector) prune(ctx context.Context, before time.Time, limit int) ([]string, error) {
	// Берём блокировку перед обращением к БД, как при загрузке.
	c.store.mutations.Lock()
	defer c.store.mutations.Unlock()
	return c.blobs.PruneOrphans(ctx, before, limit)
}
