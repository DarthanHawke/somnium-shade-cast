package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// BlobStore — файловая часть хранилища
type BlobStore struct {
	root string // корень хранилища, прим. /var/lib/musicapp/blobs
	keys *KeyManager
}

func NewBlobStore(root string, keys *KeyManager) (*BlobStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("storage: mkdir root: %w", err)
	}
	return &BlobStore{root: root, keys: keys}, nil
}

func (s *BlobStore) abs(rel string) string {
	return filepath.Join(s.root, rel)
}

// WriteResult - всё, что нужно для domain.NewBlob.
type WriteResult struct {
	SizeEncrypted int64
	WrappedKey    []byte
	MasterKeyID   string
	ChunkSize     int
}

// Write шифрует src и кладёт файл по пути StoragePathFor
func (s *BlobStore) Write(shaPlain string, src io.Reader) (*WriteResult, error) {
	const op = "store.Write"

	rel := domain.StoragePathFor(shaPlain)
	final := s.abs(rel)
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}

	dek, wrapped, err := s.keys.NewDEK()
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(final), ".upload-*")
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	encSize, err := encryptTo(tmp, src, dek, domain.DefaultChunkSize)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := tmp.Sync(); err != nil { // гарантия долговечности до commitа БД
		cleanup()
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}

	return &WriteResult{
		SizeEncrypted: encSize,
		WrappedKey:    wrapped,
		MasterKeyID:   s.keys.CurrentID(),
		ChunkSize:     domain.DefaultChunkSize,
	}, nil
}

// OpenRange открывает plaintext-диапазон блоба для отдачи в HTTP
func (s *BlobStore) OpenRange(b *domain.Blob, offset, length int64) (io.ReadCloser, domain.ByteRange, error) {
	const op = "store.OpenRange"

	rng, err := b.ResolveRange(offset, length)
	if err != nil {
		return nil, domain.ByteRange{}, err
	}
	dek, err := s.keys.Unwrap(b.EncKeyWrapped)
	if err != nil {
		return nil, domain.ByteRange{}, err
	}
	f, err := os.Open(s.abs(b.StoragePath))
	if err != nil {
		return nil, domain.ByteRange{}, fmt.Errorf("storage: open %s: %w", b.StoragePath, err)
	}
	r, err := newBlobReader(f, dek, b, b.Plan(rng))
	if err != nil {
		return nil, domain.ByteRange{}, err
	}
	return r, rng, nil
}

// Open - весь файл (download)
func (s *BlobStore) Open(b *domain.Blob) (io.ReadCloser, error) {
	const op = "store.Open"

	r, _, err := s.OpenRange(b, 0, -1)
	return r, &domain.WrappedError{
		Op:  op,
		Err: err,
	}
}

// Remove - удаление файла после commit
func (s *BlobStore) Remove(rel string) error {
	const op = "store.Remove"

	err := os.Remove(s.abs(rel))
	if err != nil && !os.IsNotExist(err) {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return nil
}
