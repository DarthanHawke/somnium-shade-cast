package storage

import (
	"crypto/cipher"
	"fmt"
	"io"
	"os"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// blobReader —- io.ReadCloser, отдающий plaintext-диапазон по ChunkPlan.
type blobReader struct {
	f    *os.File
	aead cipher.AEAD
	blob *domain.Blob
	plan domain.ChunkPlan

	cur       int64  // индекс текущего чанка
	buf       []byte // расшифрованный остаток текущего чанка
	remaining int64  // сколько plaintext-байт ещё отдать
	encBuf    []byte // буфер
}

func newBlobReader(f *os.File, dek []byte, b *domain.Blob, plan domain.ChunkPlan) (*blobReader, error) {
	aead, err := dekAEAD(dek)
	if err != nil {
		return nil, err
	}
	r := &blobReader{
		f: f, aead: aead, blob: b, plan: plan,
		cur:       plan.FirstChunk,
		remaining: plan.Length,
		encBuf:    make([]byte, b.ChunkSize+domain.GCMOverhead),
	}
	// первый чанк: расшифровать и отбросить SkipInFirst
	if err := r.loadChunk(); err != nil {
		f.Close()
		return nil, err
	}
	r.buf = r.buf[plan.SkipInFirst:]
	return r, nil
}

// loadChunk читает и расшифровывает чанк r.cur в r.buf.
func (r *blobReader) loadChunk() error {
	const op = "reader.GetBySHA256"

	// размер чанка на диске: nonce + plaintextLen + tag
	plainLen := r.blob.PlainChunkLen(r.cur)
	diskLen := domain.GCMNonceSize + plainLen + domain.GCMTagSize
	off := r.blob.ChunkOffsetOnDisk(r.cur)

	enc := r.encBuf[:diskLen]
	if _, err := r.f.ReadAt(enc, off); err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}

	nonce := enc[:domain.GCMNonceSize]
	// защита от перестановки: nonce обязан совпасть с ожидаемым
	expected := chunkNonce(r.cur)
	for i := range nonce {
		if nonce[i] != expected[i] {
			return &domain.WrappedError{
				Op:  op,
				Err: fmt.Errorf("storage: chunk %d: nonce mismatch (file tampered?)", r.cur),
			}
		}
	}

	plain, err := r.aead.Open(nil, nonce, enc[domain.GCMNonceSize:], nonce)
	if err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	r.buf = plain
	return nil
}

func (r *blobReader) Read(p []byte) (int, error) {
	const op = "reader.Read"

	if r.remaining <= 0 {
		return 0, io.EOF
	}
	// буфер пуст - следующий чанк
	if len(r.buf) == 0 {
		r.cur++
		if r.cur > r.plan.LastChunk {
			return 0, io.EOF // не должно случаться при корректном plan
		}
		if err := r.loadChunk(); err != nil {
			return 0, &domain.WrappedError{
				Op:  op,
				Err: err,
			}
		}
	}
	n := copy(p, r.buf)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	r.buf = r.buf[n:]
	r.remaining -= int64(n)
	return n, nil
}

func (r *blobReader) Close() error {
	return r.f.Close()
}
