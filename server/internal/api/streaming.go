package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/streaming"
)

// StreamService — сервис для потоковой передачи треков и обложек.
type StreamService interface {
	// TrackContent возвращает метаданные аудиоблоба и имя файла для скачивания.
	// Если трек недоступен или для него нет аудиоблоба, возвращает domain.ErrNotFound.
	TrackContent(ctx context.Context, publicID domain.PublicID, viewerID domain.ID) (*streaming.Content, error)

	// CoverContent возвращает метаданные обложки альбома доступного трека.
	// Если трек недоступен или для него нет блоба изображения, возвращает domain.ErrNotFound.
	CoverContent(ctx context.Context, publicID domain.PublicID, viewerID domain.ID) (*streaming.Content, error)

	// OpenRange открывает диапазон расшифрованных данных для результата
	// [Service.TrackContent] или [Service.CoverContent].
	// Вызывающий код должен закрыть возвращённый поток.
	OpenRange(content *streaming.Content, offset, length int64) (io.ReadCloser, domain.ByteRange, error)
}

// StreamHandler — HTTP-обработчик для потоковой передачи треков и обложек.
type StreamHandler struct {
	stream StreamService
	log    *slog.Logger
}

// NewStreamHandler создает новый StreamHandler.
func NewStreamHandler(stream StreamService, log *slog.Logger) *StreamHandler {
	return &StreamHandler{
		stream: stream,
		log:    log,
	}
}

// Stream отдаёт аудио доступного трека для воспроизведения.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.serveTrack(w, r, false)
}

// Download отдаёт аудио доступного трека как вложение.
func (h *StreamHandler) Download(w http.ResponseWriter, r *http.Request) {
	h.serveTrack(w, r, true)
}

func (h *StreamHandler) serveTrack(w http.ResponseWriter, r *http.Request, asAttachment bool) {
	w.Header().Set("Cache-Control", "no-store")

	publicID := domain.PublicID(pathParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	ctx := r.Context()
	viewer, ok := middleware.UserFrom(ctx)
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}

	content, err := h.stream.TrackContent(ctx, publicID, viewer.ID)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	h.serveContent(w, r, content, asAttachment)
}

// Cover отдаёт обложку трека, доступного текущему пользователю.
func (h *StreamHandler) Cover(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	pid := domain.PublicID(pathParam(r, "publicID"))
	if err := pid.Validate(); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	ctx := r.Context()
	viewer, ok := middleware.UserFrom(ctx)
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}

	c, err := h.stream.CoverContent(ctx, pid, viewer.ID)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	h.serveContent(w, r, c, false)
}
