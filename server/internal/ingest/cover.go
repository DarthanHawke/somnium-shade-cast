package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// albumCoverMIME возвращает тип поддерживаемой обложки в пределах ограничения.
func albumCoverMIME(data []byte) string {
	const maxBytes = 8 << 20
	const maxSide = 4096
	if len(data) == 0 || len(data) > maxBytes {
		return ""
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide {
		return ""
	}
	switch format {
	case "jpeg", "png":
		return "image/" + format
	default:
		return ""
	}
}

// prepareAlbumCover находит зарегистрированную обложку либо шифрует новый файл.
// Вызывающий код держит защиту от GC до окончания транзакции привязки.
func (s *Service) prepareAlbumCover(ctx context.Context, data []byte, mime string) (existing, written *domain.Blob, err error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	found, err := s.blobs.GetBySHA256(ctx, sha)
	switch {
	case err == nil:
		if found.Kind == domain.BlobImage {
			return found, nil, nil
		}
		return nil, nil, nil
	case !errors.Is(err, domain.ErrNotFound):
		return nil, nil, err
	}

	result, err := s.store.Write(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	written, err = domain.NewBlob(sha, int64(len(data)), result.SizeEncrypted, mime,
		domain.BlobImage, result.WrappedKey, result.MasterKeyID, result.ChunkSize, result.StoragePath)
	return nil, written, err
}
