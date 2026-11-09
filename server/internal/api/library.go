package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/library"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// LibraryRepository — интерфейс для доступа к библиотеке треков.
type LibraryRepository interface {
	// GetReadableTrackByPublicID возвращает представление трека, доступного
	// для чтения пользователю viewerID. Альбом может отсутствовать.
	// Если трек отсутствует или недоступен, возвращает domain.ErrNotFound.
	GetReadableTrackByPublicID(
		ctx context.Context,
		publicID domain.PublicID,
		viewerID domain.ID,
	) (*library.TrackView, error)

	// ListTracksByOwner возвращает представления треков владельца ownerID.
	// Результат упорядочен по CreatedAt и внутреннему ID трека по убыванию.
	// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
	ListTracksByOwner(
		ctx context.Context,
		ownerID domain.ID,
		offset, limit int,
	) ([]library.TrackView, error)
}

// LibraryHandler — HTTP-обработчик для работы с библиотекой треков.
type LibraryHandler struct {
	library LibraryRepository
	log     *slog.Logger
}

// NewLibraryHandler создает новый LibraryHandler.
func NewLibraryHandler(
	library LibraryRepository,
	log *slog.Logger,
) *LibraryHandler {
	return &LibraryHandler{
		library: library,
		log:     log,
	}
}

// ListTracks обрабатывает GET /lib/tracks и возвращает страницу треков
// текущего пользователя.
func (h *LibraryHandler) ListTracks(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}

	offset := queryInt(r, "offset", 0)
	limit := queryInt(r, "limit", defaultLimit)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}

	views, err := h.library.ListTracksByOwner(r.Context(), user.ID, offset, limit)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	items := make([]dto.TrackResponse, len(views))
	for i := range views {
		items[i] = dto.NewTrackResponse(&views[i])
	}

	respond.JSON(w, http.StatusOK, items)

}

// GetTrack обрабатывает GET /lib/tracks/{publicID} и возвращает трек,
// если он доступен текущему пользователю.
func (h *LibraryHandler) GetTrack(w http.ResponseWriter, r *http.Request) {
	raw := pathParam(r, "publicID")
	pid := domain.PublicID(raw)
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
	result, err := h.library.GetReadableTrackByPublicID(r.Context(), pid, viewer.ID)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	resp := dto.NewTrackResponse(result)
	respond.JSON(w, http.StatusOK, resp)

}
