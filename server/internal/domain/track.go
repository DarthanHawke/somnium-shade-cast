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
	ID         ID       `db:"id"`
	PublicID   PublicID `db:"public_id"`
	OwnerID    ID       `db:"owner_user_id"`
	AlbumID    *ID      `db:"album_id"`
	ArtistID   ID       `db:"artist_id"`
	Title      string   `db:"title"`
	NormTitle  string   `db:"norm_title"`
	TrackNo    *int     `db:"track_no"`
	DiscNo     int      `db:"disc_no"`
	DurationMS *int64   `db:"duration_ms"` // nil означает, что длительность неизвестна

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

// NewTrackParams содержит исходные данные для создания трека.
type NewTrackParams struct {
	OwnerID          ID
	ArtistID         ID
	AlbumID          *ID
	Title            string
	TrackNo          *int
	DiscNo           int
	DurationMS       *int64
	Status           TrackStatus
	BlobID           *ID
	Fingerprint      string
	FPDuration       int
	IsIntentionalDup bool
	OriginalFilename string
	Codec            string
	BitrateKbps      *int
	SampleRate       *int
}

// NewTrack создаёт трек и проверяет соответствие статуса привязке блоба.
// Пустой статус означает TrackGhost. Неположительный номер диска заменяется на 1.
func NewTrack(params NewTrackParams) (*Track, error) {
	if params.Status == "" {
		params.Status = TrackGhost
	}
	switch params.Status {
	case TrackUploaded:
		if params.BlobID == nil {
			return nil, Invalid("blob_id", "required for uploaded track")
		}
	case TrackGhost:
		if params.BlobID != nil {
			return nil, Invalid("blob_id", "must be empty for ghost track")
		}
	default:
		return nil, Invalid("status", "unknown track status")
	}

	if params.DiscNo <= 0 {
		params.DiscNo = 1
	}

	now := UnixTimeMillis(time.Now())
	return &Track{
		PublicID:         NewPublicID(),
		OwnerID:          params.OwnerID,
		ArtistID:         params.ArtistID,
		AlbumID:          params.AlbumID,
		Title:            params.Title,
		NormTitle:        Normalize(params.Title),
		TrackNo:          params.TrackNo,
		DiscNo:           params.DiscNo,
		DurationMS:       params.DurationMS,
		Status:           params.Status,
		BlobID:           params.BlobID,
		Fingerprint:      params.Fingerprint,
		FPDuration:       params.FPDuration,
		IsIntentionalDup: params.IsIntentionalDup,
		OriginalFilename: params.OriginalFilename,
		Codec:            params.Codec,
		BitrateKbps:      params.BitrateKbps,
		SampleRate:       params.SampleRate,
		Enrich:           EnrichPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}
