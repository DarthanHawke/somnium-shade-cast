package domain

import (
	"time"
)

type EnrichStatus string

const (
	EnrichPending EnrichStatus = "pending"
	EnrichDone    EnrichStatus = "done"
	EnrichFailed  EnrichStatus = "failed"
	EnrichManual  EnrichStatus = "manual"
)

type Artist struct {
	ID          ID              `db:"id"`
	PublicID    PublicID        `db:"public_id"`
	Name        string          `db:"name"`
	SortName    string          `db:"sort_name"`
	NormName    string          `db:"norm_name"`
	MBID        string          `db:"mbid"`
	LastfmURL   string          `db:"lastfm_url"`
	ImageBlobID *ID             `db:"image_blob_id"`
	Bio         string          `db:"bio"`
	Enrich      EnrichStatus    `db:"enrich_status"`
	EnrichedAt  *UnixTimeMillis `db:"enriched_at"`
	CreatedAt   UnixTimeMillis  `db:"created_at"`
	UpdatedAt   UnixTimeMillis  `db:"updated_at"`
}

func NewArtist(name string) (*Artist, error) {
	if name == "" {
		return nil, Invalid("name", "required")
	}
	timeNow := UnixTimeMillis(time.Now())

	return &Artist{
		PublicID:  NewPublicID(),
		Name:      name,
		NormName:  Normalize(name),
		SortName:  NormalizeSort(name),
		Enrich:    EnrichPending,
		CreatedAt: timeNow,
		UpdatedAt: timeNow,
	}, nil
}

type Album struct {
	ID          ID              `db:"id"`
	PublicID    PublicID        `db:"public_id"`
	ArtistID    ID              `db:"artist_id"`
	Title       string          `db:"title"`
	NormTitle   string          `db:"norm_title"`
	Year        *int            `db:"year"`
	ReleaseDate *UnixTimeMillis `db:"release_date"`
	MBID        string          `db:"mbid"`
	LastfmURL   string          `db:"lastfm_url"`
	CoverBlobID *ID             `db:"cover_blob_id"`
	TotalTracks *int            `db:"total_tracks"`
	IsComplete  bool            `db:"is_complete"`
	Enrich      EnrichStatus    `db:"enrich_status"`
	EnrichedAt  *UnixTimeMillis `db:"enriched_at"`
	CreatedAt   UnixTimeMillis  `db:"created_at"`
	UpdatedAt   UnixTimeMillis  `db:"updated_at"`
}

func NewAlbum(artistID ID, title string, year *int, coverBlobID *ID) (*Album, error) {
	if title == "" {
		return nil, Invalid("title", "required")
	}
	timeNow := UnixTimeMillis(time.Now())

	return &Album{
		PublicID:    NewPublicID(),
		ArtistID:    artistID,
		Title:       title,
		NormTitle:   Normalize(title),
		Year:        year,
		CoverBlobID: coverBlobID,
		Enrich:      EnrichPending,
		CreatedAt:   timeNow,
		UpdatedAt:   timeNow,
	}, nil
}
