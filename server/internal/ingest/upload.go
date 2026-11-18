package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
)

// ErrIdempotencyConflict означает повтор ключа с другими данными запроса.
var ErrIdempotencyConflict = errors.New("idempotency key already used for another upload")

// Accept сохраняет зашифрованный файл и атомарно регистрирует загрузку и задачу.
// Тот же ключ с тем же файлом и параметрами возвращает существующую загрузку.
func (s *Service) Accept(
	ctx context.Context, input UploadInput, key string, src io.Reader,
) (*domain.TrackUpload, error) {
	if err := validateUploadInput(input, key); err != nil {
		return nil, err
	}
	buffer, err := s.spool.Receive(ctx, src)
	if err != nil {
		return nil, err
	}

	hash, err := uploadHash(input, buffer.SHA256())
	if err != nil {
		return nil, err
	}
	item, err := s.byKey(ctx, input.OwnerID, key, hash)
	if err != nil || item != nil {
		return item, err
	}

	release := s.store.ProtectUpload()
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sha := buffer.SHA256()
	existing, err := s.blobs.GetBySHA256(ctx, sha)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	if existing != nil && existing.Kind != domain.BlobAudio {
		return nil, domain.Invalid("file", "not a supported audio file")
	}

	var written *domain.Blob
	if existing == nil {
		result, err := s.store.Write(buffer.Reader())
		if err != nil {
			return nil, err
		}
		written, err = domain.NewBlob(sha, buffer.Size(), result.SizeEncrypted, "",
			domain.BlobAudio, result.WrappedKey, result.MasterKeyID, result.ChunkSize, result.StoragePath)
		if err != nil {
			return nil, fmt.Errorf("ingest.Accept: build blob metadata: %w", err)
		}
	}

	err = s.tx.WithTransaction(ctx, func(ctx context.Context) error {
		prior, err := s.byKey(ctx, input.OwnerID, key, hash)
		if err != nil || prior != nil {
			item = prior
			return err
		}

		registered, err := s.registerBlobTx(ctx, existing, written)
		if err != nil {
			return err
		}
		if registered.Kind != domain.BlobAudio {
			return domain.Invalid("file", "not a supported audio file")
		}
		id := domain.NewPublicID()
		jobID, err := s.queue.Enqueue(ctx, domain.JobUploadTrack, uploadPayload{UploadID: id}, jobs.WithMaxAttempts(3))
		if err != nil {
			return err
		}
		if jobID == 0 {
			return errors.New("ingest.Accept: no job registered for upload")
		}
		item = &domain.TrackUpload{
			PublicID: id, OwnerID: input.OwnerID, IdempotencyKey: key, RequestHash: hash,
			BlobID: &registered.ID, JobID: &jobID, OriginalFilename: input.OriginalFilename,
			Fingerprint: input.Fingerprint, FPDuration: input.FPDuration, ForceDup: input.ForceDup,
			GhostID: input.GhostID, Status: domain.UploadQueued, CreatedAt: domain.UnixTimeMillis(time.Now()),
		}
		return s.uploads.Insert(ctx, item)
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) byKey(ctx context.Context, owner domain.ID, key, hash string) (*domain.TrackUpload, error) {
	item, err := s.uploads.ByKey(ctx, owner, key)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item.RequestHash != hash {
		return nil, ErrIdempotencyConflict
	}
	return item, nil
}

func uploadHash(input UploadInput, sha string) (string, error) {
	data, err := json.Marshal(struct {
		SHA         string           `json:"sha"`
		Filename    string           `json:"filename"`
		Fingerprint string           `json:"fingerprint"`
		FPDuration  int              `json:"fp_duration"`
		ForceDup    bool             `json:"force_dup"`
		GhostID     *domain.PublicID `json:"ghost_id"`
	}{sha, input.OriginalFilename, input.Fingerprint, input.FPDuration, input.ForceDup, input.GhostID})
	if err != nil {
		return "", fmt.Errorf("ingest.Accept: encode upload options: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateUploadInput(input UploadInput, key string) error {
	if len(key) == 0 || len(key) > 128 {
		return domain.Invalid("idempotency_key", "expected 1 to 128 visible ASCII characters")
	}
	for _, ch := range key {
		if ch < 33 || ch > 126 {
			return domain.Invalid("idempotency_key", "expected 1 to 128 visible ASCII characters")
		}
	}
	if input.GhostID != nil && input.GhostID.Validate() != nil {
		return domain.Invalid("ghost_id", "malformed id")
	}
	if input.FPDuration < 0 {
		return domain.Invalid("fp_duration", "expected a nonnegative duration")
	}
	return nil
}

// Get возвращает загрузку владельца по публичному идентификатору.
func (s *Service) Get(ctx context.Context, owner domain.ID, id domain.PublicID) (*domain.TrackUpload, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return s.uploads.ForOwner(ctx, owner, id)
}

// List возвращает страницу собственных загрузок пользователя.
func (s *Service) List(ctx context.Context, owner domain.ID, limit, offset int) ([]domain.TrackUpload, error) {
	return s.uploads.ListForOwner(ctx, owner, limit, offset)
}

// Process анализирует принятый файл и фиксирует один результат загрузки.
// Технические ошибки передаются воркеру для повторных попыток.
func (s *Service) Process(ctx context.Context, raw json.RawMessage) error {
	var payload uploadPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return jobs.Permanent(fmt.Errorf("ingest.Process: decode payload: %w", err))
	}
	if err := payload.UploadID.Validate(); err != nil {
		return jobs.Permanent(err)
	}
	job, ok := jobs.AttemptFromContext(ctx)
	if !ok {
		return errors.New("ingest.Process: missing job lease")
	}
	item, err := s.uploads.ByPublicID(ctx, payload.UploadID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil // Владелец мог удалить профиль.
	}
	if err != nil {
		return err
	}
	if item.BlobID == nil || item.JobID == nil || *item.JobID != job.ID {
		return nil
	}

	blob, err := s.blobs.GetByID(ctx, *item.BlobID)
	if err != nil {
		return err
	}
	reader, err := s.store.OpenReaderAt(blob)
	if err != nil {
		return err
	}
	metadata, err := s.metadata.Extract(ctx, reader, blob.SizePlain, item.OriginalFilename)
	closeErr := reader.Close()
	if _, invalid := errors.AsType[*domain.ValidationError](err); invalid && closeErr == nil {
		return s.reject(ctx, item.PublicID, job, "unsupported_audio")
	}
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}

	var existingCover, newCover *domain.Blob
	if item.GhostID == nil && metadata.AlbumTitle != "" {
		if mime := albumCoverMIME(metadata.CoverData); mime != "" {
			release := s.store.ProtectUpload()
			defer release()
			existingCover, newCover, err = s.prepareAlbumCover(ctx, metadata.CoverData, mime)
			if err != nil {
				return err
			}
		}
	}

	err = s.tx.WithTransaction(ctx, func(ctx context.Context) error {
		ok, err := s.uploads.LockAttempt(ctx, item.PublicID, job)
		if err != nil || !ok {
			return err
		}
		input := UploadInput{
			OwnerID: item.OwnerID, OriginalFilename: item.OriginalFilename, GhostID: item.GhostID,
			Fingerprint: item.Fingerprint, FPDuration: item.FPDuration, ForceDup: item.ForceDup,
		}
		result, err := s.saveTrackTx(ctx, input, metadata, blob.ID, blob.SHA256Plain)
		if err != nil {
			return err
		}
		if result.AlbumID != nil && (existingCover != nil || newCover != nil) {
			cover, err := s.registerBlobTx(ctx, existingCover, newCover)
			if err != nil {
				return err
			}
			if err := s.album.SetCoverIfAbsent(ctx, *result.AlbumID, cover.ID); err != nil {
				return err
			}
		}
		return s.uploads.Finish(ctx, item.PublicID, &result.PublicID, nil)
	})
	switch {
	case errors.Is(err, domain.ErrDuplicateAudio):
		return s.reject(ctx, item.PublicID, job, "duplicate_audio")
	case errors.Is(err, domain.ErrUploadTargetUnavailable):
		return s.reject(ctx, item.PublicID, job, "target_unavailable")
	default:
		return err
	}
}

// reject фиксирует публичный отказ, только если аренда попытки ещё действительна.
func (s *Service) reject(ctx context.Context, id domain.PublicID, job domain.Job, code string) error {
	return s.tx.WithTransaction(ctx, func(ctx context.Context) error {
		ok, err := s.uploads.LockAttempt(ctx, id, job)
		if err != nil || !ok {
			return err
		}
		return s.uploads.Finish(ctx, id, nil, &code)
	})
}
