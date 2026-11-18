package domain

// ExtractedMetadata содержит теги и технические характеристики аудиофайла.
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
	MIMEType    string
	DurationMS  *int64
	BitrateKbps *int
	SampleRate  *int
	Channels    *int

	// Обложка из тегов
	CoverData []byte

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
