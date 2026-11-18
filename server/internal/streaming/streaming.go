// Package streaming предоставляет доступ к аудиотрекам и обложкам.
package streaming

import (
	"context"
	"io"
	"path"
	"strings"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/library"
)

// TrackRepository читает записи треков с учётом доступа пользователя.
type TrackRepository interface {
	// GetReadableByPublicID возвращает трек, доступный пользователю viewerID.
	// Если трек отсутствует или недоступен, возвращает domain.ErrNotFound.
	GetReadableByPublicID(ctx context.Context, publicID domain.PublicID, viewerID domain.ID) (*domain.Track, error)
}

// LibraryRepository читает представления треков с данными об обложках.
type LibraryRepository interface {
	// GetReadableTrackByPublicID возвращает представление трека,
	// доступного пользователю viewerID. Обложка может отсутствовать.
	// Если трек отсутствует или недоступен, возвращает domain.ErrNotFound.
	GetReadableTrackByPublicID(
		ctx context.Context,
		publicID domain.PublicID,
		viewerID domain.ID,
	) (*library.TrackView, error)
}

// BlobRepository читает метаданные блобов.
type BlobRepository interface {
	// GetByID возвращает метаданные блоба по внутреннему идентификатору.
	// Если запись блоба отсутствует, возвращает domain.ErrNotFound.
	GetByID(ctx context.Context, id domain.ID) (*domain.Blob, error)
}

// Storage открывает расшифрованные данные блоба для чтения.
type BlobStorage interface {
	// OpenRange открывает диапазон по правилам [domain.Blob.ResolveRange]
	// и возвращает поток и фактический диапазон.
	// Вызывающий код должен закрыть поток, в том числе после ошибки чтения.
	// Если файл отсутствует, возвращает domain.ErrBlobFileMissing.
	OpenRange(b *domain.Blob, offset, length int64) (io.ReadCloser, domain.ByteRange, error)
}

// Service проверяет доступ к трекам и открывает потоки аудио и обложек.
type Service struct {
	tracks  TrackRepository
	library LibraryRepository
	blobs   BlobRepository
	store   BlobStorage
}

// New создаёт сервис чтения аудиотреков и обложек.
func New(tracks TrackRepository, library LibraryRepository, blobs BlobRepository, store BlobStorage) *Service {
	return &Service{tracks: tracks, library: library, blobs: blobs, store: store}
}

// TrackContent возвращает метаданные аудиоблоба и имя файла для скачивания.
// Если трек недоступен или для него нет аудиоблоба, возвращает domain.ErrNotFound.
func (s *Service) TrackContent(ctx context.Context,
	publicID domain.PublicID,
	viewerID domain.ID,
) (*Content, error) {
	track, err := s.tracks.GetReadableByPublicID(ctx, publicID, viewerID)
	if err != nil {
		return nil, err
	}
	if track.BlobID == nil {
		return nil, domain.ErrNotFound
	}

	blob, err := s.blobs.GetByID(ctx, *track.BlobID)
	if err != nil {
		return nil, err
	}

	if blob.Kind != domain.BlobAudio {
		return nil, domain.ErrNotFound
	}

	return &Content{
		Blob:     blob,
		Filename: downloadFilename(track),
	}, nil
}

// CoverContent возвращает метаданные обложки альбома доступного трека.
// Если трек недоступен или для него нет блоба изображения, возвращает domain.ErrNotFound.
func (s *Service) CoverContent(
	ctx context.Context,
	publicID domain.PublicID,
	viewerID domain.ID,
) (*Content, error) {
	view, err := s.library.GetReadableTrackByPublicID(ctx, publicID, viewerID)
	if err != nil {
		return nil, err
	}
	if view.CoverBlobID == nil {
		return nil, domain.ErrNotFound
	}

	blob, err := s.blobs.GetByID(ctx, *view.CoverBlobID)
	if err != nil {
		return nil, err
	}
	if blob.Kind != domain.BlobImage {
		return nil, domain.ErrNotFound
	}

	return &Content{Blob: blob}, nil
}

// OpenRange открывает диапазон расшифрованных данных для результата
// [Service.TrackContent] или [Service.CoverContent].
// Вызывающий код должен закрыть возвращённый поток.
func (s *Service) OpenRange(
	content *Content,
	offset, length int64,
) (io.ReadCloser, domain.ByteRange, error) {
	return s.store.OpenRange(content.Blob, offset, length)
}

// downloadFilename возвращает имя файла для скачивания трека с учётом расширения оригинального файла.
func downloadFilename(track *domain.Track) string {
	// Приводим разделители пути к формату, который понимает path.Ext.
	name := strings.ReplaceAll(track.OriginalFilename, "\\", "/")
	return track.Title + path.Ext(name)
}
