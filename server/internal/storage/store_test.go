package storage_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// testStore создаёт хранилище в указанном каталоге с новым тестовым мастер-ключом.
func testStore(t *testing.T, root string) *storage.BlobStore {
	t.Helper()
	mk := make([]byte, 32)
	rand.Read(mk)
	km, err := storage.NewKeyManager("test-key-1", mk)
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.NewBlobStore(root, km)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// writeBlob записывает данные в зашифрованный файл и собирает метаданные блоба для
// последующего чтения.
func writeBlob(t *testing.T, s *storage.BlobStore, data []byte) *domain.Blob {
	t.Helper()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	result, err := s.Write(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.NewBlob(
		sha, int64(len(data)), result.SizeEncrypted,
		"audio/mpeg", domain.BlobAudio, result.WrappedKey,
		result.MasterKeyID, result.ChunkSize, result.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// checkReadToEOF сверяет ожидаемые байты и проверяет, что повторные чтения после конца
// возвращают ноль байтов и io.EOF.
func checkReadToEOF(t *testing.T, src io.Reader, want []byte) {
	t.Helper()
	got, err := io.ReadAll(io.LimitReader(src, int64(len(want))))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("read data differs: got %d bytes, want %d", len(got), len(want))
	}
	var extra [1]byte
	for i := 0; i < 2; i++ {
		if n, err := src.Read(extra[:]); n != 0 || err != io.EOF {
			t.Fatalf("read after end %d = (%d, %v), want (0, EOF)", i+1, n, err)
		}
	}
}

// TestRoundtrip проверяет обратимость записи и чтения и расчёт размера шифротекста для
// файлов по обе стороны границы чанка.
func TestRoundtrip(t *testing.T) {
	const chunkSize = domain.DefaultChunkSize
	tests := []struct {
		name   string
		size   int
		chunks int
	}{
		{name: "one_byte", size: 1, chunks: 1},
		{name: "before_chunk_boundary", size: chunkSize - 1, chunks: 1},
		{name: "at_chunk_boundary", size: chunkSize, chunks: 1},
		{name: "after_chunk_boundary", size: chunkSize + 1, chunks: 2},
		{name: "multiple_chunks_with_tail", size: 3*chunkSize + 12345, chunks: 4},
	}

	store := testStore(t, t.TempDir())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, tc.size)
			rand.Read(data)
			blob := writeBlob(t, store, data)
			wantSize := int64(tc.size) + int64(tc.chunks)*domain.GCMOverhead
			if blob.SizeEncrypted != wantSize {
				t.Fatalf("encrypted size = %d, want %d", blob.SizeEncrypted, wantSize)
			}
			rc, err := store.Open(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			checkReadToEOF(t, rc, data)
		})
	}
}

// TestRangeReads сверяет границы и расшифрованные байты диапазонов, включая переход между
// чанками и ограничение концом файла.
func TestRangeReads(t *testing.T) {
	const chunkSize = domain.DefaultChunkSize
	store := testStore(t, t.TempDir())
	data := make([]byte, 3*chunkSize+999)
	rand.Read(data)

	blob := writeBlob(t, store, data)
	size := int64(len(data))
	tests := []struct {
		name                       string
		offset, length, wantLength int64
	}{
		{name: "start", offset: 0, length: 10, wantLength: 10},
		{name: "tail", offset: size - 10, length: 10, wantLength: 10},
		{name: "crosses_chunk_boundary", offset: chunkSize - 5, length: 10, wantLength: 10},
		{name: "whole_chunk", offset: chunkSize, length: chunkSize, wantLength: chunkSize},
		{name: "open_end", offset: 7, length: -1, wantLength: size - 7},
		{name: "last_byte", offset: size - 1, length: 1, wantLength: 1},
		{name: "clamped_to_end", offset: size - 10, length: 20, wantLength: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc, rng, err := store.OpenRange(blob, tc.offset, tc.length)
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			wantRange := domain.ByteRange{Offset: tc.offset, Length: tc.wantLength}
			if rng != wantRange {
				t.Fatalf("range = %+v, want %+v", rng, wantRange)
			}
			checkReadToEOF(t, rc, data[tc.offset:tc.offset+tc.wantLength])
		})
	}
}

// terminalErrorReader имитирует источник, который возвращает последние данные вместе с
// заданной ошибкой.
type terminalErrorReader struct {
	data []byte
	err  error
}

// Read копирует доступные данные и возвращает заданную ошибку вместе с последними байтами.
func (r *terminalErrorReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

// TestWriteSourceErrorRemovesPartialFile проверяет, что ошибка источника сохраняется,
// результат записи отсутствует и хранилище не оставляет частичный файл.
func TestWriteSourceErrorRemovesPartialFile(t *testing.T) {
	sentinel := errors.New("source disconnected")
	tests := []struct {
		name  string
		size  int
		cause error
	}{
		{name: "error_after_full_chunk", size: domain.DefaultChunkSize + 17, cause: sentinel},
		{name: "unexpected_eof_from_source", size: 17, cause: io.ErrUnexpectedEOF},
		{name: "error_with_full_chunk", size: domain.DefaultChunkSize, cause: sentinel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := testStore(t, root)
			result, err := store.Write(&terminalErrorReader{
				data: make([]byte, tc.size),
				err:  tc.cause,
			})
			if !errors.Is(err, domain.ErrIncomplete) || !errors.Is(err, tc.cause) {
				t.Errorf("Write error = %v, want ErrIncomplete and %v", err, tc.cause)
			}
			if result != nil {
				t.Errorf("Write returned a result for a failed upload")
			}
			err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					t.Errorf("failed Write left file %q", path)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
