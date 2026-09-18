package domain

type BlobKind string

const (
	BlobAudio BlobKind = "audio"
	BlobImage BlobKind = "image"
	BlobOther BlobKind = "other"
)

type Blob struct {
	ID            ID
	SHA256Plain   string
	SizePlain     int64
	SizeEncrypted int64
	MimeType      string
	Kind          BlobKind
	EncAlgo       string
	EncKeyWrapped []byte
	MasterKeyID   string
	ChunkSize     int
	StoragePath   string
	RefCount      int
	CreatedAt     UnixTimeMillis
}
