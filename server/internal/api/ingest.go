package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
)

// IngestService — сервис для обработки загрузки треков.
type IngestService interface {
	// Upload принимает src, сохраняет аудиофайл и регистрирует загруженный трек.
	// Если задан input.GhostID, материализует существующий ghost-трек,
	// сохраняя его название, исполнителя, альбом и нумерацию.
	// При input.ForceDup == false отклоняет повторную загрузку файла
	// владельцем с ошибкой domain.ErrConflict. Не закрывает src.
	// Не поддерживает выполнение в контексте существующей транзакции.
	// Ошибки постановки фоновых задач логируются и не отменяют успешную загрузку.
	Upload(ctx context.Context, req ingest.UploadInput, r io.Reader) (*ingest.UploadResult, error)
}

// IngestHandler — HTTP-обработчик для загрузки треков.
type IngestHandler struct {
	ingest IngestService
	log    *slog.Logger
}

// NewIngestHandler создает новый IngestHandler.
func NewIngestHandler(ingest IngestService, log *slog.Logger) *IngestHandler {
	return &IngestHandler{
		ingest: ingest,
		log:    log,
	}
}

// Upload принимает аудиофайл через POST /ingest/tracks.
// Запрос multipart/form-data начинается с части meta с JSON-объектом.
// Следом идёт часть file с аудиофайлом.
// При успехе возвращает 201 Created и представление трека.
func (h *IngestHandler) Upload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		respond.WriteError(w, h.log, domain.Invalid("content_type", "expected multipart/form-data"))
		return
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		respond.WriteError(w, h.log, domain.Invalid("content_type", "missing boundary"))
		return
	}

	mr := multipart.NewReader(r.Body, boundary)

	meta, err := readUploadMeta(mr)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	filePart, err := mr.NextPart()
	if err != nil {
		respond.WriteError(w, h.log, domain.Invalid("file", "missing file part"))
		return
	}

	if filePart.FormName() != "file" {
		respond.WriteError(w, h.log, domain.Invalid("file", "second part must be named 'file'"))
		return
	}

	req := ingest.UploadInput{
		OwnerID:          user.ID,
		Fingerprint:      meta.Fingerprint,
		FPDuration:       meta.FPDuration,
		ForceDup:         meta.ForceDup,
		GhostID:          meta.GhostID,
		OriginalFilename: filePart.FileName(),
	}

	result, err := h.ingest.Upload(r.Context(), req, filePart)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	resp := dto.NewUploadedTrackResponse(result)
	respond.JSON(w, http.StatusCreated, resp)
}

// readUploadMeta читает первую часть multipart-запроса.
// Часть должна называться meta и содержать один JSON-объект.
// Возвращает метаданные загрузки в виде dto.UploadMetaRequest.
func readUploadMeta(mr *multipart.Reader) (*dto.UploadMetaRequest, error) {
	part, err := mr.NextPart()
	if err != nil {
		return nil, domain.Invalid("meta", "missing meta part")
	}

	if part.FormName() != "meta" {
		return nil, domain.Invalid("meta", "first part must be named 'meta'")
	}

	const maxMetaSize = 64 * 1024 // защита от больших JSON

	data, err := io.ReadAll(io.LimitReader(part, maxMetaSize+1))
	if err != nil {
		return nil, domain.Invalid("meta", "cannot read metadata")
	}
	if len(data) > maxMetaSize {
		return nil, domain.Invalid("meta", "metadata exceeds 64 KiB")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var meta *dto.UploadMetaRequest
	if err := dec.Decode(&meta); err != nil {
		return nil, domain.Invalid("meta", "invalid JSON: "+err.Error())
	}
	if meta == nil {
		return nil, domain.Invalid("meta", "missing metadata")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, domain.Invalid("meta", "expected a single JSON object")
	}

	return meta, nil
}
