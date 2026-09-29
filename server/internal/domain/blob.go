package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

type BlobKind string

const (
	BlobAudio BlobKind = "audio"
	BlobImage BlobKind = "image"
	BlobOther BlobKind = "other"
)

func (k BlobKind) Valid() bool {
	switch k {
	case BlobAudio, BlobImage, BlobOther:
		return true
	}
	return false
}

const (
	DefaultChunkSize = 128 * 1024 // 128 КБ

	// GCMOverhead - расходы AES-GCM на один чанк:
	// 12 байт nonce + 16 байт auth tag.
	GCMNonceSize = 12
	GCMTagSize   = 16
	GCMOverhead  = GCMNonceSize + GCMTagSize

	EncAlgoAESGCM = "aes-256-gcm"
)

type Blob struct {
	ID            ID             `db:"id"`
	SHA256Plain   string         `db:"sha256_plain"`
	SizePlain     int64          `db:"size_plain"`
	SizeEncrypted int64          `db:"size_encrypted"`
	MimeType      string         `db:"mime_type"`
	Kind          BlobKind       `db:"kind"`
	EncAlgo       string         `db:"enc_algo"`
	EncKeyWrapped []byte         `db:"enc_key_wrapped"`
	MasterKeyID   string         `db:"master_key_id"`
	ChunkSize     int            `db:"chunk_size"`
	StoragePath   string         `db:"storage_path"`
	RefCount      int            `db:"ref_count"`
	CreatedAt     UnixTimeMillis `db:"created_at"`
}

// NewBlob - конструктор
func NewBlob(shaPlain string, sizePlain, sizeEnc int64, mime string, kind BlobKind,
	wrappedKey []byte, masterKeyID string, chunkSize int) (*Blob, error) {

	if len(shaPlain) != 64 { // hex sha256
		return nil, Invalid("sha256_plain", "must be 64 hex chars")
	}
	if sizePlain <= 0 {
		return nil, Invalid("size_plain", "must be positive")
	}
	if !kind.Valid() {
		return nil, Invalid("kind", "unknown blob kind")
	}
	if len(wrappedKey) == 0 {
		return nil, Invalid("enc_key_wrapped", "required")
	}
	if masterKeyID == "" {
		return nil, Invalid("master_key_id", "required")
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	return &Blob{
		SHA256Plain:   shaPlain,
		SizePlain:     sizePlain,
		SizeEncrypted: sizeEnc,
		MimeType:      mime,
		Kind:          kind,
		EncAlgo:       EncAlgoAESGCM,
		EncKeyWrapped: wrappedKey,
		MasterKeyID:   masterKeyID,
		ChunkSize:     chunkSize,
		StoragePath:   StoragePathFor(shaPlain),
		RefCount:      0,
		CreatedAt:     UnixTimeMillis(time.Now()),
	}, nil
}

// StoragePathFor - относительный путь из хэша
func StoragePathFor(shaPlain string) string {
	return filepath.Join(shaPlain[0:2], shaPlain[2:4], shaPlain+".enc")
}

// NumChunks - количество чанков в файле
func (b *Blob) NumChunks() int64 {
	cs := int64(b.ChunkSize)
	return (b.SizePlain + cs - 1) / cs
}

// encChunkSize - размер полного чанка на диске
func (b *Blob) encChunkSize() int64 {
	return int64(b.ChunkSize) + GCMOverhead
}

// PlainChunkLen - сколько plaintext-байт в последнем чанке idx
func (b *Blob) PlainChunkLen(idx int64) int64 {
	cs := int64(b.ChunkSize)
	if idx < b.NumChunks()-1 {
		return cs
	}
	rem := b.SizePlain % cs
	if rem == 0 {
		return cs
	}
	return rem
}

// ChunkOffsetOnDisk - байтовое смещение начала чанка в зашифрованном файле
func (b *Blob) ChunkOffsetOnDisk(idx int64) int64 {
	return idx * b.encChunkSize()
}

// ByteRange описывает валидированный диапазон исходника
type ByteRange struct {
	Offset int64 // от начала plaintext
	Length int64 // > 0
}

// ResolveRange валидирует HTTP Range
func (b *Blob) ResolveRange(offset, length int64) (ByteRange, error) {
	if offset < 0 || offset >= b.SizePlain {
		return ByteRange{}, fmt.Errorf("range start %d out of [0,%d): %w",
			offset, b.SizePlain, ErrInvalidInput)
	}
	if length <= 0 || offset+length > b.SizePlain {
		length = b.SizePlain - offset
	}
	return ByteRange{Offset: offset, Length: length}, nil
}

// ChunkPlan - что нужно прочитать с диска, чтобы отдать нужный кусок
type ChunkPlan struct {
	FirstChunk  int64 // индекс первого чанка
	LastChunk   int64 // индекс последнего
	SkipInFirst int64 // сколько plaintext-байт отбросить в первом чанке
	Length      int64 // сколько plaintext-байт отдать всего
}

// Plan переводит диапазон исходника в план чтения чанков
func (b *Blob) Plan(r ByteRange) ChunkPlan {
	cs := int64(b.ChunkSize)
	return ChunkPlan{
		FirstChunk:  r.Offset / cs,
		LastChunk:   (r.Offset + r.Length - 1) / cs,
		SkipInFirst: r.Offset % cs,
		Length:      r.Length,
	}
}

// HashReader - обёртка для подсчёта sha256 и размера на лету при приёме upload
type HashReader struct {
	r    io.Reader
	h    interface{ Sum([]byte) []byte }
	hw   io.Writer
	size int64
}

func NewHashReader(r io.Reader) *HashReader {
	h := sha256.New()
	return &HashReader{r: r, h: h, hw: h}
}

func (hr *HashReader) Read(p []byte) (int, error) {
	n, err := hr.r.Read(p)
	if n > 0 {
		hr.hw.Write(p[:n])
		hr.size += int64(n)
	}
	return n, err
}

func (hr *HashReader) SumHex() string { return hex.EncodeToString(hr.h.Sum(nil)) }
func (hr *HashReader) Size() int64    { return hr.size }
