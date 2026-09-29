package domain

import (
	"time"
)

type TrackStatus string

const (
	TrackUploaded TrackStatus = "uploaded"
	TrackGhost    TrackStatus = "ghost" // виден в треклисте альбома, файла нет
)

type Track struct {
	ID        ID            `db:"id"`
	PublicID  PublicID      `db:"public_id"`
	OwnerID   ID            `db:"owner_user_id"`
	AlbumID   *ID           `db:"album_id"`
	ArtistID  ID            `db:"artist_id"`
	Title     string        `db:"title"`
	NormTitle string        `db:"norm_title"`
	TrackNo   *int          `db:"track_no"`
	DiscNo    int           `db:"disc_no"`
	Duration  time.Duration `db:"duration_ms"` // 0 у ghost допустимо

	Status TrackStatus `db:"status"`
	BlobID *ID         `db:"blob_id"`

	Fingerprint      string `db:"fingerprint"`
	FPDuration       int    `db:"fp_duration"`
	IsIntentionalDup bool   `db:"is_intentional_dup"`

	OriginalFilename string `db:"original_filename"`
	Codec            string `db:"codec"`
	BitrateKbps      *int   `db:"bitrate_kbps"`
	SampleRate       *int   `db:"sample_rate"`

	MBID           string          `db:"mbid"`
	LastfmURL      string          `db:"lastfm_url"`
	Enrich         EnrichStatus    `db:"enrich_status"`
	EnrichedAt     *UnixTimeMillis `db:"enriched_at"`
	ManualOverride bool            `db:"manual_override"`

	CreatedAt UnixTimeMillis `db:"created_at"`
	UpdatedAt UnixTimeMillis `db:"updated_at"`
}

func NewTrack(ownerID, artistID ID, albumID *ID, title string,
	trackNo *int, discNo int, duration time.Duration, status TrackStatus,
	blobID *ID, fingerprint string, fPDuration int, isIntentionalDup bool,
	originalFilename string, codec string, bitrateKbps *int, sampleRate *int,
	mBID string, lastfmURL string) (*Track, error) {
	if discNo <= 0 {
		discNo = 1
	}
	if status == "" {
		// Дефолт - ghost без файла. Upload выставит uploaded явно.
		status = TrackGhost
	}

	now := UnixTimeMillis(time.Now())

	t := &Track{
		PublicID:         NewPublicID(),
		OwnerID:          ownerID,
		ArtistID:         artistID,
		AlbumID:          albumID,
		Title:            title,
		NormTitle:        Normalize(title),
		TrackNo:          trackNo,
		DiscNo:           discNo,
		Duration:         duration,
		Status:           status,
		BlobID:           blobID,
		Fingerprint:      fingerprint,
		FPDuration:       fPDuration,
		IsIntentionalDup: isIntentionalDup,
		OriginalFilename: originalFilename,
		Codec:            codec,
		BitrateKbps:      bitrateKbps,
		SampleRate:       sampleRate,
		MBID:             mBID,
		LastfmURL:        lastfmURL,
		Enrich:           EnrichPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return t, nil
}

// DupVerdict — результат сравнения при сканировании
type DupVerdict string

const (
	DupNone  DupVerdict = "new"
	DupExact DupVerdict = "exact_dup" // sha256 совпал
	DupNear  DupVerdict = "near_dup"  // fingerprint совпал, файл другой
)
