// Package tags читает встроенные теги аудиофайлов.
package tags

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/dhowden/tag"
)

// Parser читает теги из источника с произвольным доступом.
type Parser struct{}

// NewParser создаёт парсер аудиотегов.
func NewParser() *Parser {
	return &Parser{}
}

// ParsedTags содержит значения тегов аудиофайла.
// Нулевые значения соответствуют отсутствующим тегам.
type ParsedTags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNo     int
	DiscNo      int
	CoverData   []byte // сырые байты обложки (jpg/png)
	CoverMime   string
}

// Parse читает теги из src. Если теги отсутствуют, возвращает пустой результат.
// Отмена контекста проверяется до и после чтения.
func (p *Parser) Parse(ctx context.Context, src io.ReadSeeker) (*ParsedTags, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsed, err := ParseFromReader(src)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return parsed, err
}

// ParseFromReader — вариант для случаев, когда файл уже открыт
// (например, при стриминге из multipart без сохранения в tmp)
func ParseFromReader(r io.ReadSeeker) (*ParsedTags, error) {
	m, err := tag.ReadFrom(r)
	if errors.Is(err, tag.ErrNoTagsFound) {
		return &ParsedTags{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read audio tags: %v", err)
	}
	parsed := &ParsedTags{
		Title:       m.Title(),
		Artist:      m.Artist(),
		Album:       m.Album(),
		AlbumArtist: m.AlbumArtist(),
		Genre:       m.Genre(),
		Year:        m.Year(),
	}
	trackNo, _ := m.Track()
	parsed.TrackNo = trackNo
	discNo, _ := m.Disc()
	if discNo == 0 {
		discNo = 1
	}
	parsed.DiscNo = discNo
	if pic := m.Picture(); pic != nil {
		parsed.CoverData = pic.Data
		parsed.CoverMime = pic.MIMEType
	}
	return parsed, nil
}
