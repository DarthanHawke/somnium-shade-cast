package storage

import (
	"crypto/cipher"
	"fmt"
	"io"
	"os"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// blobReader последовательно расшифровывает диапазон, заданный планом чтения чанков.
// После ошибки чтения вызывающий код должен закрыть reader.
type blobReader struct {
	file *os.File         // file файл, содержащий зашифрованные чанки
	aead cipher.AEAD      // aead на файловом ключе
	blob *domain.Blob     // blob содержит параметры шифрования и смещения чанков
	plan domain.ChunkPlan // plan план чтения чанков, включая смещение в первом чанке

	chunkIndex      int64  // chunkIndex текущий индекс чанка, который нужно расшифровать
	plainBuffer     []byte // plainBuffer буфер для чтения расшифрованного чанка
	remaining       int64  // remaining количество байт, оставшихся для чтения в диапазоне
	encryptedBuffer []byte // encryptedBuffer зашифрованный буфер для чтения чанка с диска
}

// newBlobReader создаёт reader и проверяет первый чанк.
// При ошибке файл остаётся во владении вызывающего кода.
// При успехе файл закрывается через Close созданного reader.
func newBlobReader(file *os.File, dek []byte, b *domain.Blob, plan domain.ChunkPlan) (*blobReader, error) {
	aead, err := dekAEAD(dek)
	if err != nil {
		return nil, err
	}
	r := &blobReader{
		file: file, aead: aead, blob: b, plan: plan,
		chunkIndex:      plan.FirstChunk,
		remaining:       plan.Length,
		encryptedBuffer: make([]byte, b.ChunkSize+domain.GCMOverhead),
	}
	// первый чанк: расшифровать и отбросить SkipInFirst
	if err := r.loadChunk(); err != nil {
		return nil, err
	}
	r.plainBuffer = r.plainBuffer[plan.SkipInFirst:]
	return r, nil
}

// loadChunk читает, проверяет и расшифровывает текущий чанк в буфер reader.
func (r *blobReader) loadChunk() error {
	const op = "storage.loadChunk"

	// размер чанка на диске: nonce + plaintextLen + tag
	plainLen := r.blob.PlainChunkLen(r.chunkIndex)
	diskLen := domain.GCMNonceSize + plainLen + domain.GCMTagSize
	off := r.blob.ChunkOffsetOnDisk(r.chunkIndex)

	enc := r.encryptedBuffer[:diskLen]
	if _, err := r.file.ReadAt(enc, off); err != nil {
		return fmt.Errorf("%s: chunk %d: read: %v", op, r.chunkIndex, err)
	}

	nonce := enc[:domain.GCMNonceSize]
	// Ожидаемый nonce связывает чанк с его позицией в файле.
	expected := chunkNonce(r.chunkIndex)
	for i := range nonce {
		if nonce[i] != expected[i] {
			return fmt.Errorf("%w: %s: chunk %d: nonce mismatch", domain.ErrTampered, op, r.chunkIndex)
		}
	}

	plain, err := r.aead.Open(nil, nonce, enc[domain.GCMNonceSize:], nonce)
	if err != nil {
		return fmt.Errorf("%w: %s: chunk %d: %v", domain.ErrDecryptionFailed, op, r.chunkIndex, err)
	}
	r.plainBuffer = plain
	return nil
}

// Read читает расшифрованные данные в p.
// Возвращает io.EOF, когда достигнут конец диапазона.
// Вызовы Read на одном reader должны быть последовательными.
func (r *blobReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.plainBuffer) == 0 {
		if r.chunkIndex >= r.plan.LastChunk {
			return 0, io.ErrUnexpectedEOF
		}
		r.chunkIndex++
		if err := r.loadChunk(); err != nil {
			return 0, err
		}
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n := copy(p, r.plainBuffer)
	r.plainBuffer = r.plainBuffer[n:]
	r.remaining -= int64(n)
	return n, nil
}

// Close закрывает файл, принадлежащий reader.
func (r *blobReader) Close() error {
	if err := r.file.Close(); err != nil {
		return fmt.Errorf("error closing blob reader: %v", err)
	}
	return nil
}
