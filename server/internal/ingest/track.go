package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// saveTrackTx проверяет дубликаты и сохраняет конечную ссылку на аудиофайл.
// Создание трека, изменение счётчика и FTS используют общую транзакцию.
func (s *Service) saveTrackTx(
	ctx context.Context,
	input UploadInput,
	extracted *domain.ExtractedMetadata,
	blobID domain.ID,
	sha string,
) (*domain.Track, error) {
	if !input.ForceDup {
		if err := s.checkDuplicate(ctx, input.OwnerID, sha); err != nil {
			return nil, err
		}
	}
	if err := s.blobs.SetAudioMIME(ctx, blobID, extracted.MIMEType); err != nil {
		return nil, err
	}
	if err := s.blobs.IncRef(ctx, blobID); err != nil {
		return nil, err
	}
	if input.GhostID != nil {
		return s.materializeTx(ctx, input, extracted, blobID)
	}
	return s.createTrackTx(ctx, input, extracted, blobID)
}

// createTrackTx создаёт трек по метаданным файла и добавляет его в поисковый индекс.
// Поиск и создание исполнителя и альбома выполняются в той же транзакции.
// ctx должен содержать транзакцию.
func (s *Service) createTrackTx(
	ctx context.Context,
	req UploadInput,
	extracted *domain.ExtractedMetadata,
	blobID domain.ID,
) (*domain.Track, error) {
	artist, album, err := s.metadata.Resolve(ctx, extracted)
	if err != nil {
		return nil, err
	}

	var albumID *domain.ID
	if album != nil {
		albumID = &album.ID
	}
	track, err := domain.NewTrack(domain.NewTrackParams{
		OwnerID:          req.OwnerID,
		ArtistID:         artist.ID,
		AlbumID:          albumID,
		Title:            extracted.Title,
		TrackNo:          extracted.TrackNo,
		DiscNo:           extracted.DiscNo,
		DurationMS:       extracted.DurationMS,
		Status:           domain.TrackUploaded,
		BlobID:           &blobID,
		Fingerprint:      req.Fingerprint,
		FPDuration:       req.FPDuration,
		IsIntentionalDup: req.ForceDup,
		OriginalFilename: req.OriginalFilename,
		Codec:            extracted.Codec,
		BitrateKbps:      extracted.BitrateKbps,
		SampleRate:       extracted.SampleRate,
		Channels:         extracted.Channels,
	})
	if err != nil {
		return nil, err
	}
	if err := s.tracks.Insert(ctx, track); err != nil {
		return nil, err
	}
	if err := s.search.IndexTrack(ctx, track.ID); err != nil {
		return nil, err
	}

	return track, nil
}

// materializeTx прикрепляет загруженный файл к ghost-треку,
// сохраняя его идентичность и нумерацию.
// req.GhostID должен быть задан; ctx должен содержать транзакцию.
func (s *Service) materializeTx(
	ctx context.Context,
	req UploadInput,
	extracted *domain.ExtractedMetadata,
	blobID domain.ID,
) (*domain.Track, error) {
	ghost, err := s.tracks.GetByPublicID(ctx, *req.GhostID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrUploadTargetUnavailable
	}
	if err != nil {
		return nil, err
	}
	if ghost.OwnerID != req.OwnerID || ghost.Status != domain.TrackGhost || ghost.BlobID != nil {
		return nil, domain.ErrUploadTargetUnavailable
	}

	// Идентичность берётся из ghost; из загрузки переносим только данные файла.
	ghost.BlobID = &blobID
	ghost.Status = domain.TrackUploaded
	ghost.DurationMS = extracted.DurationMS
	ghost.Codec = extracted.Codec
	ghost.BitrateKbps = extracted.BitrateKbps
	ghost.SampleRate = extracted.SampleRate
	ghost.Channels = extracted.Channels
	ghost.OriginalFilename = req.OriginalFilename
	ghost.Fingerprint = req.Fingerprint
	ghost.FPDuration = req.FPDuration
	ghost.IsIntentionalDup = req.ForceDup
	ghost.UpdatedAt = domain.UnixTimeMillis(time.Now())

	if err := s.tracks.Materialize(ctx, ghost); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, domain.ErrUploadTargetUnavailable
		}
		return nil, err
	}
	if err := s.search.IndexTrack(ctx, ghost.ID); err != nil {
		return nil, err
	}

	return ghost, nil
}

// checkDuplicate возвращает domain.ErrDuplicateAudio, если у владельца уже есть
// загруженный трек с SHA-256 sha. При отсутствии совпадения возвращает nil.
func (s *Service) checkDuplicate(ctx context.Context, ownerID domain.ID, sha string) error {
	_, err := s.tracks.GetByOwnerAndSHA256(ctx, sha, ownerID)
	switch {
	case err == nil:
		return domain.ErrDuplicateAudio
	case errors.Is(err, domain.ErrNotFound):
		return nil
	default:
		return err
	}
}

// registerBlobTx возвращает запись блоба для привязки к треку или альбому.
// Регистрирует newBlob либо использует ранее найденный existingBlob.
// При конкурентной регистрации того же SHA-256 использует зарегистрированный блоб.
// При успешном использовании чужой записи файл newBlob остаётся для GC.
// ctx должен содержать транзакцию.
func (s *Service) registerBlobTx(
	ctx context.Context,
	existingBlob, newBlob *domain.Blob,
) (*domain.Blob, error) {
	const op = "ingest.registerBlobTx"

	if newBlob != nil {
		if err := s.blobs.Insert(ctx, newBlob); err == nil {
			return newBlob, nil
		} else if !errors.Is(err, domain.ErrBlobAlreadyExist) {
			return nil, err
		}

		var err error
		existingBlob, err = s.blobs.GetBySHA256(ctx, newBlob.SHA256Plain)
		if err != nil {
			return nil, err
		}
	}
	if existingBlob == nil {
		return nil, fmt.Errorf("%s: %w", op, domain.ErrNoBlobAvailable)
	}

	reader, err := s.store.Open(existingBlob)
	if err != nil {
		return nil, fmt.Errorf("%s: open blob %d: %w", op, existingBlob.ID, err)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("%s: close blob %d: %w", op, existingBlob.ID, err)
	}
	return existingBlob, nil
}
