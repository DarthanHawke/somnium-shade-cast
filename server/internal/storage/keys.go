package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// KeyManager владеет мастер-ключом и умеет оборачивать/разворачивать файловые ключи
type KeyManager struct {
	keyID string
	aead  cipher.AEAD // AES-256-GCM на мастер-ключе
}

// NewKeyManager принимает 32-байтовый мастер-ключ. Сюды приходит готовым.
func NewKeyManager(keyID string, masterKey []byte) (*KeyManager, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("keys: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keys: gcm: %w", err)
	}
	return &KeyManager{
		keyID: keyID,
		aead:  aead,
	}, nil
}

func (k *KeyManager) CurrentID() string {
	return k.keyID
}

// NewDEK генерирует свежий файловый ключ и сразу его обёртку для БД
func (k *KeyManager) NewDEK() (dek []byte, wrapped []byte, err error) {
	const op = "keys.NewDEK"

	dek = make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	wrapped, err = k.Wrap(dek)
	return dek, wrapped, err
}

// Wrap = [nonce 12Б][ciphertext+tag]
func (k *KeyManager) Wrap(plain []byte) ([]byte, error) {
	const op = "keys.Wrap"

	nonce := make([]byte, domain.GCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return k.aead.Seal(nonce, nonce, plain, nil), nil
}

func (k *KeyManager) Unwrap(wrapped []byte) ([]byte, error) {
	const op = "keys.Unwrap"

	if len(wrapped) < domain.GCMNonceSize+domain.GCMTagSize {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: fmt.Errorf("keys: wrapped key too short"),
		}
	}
	nonce, ct := wrapped[:domain.GCMNonceSize], wrapped[domain.GCMNonceSize:]
	plain, err := k.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return plain, nil
}

// chunkNonce - nonce чанка: 4 нулевых байта + big-endian номер чанка
func chunkNonce(idx int64) []byte {
	n := make([]byte, domain.GCMNonceSize)
	binary.BigEndian.PutUint64(n[4:], uint64(idx))
	return n
}

// dekAEAD - AEAD на файловом ключе.
func dekAEAD(dek []byte) (cipher.AEAD, error) {
	const op = "keys.dekAEAD"

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return cipher.NewGCM(block)
}

// ReadMasterKey - хелпер для bootstrap: читает ровно 32 байта.
func ReadMasterKey(r io.Reader) ([]byte, error) {
	const op = "keys.ReadMasterKey"

	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return key, nil
}
