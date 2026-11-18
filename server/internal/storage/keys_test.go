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
