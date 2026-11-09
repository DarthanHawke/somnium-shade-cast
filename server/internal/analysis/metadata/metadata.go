// Package metadata извлекает метаданные аудио и связывает их с библиотекой.
package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/tags"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

const unknownArtist = "Unknown Artist"

type ArtistRepository interface {
	GetByNormName(ctx context.Context, norm string) (*domain.Artist, error)
	GetByID(ctx context.Context, id domain.ID) (*domain.Artist, error)
	Insert(ctx context.Context, artist *domain.Artist) error
}

type AlbumRepository interface {
	GetByArtistAndNormTitle(ctx context.Context, artistID domain.ID, norm string) (*domain.Album, error)
	GetByID(ctx context.Context, id domain.ID) (*domain.Album, error)
	Insert(ctx context.Context, album *domain.Album) error
}

type Prober interface {
	Probe(ctx context.Context, src io.ReaderAt, size int64) (*prober.ProbeResult, error)
}

type Tagger interface {
	Parse(ctx context.Context, src io.ReadSeeker) (*tags.ParsedTags, error)
}

// Service извлекает метаданные и находит или создаёт записи исполнителя и альбома.
type Service struct {
	artists ArtistRepository
	albums  AlbumRepository
	prober  Prober
	tags    Tagger
	log     *slog.Logger
}

// New создаёт сервис извлечения и разрешения метаданных.
func New(
	artists ArtistRepository,
	albums AlbumRepository,
	prober Prober,
	tags Tagger,
	log *slog.Logger,
) *Service {
	return &Service{
		artists: artists,
		albums:  albums,
		prober:  prober,
		tags:    tags,
		log:     log,
	}
}

// Extract читает технические характеристики и теги аудио из src.
// Если теги отсутствуют или не читаются, использует запасные значения.
func (s *Service) Extract(
	ctx context.Context,
	src io.ReaderAt,
	size int64,
	originalFilename string,
) (*domain.ExtractedMetadata, error) {
	const op = "metadata.Extract"

	probe, err := s.prober.Probe(ctx, src, size)
	if err != nil {
		if errors.Is(err, prober.ErrNotAudio) || errors.Is(err, prober.ErrInvalidFile) {
			return nil, domain.Invalid("file", "not a supported audio file")
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	fileTags, err := s.tags.Parse(ctx, io.NewSectionReader(src, 0, size))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		// Ошибка чтения тегов допускает загрузку с запасными метаданными.
		s.log.WarnContext(ctx, "tag parse failed", "err", err)
		fileTags = &tags.ParsedTags{}
	}

	meta := &domain.ExtractedMetadata{
		// Из тегов.
		Title:       strings.TrimSpace(fileTags.Title),
		ArtistName:  strings.TrimSpace(fileTags.Artist),
		AlbumTitle:  strings.TrimSpace(fileTags.Album),
		AlbumArtist: strings.TrimSpace(fileTags.AlbumArtist),
		Genre:       strings.TrimSpace(fileTags.Genre),
		Year:        fileTags.Year,
		DiscNo:      fileTags.DiscNo,
		CoverData:   fileTags.CoverData,
		CoverMime:   fileTags.CoverMime,

		// Из probe.
		Codec:       probe.Codec,
		MimeType:    probe.MimeType,
		DurationMS:  probe.DurationMS,
		SampleRate:  intPtr(probe.SampleRate),
		BitrateKbps: intPtr(probe.BitrateKbps),
	}

	if fileTags.TrackNo > 0 {
		n := fileTags.TrackNo
		meta.TrackNo = &n
	}

	meta.Source = domain.MetaSourceFile
	if meta.Title == "" {
		meta.Title = titleFromFilename(originalFilename)
		meta.Source = domain.MetaSourceFallback
	}
	if meta.ArtistName == "" {
		meta.ArtistName = unknownArtist
		meta.Source = domain.MetaSourceFallback
	}

	return meta, nil
}

// Resolve находит или создаёт исполнителя и альбом по метаданным.
// Если название альбома пустое, возвращает nil вместо альбома.
// Вызывающий код передаёт контекст транзакции, если операции должны быть атомарными.
func (s *Service) Resolve(
	ctx context.Context,
	extracted *domain.ExtractedMetadata,
) (*domain.Artist, *domain.Album, error) {
	artist, err := s.resolveArtist(ctx, extracted.ArtistName)
	if err != nil {
		return nil, nil, err
	}

	album, err := s.resolveAlbum(ctx, artist.ID, extracted.AlbumTitle)
	if err != nil {
		return nil, nil, err
	}

	return artist, album, nil
}

// resolveArtist — GetOrCreate
func (s *Service) resolveArtist(ctx context.Context, name string) (*domain.Artist, error) {
	const op = "metadata.resolveArtist"

	name = strings.TrimSpace(name)
	if name == "" {
		name = unknownArtist
	}

	norm := domain.Normalize(name)

	existing, err := s.artists.GetByNormName(ctx, norm)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	artist, err := domain.NewArtist(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	err = s.artists.Insert(ctx, artist)
	if errors.Is(err, domain.ErrConflict) {
		artist, err = s.artists.GetByNormName(ctx, norm)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return artist, nil
}

// resolveAlbum — GetOrCreate
func (s *Service) resolveAlbum(
	ctx context.Context,
	artistID domain.ID,
	title string,
) (*domain.Album, error) {
	const op = "metadata.resolveAlbum"

	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil
	}

	norm := domain.Normalize(title)

	existing, err := s.albums.GetByArtistAndNormTitle(ctx, artistID, norm)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	album, err := domain.NewAlbum(artistID, title, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	err = s.albums.Insert(ctx, album)
	if errors.Is(err, domain.ErrConflict) {
		album, err = s.albums.GetByArtistAndNormTitle(ctx, artistID, norm)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return album, nil
}

// GetNamesByID возвращает имя исполнителя и название альбома.
// Если albumID равен nil, название альбома пустое.
func (s *Service) GetNamesByID(
	ctx context.Context,
	artistID domain.ID,
	albumID *domain.ID,
) (string, string, error) {
	artist, err := s.artists.GetByID(ctx, artistID)
	if err != nil {
		return "", "", err
	}
	albumTitle := ""
	if albumID != nil {
		album, err := s.albums.GetByID(ctx, *albumID)
		if err != nil {
			return "", "", err
		}
		albumTitle = album.Title
	}
	return artist.Name, albumTitle, nil
}

// titleFromFilename берёт имя файла без расширения и обрезает пробелы по краям.
// Если название пустое, возвращает "Untitled".
func titleFromFilename(name string) string {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	title := strings.TrimSpace(strings.TrimSuffix(base, path.Ext(base)))
	if title == "" {
		return "Untitled"
	}
	return title
}

// intPtr возвращает указатель на v, если v != 0, иначе nil.
func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
