package storage_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
// файлы, удаляя старый сиротский блоб и временный файл.
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
	for _, name := range []string{"known.enc", "orphan.enc", ".upload-stale", "fresh.enc"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("test cipher bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		if name != "fresh.enc" {
			old := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	removed, err := store.GC(
		context.Background(),
		pathChecker(func(_ context.Context, name string) (bool, error) {
			return name == "known.enc", nil
		}), time.Hour)
	if err != nil || removed != 2 {
		t.Fatalf("removed=%d, err=%v", removed, err)
	}
	for _, name := range []string{"known.enc", "fresh.enc"} {
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
