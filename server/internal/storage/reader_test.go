package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// TestNewBlobReaderLeavesFileOpenOnError проверяет, что при неверном ключе или недоступном
// первом чанке открытый файл остаётся во владении вызывающего кода.
func TestNewBlobReaderLeavesFileOpenOnError(t *testing.T) {
	tests := []struct {
		name string
		dek  []byte
	}{
		{name: "invalid_key", dek: []byte{1}},
		{name: "missing_first_chunk", dek: make([]byte, 32)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := os.Create(filepath.Join(t.TempDir(), "empty.enc"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			blob := &domain.Blob{SizePlain: 1, ChunkSize: domain.DefaultChunkSize}
			plan := blob.Plan(domain.ByteRange{Offset: 0, Length: 1})
			if _, err := newBlobReader(file, tc.dek, blob, plan); err == nil {
				t.Fatal("expected reader initialization to fail")
			}
			if _, err := file.Stat(); err != nil {
				t.Fatalf("caller no longer owns an open file: %v", err)
			}
		})
	}
}
