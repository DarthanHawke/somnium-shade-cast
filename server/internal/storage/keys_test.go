package storage_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// TestKeyManagerUnwrap проверяет восстановление исходного 32-байтного ключа файла и ошибки
// для чужого мастер-ключа, повреждённой и слишком короткой обёртки.
func TestKeyManagerUnwrap(t *testing.T) {
	manager, err := storage.NewKeyManager("first", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	other, err := storage.NewKeyManager("second", bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	dek, wrapped := manager.NewDEK()
	if len(dek) != 32 {
		t.Fatalf("file key length = %d, want 32", len(dek))
	}
	plain, err := manager.Unwrap(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, dek) {
		t.Fatal("unwrapped file key differs from the original")
	}

	tampered := bytes.Clone(wrapped)
	tampered[len(tampered)-1] ^= 1
	tests := []struct {
		name    string
		manager *storage.KeyManager
		wrapped []byte
		want    error
	}{
		{name: "wrong_master_key", manager: other, wrapped: wrapped, want: domain.ErrDecryptionFailed},
		{name: "tampered_tag", manager: manager, wrapped: tampered, want: domain.ErrDecryptionFailed},
		{name: "too_short", manager: manager, wrapped: []byte{1}, want: domain.ErrInvalidWrappedKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.manager.Unwrap(tc.wrapped); !errors.Is(err, tc.want) {
				t.Fatalf("Unwrap error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestReadMasterKey проверяет чтение ровно 32 байт с сохранением непрочитанного хвоста и
// исходной ошибки чтения.
func TestReadMasterKey(t *testing.T) {
	t.Run("leaves_tail_unread", func(t *testing.T) {
		want := bytes.Repeat([]byte{3}, 32)
		input := bytes.NewReader(append(bytes.Clone(want), 7, 8))
		got, err := storage.ReadMasterKey(input)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) || input.Len() != 2 {
			t.Fatalf("unexpected key or unread tail length = %d", input.Len())
		}
	})
	t.Run("preserves_source_error", func(t *testing.T) {
		cause := errors.New("key source unavailable")
		_, err := storage.ReadMasterKey(brokenReader{err: cause})
		if !errors.Is(err, domain.ErrInvalidMasterKey) || !errors.Is(err, cause) {
			t.Fatalf("ReadMasterKey error = %v, want ErrInvalidMasterKey and %v", err, cause)
		}
	})
}
