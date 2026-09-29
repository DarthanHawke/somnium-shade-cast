package storage

import (
	"bufio"
	"io"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// encryptTo читает plaintext из src,
// пишет шифрованные чанки в dst. Возвращает размер записанного шифртекста.
func encryptTo(dst io.Writer, src io.Reader, dek []byte, chunkSize int) (int64, error) {
	const op = "writer.encryptTo"

	aead, err := dekAEAD(dek)
	if err != nil {
		return 0, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	bw := bufio.NewWriterSize(dst, chunkSize+domain.GCMOverhead)
	buf := make([]byte, chunkSize)
	var sealed []byte
	var encSize, idx int64

	for {
		n, rerr := io.ReadFull(src, buf)
		if n > 0 {
			nonce := chunkNonce(idx)
			sealed = aead.Seal(sealed[:0], nonce, buf[:n], nonce)
			bw.Write(nonce)
			bw.Write(sealed)
			encSize += int64(len(nonce) + len(sealed))
			idx++
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return encSize, &domain.WrappedError{
				Op:  op,
				Err: rerr,
			}
		}
	}
	if err := bw.Flush(); err != nil {
		return encSize, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return encSize, nil
}
