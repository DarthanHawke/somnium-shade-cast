package dto

import "github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"

// UploadMetaRequest содержит метаданные для загрузки трека.
type UploadMetaRequest struct {
	ForceDup    bool             `json:"force_dup"`
	GhostID     *domain.PublicID `json:"ghost_id"`
	Fingerprint string           `json:"fingerprint"`
	// FPDuration содержит длительность фрагмента для Fingerprint в миллисекундах.
	// Ноль означает, что длительность не передана.
	FPDuration int `json:"fp_duration"`
}
