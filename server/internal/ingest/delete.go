package ingest

import (
	"context"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// Delete удаляет трек и его запись в поисковом индексе.
// Уменьшает число ссылок на блоб; при нулевом счётчике удаляет запись блоба.
// После успешной фиксации собственной транзакции пытается удалить файл
// блоба, который больше не используется.
// Ошибка удаления файла логируется и не возвращается вызывающему коду.
// Если трек отсутствует, возвращает domain.ErrNotFound.
func (s *Service) Delete(ctx context.Context, publicID domain.PublicID) error {
	var orphanPath string

	err := s.tx.WithTransaction(ctx, func(ctx context.Context) error {
		track, err := s.tracks.GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		artistName, albumTitle, err := s.metadata.GetNamesByID(ctx, track.ArtistID, track.AlbumID)
		if err != nil {
			return err
		}
		if err := s.search.Remove(ctx, track.ID, track.Title, artistName, albumTitle); err != nil {
			return err
		}
		if err := s.tracks.Delete(ctx, publicID); err != nil {
			return err
		}
		if track.BlobID == nil {
			return nil
		}
		newCount, err := s.blobs.DecRef(ctx, *track.BlobID)
		if err != nil {
			return err
		}
		if newCount == 0 {
			path, err := s.blobs.DeleteIfOrphan(ctx, *track.BlobID)
			if err != nil {
				return err
			}
			orphanPath = path
		}
		return nil
	})
	if err != nil {
		return err
	}

	if orphanPath != "" {
		if err := s.store.Remove(orphanPath); err != nil {
			s.log.Warn("failed to remove orphan file", "storage_path", orphanPath, "err", err)
		}
	}
	return nil
}
