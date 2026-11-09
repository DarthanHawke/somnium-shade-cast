package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
)

// Upload принимает src, сохраняет аудиофайл и регистрирует загруженный трек.
// Если задан input.GhostID, материализует существующий ghost-трек,
// сохраняя его название, исполнителя, альбом и нумерацию.
// При input.ForceDup == false отклоняет повторную загрузку файла
// владельцем с ошибкой domain.ErrConflict. Не закрывает src.
// Не поддерживает выполнение в контексте существующей транзакции.
// Ошибки постановки фоновых задач логируются и не отменяют успешную загрузку.
func (s *Service) Upload(
	ctx context.Context,
	input UploadInput,
	src io.Reader,
) (*UploadResult, error) {
	if input.GhostID != nil {
		if err := input.GhostID.Validate(); err != nil {
			return nil, domain.Invalid("ghost_id", "malformed id")
		}
	}

	buffer, err := s.spool.Receive(ctx, src)
	if err != nil {
		return nil, err
	}

	extracted, err := s.metadata.Extract(ctx, buffer, buffer.Size(), input.OriginalFilename)
	if err != nil {
		return nil, err
	}

	sha := buffer.SHA256()
	existingBlob, err := s.blobs.GetBySHA256(ctx, sha)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	var newBlob *domain.Blob
	if existingBlob == nil {
		writeResult, err := s.store.Write(buffer.Reader())
		if err != nil {
			return nil, err
		}
		newBlob, err = domain.NewBlob(
			sha, buffer.Size(), writeResult.SizeEncrypted,
			extracted.MimeType, domain.BlobAudio,
			writeResult.WrappedKey, writeResult.MasterKeyID,
			writeResult.ChunkSize, writeResult.StoragePath,
		)
		if err != nil {
			return nil, fmt.Errorf("ingest.Upload: create blob: %v", err)
		}
	}

	if !input.ForceDup {
		if err := s.checkDuplicate(ctx, input.OwnerID, sha); err != nil {
			return nil, err
		}
	}

	var result *UploadResult
	err = s.tx.WithTransaction(ctx, func(ctx context.Context) error {
		blobID, err := s.registerBlobTx(ctx, existingBlob, newBlob)
		if err != nil {
			return err
		}

		if err := s.blobs.IncRef(ctx, blobID); err != nil {
			return err
		}
		// Между первой проверкой и транзакцией мог появиться дубликат.
		if !input.ForceDup {
			if err := s.checkDuplicate(ctx, input.OwnerID, sha); err != nil {
				return err
			}
		}
		if input.GhostID != nil {
			result, err = s.materializeTx(ctx, input, extracted, blobID)
		} else {
			result, err = s.createTrackTx(ctx, input, extracted, blobID)
		}
		return err
	})
	if err != nil {
		if newBlob != nil {
			if removeErr := s.store.Remove(newBlob.StoragePath); removeErr != nil {
				s.log.Warn("failed to remove orphaned blob after transaction error",
					"storage_path", newBlob.StoragePath, "err", removeErr)
			} else {
				s.log.Debug("removed orphaned blob after transaction error",
					"storage_path", newBlob.StoragePath)
			}
		}
		return nil, err
	}
	// Enqueue jobs
	s.enqueueJobs(ctx, result.Track.ID)

	return result, nil
}

// createTrackTx создаёт трек по метаданным файла и добавляет его в поисковый индекс.
// Поиск и создание исполнителя и альбома выполняются в той же транзакции.
// ctx должен содержать транзакцию.
func (s *Service) createTrackTx(
	ctx context.Context,
	req UploadInput,
	extracted *domain.ExtractedMetadata,
	blobID domain.ID,
) (*UploadResult, error) {
	artist, album, err := s.metadata.Resolve(ctx, extracted)
	if err != nil {
		return nil, err
	}

	var albumID *domain.ID
	albumTitle := ""
	if album != nil {
		albumID = &album.ID
		albumTitle = album.Title
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
	})
	if err != nil {
		return nil, err
	}
	if err := s.tracks.Insert(ctx, track); err != nil {
		return nil, err
	}
	if err := s.search.Insert(ctx, track.ID, track.Title, artist.Name, albumTitle); err != nil {
		return nil, err
	}

	return &UploadResult{
		Track:  track,
		Artist: artist,
		Album:  album,
	}, nil
}

// materializeTx прикрепляет загруженный файл к ghost-треку,
// сохраняя его идентичность и нумерацию.
// req.GhostID должен быть задан; ctx должен содержать транзакцию.
func (s *Service) materializeTx(
	ctx context.Context,
	req UploadInput,
	extracted *domain.ExtractedMetadata,
	blobID domain.ID,
) (*UploadResult, error) {
	ghost, err := s.tracks.GetByPublicID(ctx, *req.GhostID)
	if err != nil {
		return nil, err
	}
	if ghost.OwnerID != req.OwnerID {
		return nil, domain.ErrForbidden
	}
	if ghost.Status != domain.TrackGhost || ghost.BlobID != nil {
		return nil, domain.ErrConflict
	}

	artist, err := s.artists.GetByID(ctx, ghost.ArtistID)
	if err != nil {
		return nil, err
	}
	var album *domain.Album
	albumTitle := ""
	if ghost.AlbumID != nil {
		album, err = s.albums.GetByID(ctx, *ghost.AlbumID)
		if err != nil {
			return nil, err
		}
		albumTitle = album.Title
	}

	// Идентичность берётся из ghost; из загрузки переносим только данные файла.
	ghost.BlobID = &blobID
	ghost.Status = domain.TrackUploaded
	ghost.DurationMS = extracted.DurationMS
	ghost.Codec = extracted.Codec
	ghost.BitrateKbps = extracted.BitrateKbps
	ghost.SampleRate = extracted.SampleRate
	ghost.OriginalFilename = req.OriginalFilename
	ghost.Fingerprint = req.Fingerprint
	ghost.FPDuration = req.FPDuration
	ghost.IsIntentionalDup = req.ForceDup
	ghost.UpdatedAt = domain.UnixTimeMillis(time.Now())

	if err := s.tracks.Materialize(ctx, ghost); err != nil {
		return nil, err
	}
	if err := s.search.Insert(ctx, ghost.ID, ghost.Title, artist.Name, albumTitle); err != nil {
		return nil, err
	}

	return &UploadResult{
		Track:  ghost,
		Artist: artist,
		Album:  album,
	}, nil
}

// enqueueJobs запрашивает анализ и обогащение трека.
// Ошибки постановки задач логируются и не передаются вызывающему коду.
func (s *Service) enqueueJobs(ctx context.Context, trackID domain.ID) {
	payload := map[string]any{"track_id": trackID}
	if err := s.jobs.Enqueue(ctx, domain.JobAnalyzeAudio, payload,
		jobs.WithDedupKey(fmt.Sprintf("analyze:%d", trackID)),
		jobs.WithPriority(7),
	); err != nil {
		s.log.Warn("enqueue analyze failed", "track_id", trackID, "err", err)
	}
	if err := s.jobs.Enqueue(ctx, domain.JobEnrichTrack, payload,
		jobs.WithDedupKey(fmt.Sprintf("enrich:%d", trackID)),
		jobs.WithPriority(5),
	); err != nil {
		s.log.Warn("enqueue enrich failed", "track_id", trackID, "err", err)
	}
}

// checkDuplicate возвращает domain.ErrConflict, если у владельца уже есть
// загруженный трек с SHA-256 sha. При отсутствии совпадения возвращает nil.
func (s *Service) checkDuplicate(ctx context.Context, ownerID domain.ID, sha string) error {
	_, err := s.tracks.GetByOwnerAndSHA256(ctx, sha, ownerID)
	switch {
	case err == nil:
		return domain.ErrConflict
	case errors.Is(err, domain.ErrNotFound):
		return nil
	default:
		return err
	}
}

// registerBlobTx возвращает ID блоба для привязки трека.
// Регистрирует newBlob либо использует ранее найденный existingBlob.
// При конкурентной регистрации того же SHA-256 использует зарегистрированный блоб.
// При успешном использовании чужой записи файл newBlob остаётся для GC.
// ctx должен содержать транзакцию.
func (s *Service) registerBlobTx(
	ctx context.Context,
	existingBlob, newBlob *domain.Blob,
) (domain.ID, error) {
	const op = "ingest.registerBlobTx"

	if newBlob == nil {
		if existingBlob == nil {
			return 0, fmt.Errorf("%s: %w", op, domain.ErrNoBlobAvailable)
		}
		return s.reuseBlob(existingBlob)
	}

	if err := s.blobs.Insert(ctx, newBlob); err != nil {
		if !errors.Is(err, domain.ErrBlobAlreadyExist) {
			return 0, err
		}

		// Другая загрузка зарегистрировала тот же файл после нашей проверки.
		registeredBlob, err := s.blobs.GetBySHA256(ctx, newBlob.SHA256Plain)
		if err != nil {
			return 0, err
		}

		return s.reuseBlob(registeredBlob)
	}

	return newBlob.ID, nil
}

// reuseBlob проверяет, что существующий блоб открывается, и возвращает его ID.
func (s *Service) reuseBlob(blob *domain.Blob) (domain.ID, error) {
	r, err := s.store.Open(blob)
	if err != nil {
		return 0, fmt.Errorf("ingest.reuseBlob: open blob %d: %w", blob.ID, err)
	}
	if err := r.Close(); err != nil {
		return 0, fmt.Errorf("ingest.reuseBlob: close blob %d: %v", blob.ID, err)
	}
	return blob.ID, nil
}
