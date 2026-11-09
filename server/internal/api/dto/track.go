package dto

import (
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/library"
)

// ArtistSummary содержит краткую информацию об исполнителе.
type ArtistSummary struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
}

// AlbumSummary содержит краткую информацию об альбоме.
type AlbumSummary struct {
	PublicID string `json:"public_id"`
	Title    string `json:"title"`
}

type TrackResponse struct {
	PublicID string        `json:"public_id"`
	Artist   ArtistSummary `json:"artist"`
	// Album содержит сведения об альбоме. Если трек не связан с альбомом,
	// поле равно nil и передаётся как null.
	Album   *AlbumSummary `json:"album"`
	Title   string        `json:"title"`
	TrackNo *int          `json:"track_no"`
	DiscNo  int           `json:"disc_no"`
	// DurationMS содержит длительность трека в миллисекундах.
	// Если длительность неизвестна, поле передаётся как null.
	DurationMS  *int64 `json:"duration_ms"`
	Status      string `json:"status"`
	Codec       string `json:"codec"`
	BitrateKbps *int   `json:"bitrate_kbps"`
	SampleRate  *int   `json:"sample_rate"`
	LastfmURL   string `json:"lastfm_url"`
	// CoverURL содержит путь к обложке альбома.
	// Если обложка не задана, поле передаётся как null.
	CoverURL *string `json:"cover_url"`
	// CreatedAt содержит время создания в миллисекундах с начала эпохи Unix.
	CreatedAt int64 `json:"created_at"`
	// UpdatedAt содержит время последнего обновления в миллисекундах с начала эпохи Unix.
	UpdatedAt int64 `json:"updated_at"`
}

// NewUploadedTrackResponse — создает TrackResponse из ingest.UploadResult.
func NewUploadedTrackResponse(result *ingest.UploadResult) TrackResponse {
	track := result.Track

	response := TrackResponse{
		PublicID:    string(track.PublicID),
		Title:       track.Title,
		TrackNo:     track.TrackNo,
		DiscNo:      track.DiscNo,
		DurationMS:  track.DurationMS,
		Status:      string(track.Status),
		Codec:       track.Codec,
		BitrateKbps: track.BitrateKbps,
		SampleRate:  track.SampleRate,
		LastfmURL:   track.LastfmURL,
		CreatedAt:   track.CreatedAt.UnixMilli(),
		UpdatedAt:   track.UpdatedAt.UnixMilli(),
		Artist: ArtistSummary{
			PublicID: string(result.Artist.PublicID),
			Name:     result.Artist.Name,
		},
	}

	if album := result.Album; album != nil {
		response.Album = &AlbumSummary{
			PublicID: string(album.PublicID),
			Title:    album.Title,
		}

		if album.CoverBlobID != nil {
			url := "/lib/tracks/" + string(track.PublicID) + "/cover"
			response.CoverURL = &url
		}
	}

	return response
}

// NewTrackResponse — создает TrackResponse из library.TrackView.
func NewTrackResponse(view *library.TrackView) TrackResponse {
	response := TrackResponse{
		PublicID:    string(view.PublicID),
		Title:       view.Title,
		TrackNo:     view.TrackNo,
		DiscNo:      view.DiscNo,
		DurationMS:  view.DurationMS,
		Status:      string(view.Status),
		Codec:       view.Codec,
		BitrateKbps: view.BitrateKbps,
		SampleRate:  view.SampleRate,
		LastfmURL:   view.LastfmURL,
		CreatedAt:   view.CreatedAt.UnixMilli(),
		UpdatedAt:   view.UpdatedAt.UnixMilli(),
		Artist: ArtistSummary{
			PublicID: string(view.ArtistPublicID),
			Name:     view.ArtistName,
		},
	}

	if view.AlbumPublicID != nil && view.AlbumTitle != nil {
		response.Album = &AlbumSummary{
			PublicID: string(*view.AlbumPublicID),
			Title:    *view.AlbumTitle,
		}
	}

	if view.CoverBlobID != nil {
		url := "/lib/tracks/" + string(view.PublicID) + "/cover"
		response.CoverURL = &url
	}

	return response
}
