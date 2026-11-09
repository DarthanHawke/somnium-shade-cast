// Package library содержит модели чтения музыкальной библиотеки.
package library

import "github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"

// TrackView содержит данные трека, исполнителя и необязательного альбома
// для чтения библиотеки.
type TrackView struct {
	PublicID   domain.PublicID    `db:"public_id"`
	Title      string             `db:"title"`
	DurationMS *int64             `db:"duration_ms"`
	Status     domain.TrackStatus `db:"status"`

	TrackNo     *int                  `db:"track_no"`
	DiscNo      int                   `db:"disc_no"`
	Codec       string                `db:"codec"`
	BitrateKbps *int                  `db:"bitrate_kbps"`
	SampleRate  *int                  `db:"sample_rate"`
	LastfmURL   string                `db:"lastfm_url"`
	CreatedAt   domain.UnixTimeMillis `db:"created_at"`
	UpdatedAt   domain.UnixTimeMillis `db:"updated_at"`

	ArtistPublicID domain.PublicID `db:"artist_public_id"`
	ArtistName     string          `db:"artist_name"`

	// Поля альбома равны nil, если трек с ним не связан.
	AlbumPublicID *domain.PublicID `db:"album_public_id"`
	AlbumTitle    *string          `db:"album_title"`

	CoverBlobID *domain.ID `db:"cover_blob_id"` // Внутренний идентификатор обложки альбома, если она есть.
}
