package domain

// ExtractedMetadata - метаданные, извлечённые из файла и/или заданные извне
type ExtractedMetadata struct {
	// Из тегов
	Title       string
	ArtistName  string
	AlbumTitle  string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNo     *int
	DiscNo      int

	// Из ffprobe
	Codec       string
	MimeType    string
	DurationMS  *int64
	BitrateKbps *int
	SampleRate  *int

	// Обложка из тегов
	CoverData []byte
	CoverMime string

	// Источник метаданных: file | fallback | user
	Source MetadataSource
}

type MetadataSource string

const (
	MetaSourceFile MetadataSource = "file"
	// MetaSourceFallback название трека или имя исполнителя подставлено.
	MetaSourceFallback MetadataSource = "fallback"
	MetaSourceUser     MetadataSource = "user"
)

// ResolvedMetadata — метаданные + artist, album
type ResolvedMetadata struct {
	Extracted ExtractedMetadata
	Artist    *Artist
	Album     *Album
}
