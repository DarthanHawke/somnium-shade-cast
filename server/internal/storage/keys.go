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

// KeyManager шифрует и расшифровывает файловые ключи с помощью одного мастер-ключа.
type KeyManager struct {
	keyID string
	aead  cipher.AEAD // AES-256-GCM на мастер-ключе
}

// NewKeyManager создаёт менеджер для 32-байтового мастер-ключа с идентификатором keyID.
func NewKeyManager(keyID string, masterKey []byte) (*KeyManager, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("keys: cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keys: gcm: %v", err)
	}
	return &KeyManager{
		keyID: keyID,
		aead:  aead,
	}, nil
}

// CurrentID возвращает идентификатор мастер-ключа этого менеджера.
func (k *KeyManager) CurrentID() string {
	return k.keyID
}

// NewDEK создаёт новый 32-байтовый файловый ключ и его зашифрованную обёртку.
func (k *KeyManager) NewDEK() (dek []byte, wrappedKey []byte) {
	dek = make([]byte, 32)
	rand.Read(dek)
	wrappedKey = k.Wrap(dek)

	return dek, wrappedKey
}

// Wrap шифрует plain мастер-ключом с новым случайным nonce.
// Результат содержит nonce, шифртекст и тег проверки подлинности.
func (k *KeyManager) Wrap(plain []byte) []byte {
	nonce := make([]byte, domain.GCMNonceSize)
	rand.Read(nonce)
	return k.aead.Seal(nonce, nonce, plain, nil)
}

// Unwrap расшифровывает обёртку, созданную Wrap.
// Возвращает domain.ErrInvalidWrappedKey для слишком короткой обёртки
// и domain.ErrDecryptionFailed при ошибке проверки подлинности.
func (k *KeyManager) Unwrap(wrappedKey []byte) ([]byte, error) {
	const op = "storage.Unwrap"

	if len(wrappedKey) < domain.GCMNonceSize+domain.GCMTagSize {
		return nil, fmt.Errorf("%w: %s: too short (%d bytes, need %d)",
			domain.ErrInvalidWrappedKey, op, len(wrappedKey),
			domain.GCMNonceSize+domain.GCMTagSize)
	}
	nonce, ct := wrappedKey[:domain.GCMNonceSize], wrappedKey[domain.GCMNonceSize:]
	plain, err := k.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", domain.ErrDecryptionFailed, op, err)
	}
	return plain, nil
}

// chunkNonce формирует nonce из четырёх нулевых байтов
// и восьмибайтового индекса чанка в порядке big-endian.
// idx должен быть неотрицательным.
func chunkNonce(idx int64) []byte {
	n := make([]byte, domain.GCMNonceSize)
	binary.BigEndian.PutUint64(n[4:], uint64(idx))
	return n
}

// dekAEAD создаёт AES-GCM для файлового ключа dek.
func dekAEAD(dek []byte) (cipher.AEAD, error) {
	const op = "storage.dekAEAD"

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}
	return aead, nil
}

// ReadMasterKey читает следующие 32 байта из r как мастер-ключ.
// При ошибке чтения возвращает domain.ErrInvalidMasterKey, сохраняя исходную причину.
// Оставляет последующие байты непрочитанными и не закрывает r.
func ReadMasterKey(r io.Reader) ([]byte, error) {
	const op = "storage.ReadMasterKey"

	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", domain.ErrInvalidMasterKey, op, err)
	}
	return key, nil
}
