package domain

// TrackUpload хранит параметры загрузки, его связь с очередью и публичный результат.
type TrackUpload struct {
	PublicID         PublicID        `db:"public_id"`
	OwnerID          ID              `db:"owner_user_id"`
	IdempotencyKey   string          `db:"idempotency_key"`
	RequestHash      string          `db:"request_hash"`
	BlobID           *ID             `db:"blob_id"`
	JobID            *ID             `db:"job_id"`
	OriginalFilename string          `db:"original_filename"`
	ForceDup         bool            `db:"force_dup"`
	GhostID          *PublicID       `db:"ghost_public_id"`
	Fingerprint      string          `db:"fingerprint"`
	FPDuration       int             `db:"fp_duration"`
	TrackID          *PublicID       `db:"track_public_id"`
	ErrorCode        *string         `db:"error_code"`
	CreatedAt        UnixTimeMillis  `db:"created_at"`
	CompletedAt      *UnixTimeMillis `db:"completed_at"`
	Status           UploadStatus    `db:"status"`
}

// UploadStatus описывает видимое клиенту состояние загрузки.
type UploadStatus string

const (
	UploadQueued     UploadStatus = "queued"
	UploadProcessing UploadStatus = "processing"
	UploadReady      UploadStatus = "ready"
	UploadFailed     UploadStatus = "failed"
)
