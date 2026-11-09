package ingest

import "github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"

// UploadInput задаёт владельца файла и параметры загрузки трека.
type UploadInput struct {
	// OwnerID задаётся вызывающим кодом из контекста текущего пользователя.
	OwnerID domain.ID

	// Fingerprint содержит необязательный Chromaprint-отпечаток от клиента.
	Fingerprint string

	// FPDuration содержит длительность фрагмента для Fingerprint в миллисекундах.
	// Ноль означает, что длительность не передана.
	FPDuration int

	// ForceDup разрешает добавить ещё один трек владельца с тем же SHA-256.
	ForceDup bool

	// GhostID задаёт ghost-трек для материализации.
	// При nil создаётся новый трек.
	GhostID *domain.PublicID

	// OriginalFilename содержит исходное имя файла.
	// Используется как запасной источник названия при отсутствии тега title.
	OriginalFilename string
}

// UploadResult содержит трек и связанные сущности после успешной загрузки.
// Track и Artist всегда заданы; Album равен nil, если альбом отсутствует.
type UploadResult struct {
	Track  *domain.Track
	Artist *domain.Artist
	Album  *domain.Album
}
