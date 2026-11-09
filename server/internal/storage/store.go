// Package storage принимает файлы в память и хранит их зашифрованное содержимое на диске.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// BlobStore сохраняет зашифрованные файлы блобов и открывает их для чтения с расшифровкой.
type BlobStore struct {
	root string // корень хранилища, прим. /var/lib/musicapp/blobs
	keys *KeyManager
}

// WriteResult содержит путь и параметры сохранённого зашифрованного файла
// для последующей регистрации блоба.
type WriteResult struct {
	SizeEncrypted int64  // SizeEncrypted размер шифртекста в байтах, включая nonce и теги чанков.
	WrappedKey    []byte // WrappedKey файловый ключ, зашифрованный мастер-ключом.
	MasterKeyID   string // MasterKeyID идентификатор мастер-ключа, использованного для WrappedKey.
	ChunkSize     int    // ChunkSize размер полного незашифрованного чанка в байтах.
	StoragePath   string // StoragePath путь к файлу относительно корня BlobStore.
}

// NewBlobStore создаёт файловое хранилище в root, используя keys для файловых ключей.
// Создаёт корневой каталог, если он отсутствует.
// keys должен быть задан.
func NewBlobStore(root string, keys *KeyManager) (*BlobStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("storage: mkdir root: %w", err)
	}
	return &BlobStore{root: root, keys: keys}, nil
}

// blobPath возвращает абсолютный путь к файлу относительно корня хранилища.
func (s *BlobStore) blobPath(rel string) string {
	return filepath.Join(s.root, rel)
}

// newStoragePath создаёт случайный относительный путь с двумя уровнями каталогов.
func newStoragePath() string {
	var id [32]byte
	rand.Read(id[:])
	name := hex.EncodeToString(id[:])
	return filepath.Join(name[:2], name[2:4], name+".enc")
}

// Write читает src до конца, шифрует данные и сохраняет их в новом файле.
// Возвращает относительный путь и параметры для регистрации блоба.
// Временный файл содержит шифртекст. Не закрывает src.
// При ошибке чтения возвращает domain.ErrIncomplete, сохраняя исходную причину.
func (s *BlobStore) Write(src io.Reader) (*WriteResult, error) {
	const op = "storage.Write"

	rel := newStoragePath()

	finalPath := s.blobPath(rel)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return nil, fmt.Errorf("%s: mkdir dir: %v", op, err)
	}

	dek, wrappedKey := s.keys.NewDEK()

	// Временный шифрованный файл создаём рядом с итоговым.
	tempFile, err := os.CreateTemp(filepath.Dir(finalPath), ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("%s: create temp: %v", op, err)
	}
	tempPath := tempFile.Name()
	removeTemp := func(cause error) error {
		if err := os.Remove(tempPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Join(cause, fmt.Errorf("%s: remove temp %q: %v", op, tempPath, err))
		}
		return cause
	}

	closeAndRemoveTemp := func(cause error) error {
		if err := tempFile.Close(); err != nil {
			cause = errors.Join(cause, fmt.Errorf("%s: close temp %q: %v", op, tempPath, err))
		}
		return removeTemp(cause)
	}

	encryptedSize, err := encryptTo(tempFile, src, dek, domain.DefaultChunkSize)
	if err != nil {
		return nil, closeAndRemoveTemp(err)
	}

	// Синхронизируем содержимое временного файла перед переименованием.
	if err := tempFile.Sync(); err != nil {
		return nil, closeAndRemoveTemp(fmt.Errorf("%s: sync temp %q: %v", op, tempPath, err))
	}
	if err := tempFile.Close(); err != nil {
		return nil, removeTemp(fmt.Errorf("%s: close temp %q: %v", op, tempPath, err))
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return nil, removeTemp(fmt.Errorf("%s: rename temp %q to %q: %v", op, tempPath, finalPath, err))
	}

	return &WriteResult{
		SizeEncrypted: encryptedSize,
		WrappedKey:    wrappedKey,
		MasterKeyID:   s.keys.CurrentID(),
		ChunkSize:     domain.DefaultChunkSize,
		StoragePath:   rel,
	}, nil
}

// OpenRange открывает диапазон расшифрованного содержимого блоба.
// Диапазон определяется по правилам domain.Blob.ResolveRange.
// До успешного возврата проверяет первый используемый чанк;
// ошибки последующих чанков могут возникнуть во время чтения.
// Вызывающий код должен закрыть возвращённый поток, в том числе после ошибки чтения.
// Если файл отсутствует, возвращает domain.ErrBlobFileMissing.
func (s *BlobStore) OpenRange(b *domain.Blob, offset, length int64) (io.ReadCloser, domain.ByteRange, error) {
	const op = "storage.OpenRange"

	rng, err := b.ResolveRange(offset, length)
	if err != nil {
		return nil, domain.ByteRange{}, err
	}
	dek, err := s.keys.Unwrap(b.EncKeyWrapped)
	if err != nil {
		return nil, domain.ByteRange{}, err
	}
	file, err := os.Open(s.blobPath(b.StoragePath))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, domain.ByteRange{}, fmt.Errorf("%w: %s",
				domain.ErrBlobFileMissing, b.StoragePath)
		}
		return nil, domain.ByteRange{}, fmt.Errorf("%s: open %s: %v",
			op, b.StoragePath, err)
	}
	r, err := newBlobReader(file, dek, b, b.Plan(rng))
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("%s: close after reader initialization failure: %v", op, closeErr))
		}
		return nil, domain.ByteRange{}, err
	}
	return r, rng, nil
}

// Open открывает всё расшифрованное содержимое блоба.
// Вызывающий код должен закрыть возвращённый поток.
func (s *BlobStore) Open(b *domain.Blob) (io.ReadCloser, error) {
	r, _, err := s.OpenRange(b, 0, -1)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Remove удаляет файл по пути rel относительно корня хранилища.
// Отсутствие файла не считается ошибкой.
func (s *BlobStore) Remove(rel string) error {
	const op = "storage.Remove"

	err := os.Remove(s.blobPath(rel))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %v", op, err)
	}
	return nil
}
