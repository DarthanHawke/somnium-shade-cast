package storage_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// pathChecker адаптирует функцию проверки пути к интерфейсу, который использует GC.
type pathChecker func(context.Context, string) (bool, error)

// PathExists передаёт контекст и путь тестовой функции проверки.
func (f pathChecker) PathExists(ctx context.Context, path string) (bool, error) {
	return f(ctx, path)
}

// TestGCPreservesKnownAndFreshFiles проверяет, что GC сохраняет зарегистрированные и свежие
// файлы, удаляя старый блоб и временный файл.
// Дополнительно проверяет передачу ошибки проверки пути, отмену и отказ при отрицательном
// защитном интервале.
func TestGCPreservesKnownAndFreshFiles(t *testing.T) {
	root := t.TempDir()
	keys, err := storage.NewKeyManager("test", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewBlobStore(root, keys)
	if err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join("ab", "cd")
	if err := os.MkdirAll(filepath.Join(root, shard), 0o700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(shard, "abcd"+strings.Repeat("0", 60)+".enc")
	orphan := filepath.Join(shard, "abcd"+strings.Repeat("1", 60)+".enc")
	fresh := filepath.Join(shard, "abcd"+strings.Repeat("2", 60)+".enc")
	temp := filepath.Join(shard, ".upload-stale")
	unmanaged := filepath.Join(shard, "library.sqlite")
	for _, name := range []string{known, orphan, temp, fresh, unmanaged} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("test cipher bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		if name != fresh {
			old := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	removed, err := store.GC(
		context.Background(),
		pathChecker(func(_ context.Context, name string) (bool, error) {
			return name == filepath.ToSlash(known), nil
		}), time.Hour)
	if err != nil || removed != 2 {
		t.Fatalf("removed=%d, err=%v", removed, err)
	}
	for _, name := range []string{known, fresh, unmanaged} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := errors.New("database unavailable")
	removed, err = store.GC(
		context.Background(),
		pathChecker(func(context.Context, string) (bool, error) {
			return false, sentinel
		}), 0)
	if removed != 0 || !errors.Is(err, sentinel) {
		t.Fatalf("checker error swallowed: removed=%d, err=%v", removed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.GC(ctx, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := store.GC(context.Background(), nil, -time.Second); err == nil {
		t.Fatal("negative grace period accepted")
	}
}

// TestGCRechecksPathAfterUpload проверяет путь, зарегистрированный между
// первым чтением метаданных и моментом, когда GC сможет удалить файл.
func TestGCRechecksPathAfterUpload(t *testing.T) {
	root := t.TempDir()
	keys, err := storage.NewKeyManager("test", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewBlobStore(root, keys)
	if err != nil {
		t.Fatal(err)
	}
	release := store.ProtectUpload()
	var releaseOnce sync.Once
	defer releaseOnce.Do(release)

	written, err := store.Write(bytes.NewReader([]byte("upload in progress")))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	firstCheck := make(chan struct{})
	result := make(chan error, 1)
	var registered atomic.Bool
	var calls int
	go func() {
		_, err := store.GC(ctx, pathChecker(func(_ context.Context, path string) (bool, error) {
			if path != written.StoragePath {
				return false, nil
			}
			calls++
			if calls == 1 {
				close(firstCheck)
				return false, nil
			}
			return registered.Load(), nil
		}), 0)
		result <- err
	}()
	select {
	case <-firstCheck:
	case <-ctx.Done():
		t.Fatal("GC did not reach the upload")
	}

	registered.Store(true)
	releaseOnce.Do(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("GC did not finish after upload")
	}
	if calls != 2 {
		t.Fatalf("path checks=%d, want 2", calls)
	}
	if _, err := os.Stat(filepath.Join(root, written.StoragePath)); err != nil {
		t.Fatalf("registered file was removed: %v", err)
	}
}
