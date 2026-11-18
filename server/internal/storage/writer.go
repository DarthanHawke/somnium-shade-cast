package storage

import (
	"bufio"
	"fmt"
	"io"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// encryptTo читает src и записывает в dst зашифрованные чанки.
// При успехе возвращает размер шифртекста в байтах.
// Не закрывает src и dst.
func encryptTo(dst io.Writer, src io.Reader, dek []byte, chunkSize int) (int64, error) {
	const op = "storage.encryptTo"

	aead, err := dekAEAD(dek)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(dst, chunkSize+domain.GCMOverhead)
	br := bufio.NewReaderSize(src, chunkSize)
	var sealed []byte
	var encryptedSize, idx int64

	for {
		chunk, readErr := br.Peek(chunkSize)
		if readErr != nil && readErr != io.EOF {
			return encryptedSize, fmt.Errorf(
				"%s: read source: %w: %w", op, domain.ErrIncomplete, readErr,
			)
		}

		if len(chunk) > 0 {
			nonce := chunkNonce(idx)
			sealed = aead.Seal(sealed[:0], nonce, chunk, nonce)
			if _, err := bw.Write(nonce); err != nil {
				return encryptedSize, fmt.Errorf("%s: write nonce: %v", op, err)
			}
			if _, err := bw.Write(sealed); err != nil {
				return encryptedSize, fmt.Errorf("%s: write sealed: %v", op, err)
			}

			encryptedSize += int64(len(nonce) + len(sealed))
			idx++
			_, _ = br.Discard(len(chunk))
		}

		if readErr == io.EOF {
			break
		}
	}
	if err := bw.Flush(); err != nil {
		return encryptedSize, fmt.Errorf("%s: flush: %v", op, err)
	}
	return encryptedSize, nil
}
