package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// IngestService задаёт операции приёма и чтения собственных загрузок.
type IngestService interface {
	Accept(ctx context.Context, input ingest.UploadInput, key string, src io.Reader) (*domain.TrackUpload, error)
	Get(ctx context.Context, owner domain.ID, id domain.PublicID) (*domain.TrackUpload, error)
	List(ctx context.Context, owner domain.ID, limit, offset int) ([]domain.TrackUpload, error)
}

// IngestHandler обслуживает приём файлов и историю загрузки.
type IngestHandler struct {
	ingest IngestService
	log    *slog.Logger
}

// NewIngestHandler создаёт обработчик загрузки.
func NewIngestHandler(service IngestService, log *slog.Logger) *IngestHandler {
	return &IngestHandler{ingest: service, log: log}
}

// AcceptUpload принимает multipart-загрузку и возвращает адрес загрузки после
// устойчивой регистрации файла и задачи. Требуется заголовок Idempotency-Key.
//
// @Summary Принять файл для фоновой обработки
// @Tags Uploads
// @Accept mpfd
// @Produce json
// @Param Idempotency-Key header string true "Ключ повторной отправки"
// @Param meta formData string true "JSON с метаданными. Часть meta идёт первой."
// @Param file formData file true "Аудиофайл"
// @Success 202 {object} dto.TrackUploadResponse
// @Header 202 {string} Location "Путь для опроса состояния"
// @Failure 400 {object} respond.ErrorBody
// @Failure 409 {object} respond.ErrorBody
// @Failure 413 {object} respond.ErrorBody
// @Failure 503 {object} respond.ErrorBody
// @Header 503 {string} Retry-After "Ожидание перед повтором в секундах"
// @Security DevAuth
// @Security BasicAuth
// @Router /ingest/uploads [post]
func (h *IngestHandler) AcceptUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		respond.WriteError(w, h.log, domain.Invalid("idempotency_key", "expected one Idempotency-Key header"))
		return
	}
	req, file, err := readTrackUpload(r)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	req.OwnerID = user.ID
	item, err := h.ingest.Accept(r.Context(), req, keys[0], file)
	if errors.Is(err, ingest.ErrIdempotencyConflict) {
		respond.JSON(w, http.StatusConflict, respond.ErrorBody{Error: err.Error(), Field: "idempotency_key"})
		return
	}
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	w.Header().Set("Location", "/ingest/uploads/"+string(item.PublicID))
	respond.JSON(w, http.StatusAccepted, dto.NewTrackUploadResponse(item))
}

// GetUpload возвращает состояние загрузки текущего пользователя.
//
// @Summary Получить состояние загрузки
// @Tags Uploads
// @Produce json
// @Param publicID path string true "ULID загрузки"
// @Success 200 {object} dto.TrackUploadResponse
// @Failure 400 {object} respond.ErrorBody
// @Failure 404 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /ingest/uploads/{publicID} [get]
func (h *IngestHandler) GetUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	item, err := h.ingest.Get(r.Context(), user.ID, domain.PublicID(pathParam(r, "publicID")))
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.NewTrackUploadResponse(item))
}

// ListUploads возвращает историю загрузок текущего пользователя.
//
// @Summary Получить список загрузок
// @Tags Uploads
// @Produce json
// @Param limit query int false "Размер страницы" minimum(1) maximum(200) default(50)
// @Param offset query int false "Сколько результатов пропустить" minimum(0) default(0)
// @Success 200 {array} dto.TrackUploadResponse
// @Failure 400 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /ingest/uploads [get]
func (h *IngestHandler) ListUploads(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	limit, offset, err := parsePagination(r)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	items, err := h.ingest.List(r.Context(), user.ID, limit, offset)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	result := make([]dto.TrackUploadResponse, 0, len(items))
	for _, item := range items {
		result = append(result, dto.NewTrackUploadResponse(&item))
	}
	respond.JSON(w, http.StatusOK, result)
}

// readTrackUpload читает параметры и открывает часть file общего multipart-запроса.
func readTrackUpload(r *http.Request) (ingest.UploadInput, *multipart.Part, error) {
	var input ingest.UploadInput
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return input, nil, domain.Invalid("content_type", "expected multipart/form-data")
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return input, nil, domain.Invalid("content_type", "missing boundary")
	}

	mr := multipart.NewReader(r.Body, boundary)

	meta, err := readUploadMeta(mr)
	if err != nil {
		return input, nil, err
	}

	filePart, err := mr.NextPart()
	if err != nil {
		return input, nil, domain.Invalid("file", "missing file part")
	}

	if filePart.FormName() != "file" {
		return input, nil, domain.Invalid("file", "second part must be named 'file'")
	}

	input = ingest.UploadInput{
		Fingerprint:      meta.Fingerprint,
		FPDuration:       meta.FPDuration,
		ForceDup:         meta.ForceDup,
		GhostID:          meta.GhostID,
		OriginalFilename: filePart.FileName(),
	}

	return input, filePart, nil
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
