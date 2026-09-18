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
	ID        ID
	PublicID  PublicID
	OwnerID   ID
	AlbumID   *ID
	ArtistID  ID
	Title     string
	NormTitle string
	TrackNo   *int
	DiscNo    int
	Duration  time.Duration // 0 у ghost допустимо

	Status TrackStatus
	BlobID *ID

	Fingerprint      string
	FPDuration       int
	IsIntentionalDup bool

	OriginalFilename string
	Codec            string
	BitrateKbps      *int
	SampleRate       *int

	MBID           string
	LastfmURL      string
	Enrich         EnrichStatus
	EnrichedAt     *time.Time
	ManualOverride bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DupVerdict — результат сравнения при сканировании
type DupVerdict string

const (
	DupNone  DupVerdict = "new"
	DupExact DupVerdict = "exact_dup" // sha256 совпал
	DupNear  DupVerdict = "near_dup"  // fingerprint совпал, файл другой
)
