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

// TrackUploadResponse содержит состояние принятого файла.
// queued ожидает обработки, processing обрабатывается, ready создал трек, failed завершился ошибкой.
// TrackPublicID заполнен для ready, ErrorCode — для failed. До завершения CompletedAt равен null.
type TrackUploadResponse struct {
	PublicID      domain.PublicID     `json:"public_id"`
	Status        domain.UploadStatus `json:"status" enums:"queued,processing,ready,failed"`
	TrackPublicID *domain.PublicID    `json:"track_public_id" extensions:"x-nullable"`
	ErrorCode     *string             `json:"error_code" enums:"duplicate_audio,target_unavailable,unsupported_audio,processing_failed" extensions:"x-nullable"`
	// CreatedAt — время приёма в миллисекундах от Unix epoch.
	CreatedAt int64 `json:"created_at"`
	// CompletedAt — время завершения в миллисекундах от Unix epoch.
	CompletedAt *int64 `json:"completed_at" extensions:"x-nullable"`
}

// NewTrackUploadResponse преобразует загрузку в публичный ответ для владельца.
func NewTrackUploadResponse(item *domain.TrackUpload) TrackUploadResponse {
	response := TrackUploadResponse{
		PublicID: item.PublicID, Status: item.Status, TrackPublicID: item.TrackID,
		ErrorCode: item.ErrorCode, CreatedAt: item.CreatedAt.UnixMilli(),
	}
	if item.CompletedAt != nil {
		ms := item.CompletedAt.UnixMilli()
		response.CompletedAt = &ms
	}
	return response
}
