package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/library"
)

const maxSearchQueryBytes = 256

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

	// SearchTracksByOwner возвращает страницу совпадений из библиотеки владельца.
	// queryText содержит обычный текст запроса.
	SearchTracksByOwner(
		ctx context.Context,
		ownerID domain.ID,
		queryText string,
		offset, limit int,
	) ([]library.TrackView, error)

	// RemoveTrack удаляет трек владельца и возвращает число удалённых записей.
	RemoveTrack(ctx context.Context, ownerID domain.ID, publicID domain.PublicID) (int, error)

	// RemoveAlbumTracks удаляет треки владельца из альбома и возвращает их число.
	RemoveAlbumTracks(ctx context.Context, ownerID domain.ID, albumID domain.PublicID) (int, error)

	// RemoveArtistTracks удаляет треки владельца с указанным исполнителем и возвращает их число.
	RemoveArtistTracks(ctx context.Context, ownerID domain.ID, artistID domain.PublicID) (int, error)
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
//
// @Summary Получить список треков или результаты поиска
// @Tags Library
// @Produce json
// @Param limit query int false "Размер страницы" minimum(1) maximum(200) default(50)
// @Param offset query int false "Сколько результатов пропустить" minimum(0) default(0)
// @Param q query string false "Поисковая строка до 256 байт"
// @Success 200 {array} dto.TrackResponse
// @Failure 400 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /lib/tracks [get]
func (h *LibraryHandler) ListTracks(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}

	limit, offset, err := parsePagination(r)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > maxSearchQueryBytes {
		respond.WriteError(w, h.log, domain.Invalid("q", "search query exceeds 256 bytes"))
		return
	}

	var views []library.TrackView
	if query == "" {
		views, err = h.library.ListTracksByOwner(r.Context(), user.ID, offset, limit)
	} else {
		views, err = h.library.SearchTracksByOwner(r.Context(), user.ID, query, offset, limit)
	}
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
//
// @Summary Получить метаданные трека
// @Tags Library
// @Produce json
// @Param publicID path string true "ULID трека"
// @Success 200 {object} dto.TrackResponse
// @Failure 404 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /lib/tracks/{publicID} [get]
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

// RemoveTrack обрабатывает DELETE /lib/tracks/{publicID}
// и удаляет один трек из библиотеки текущего пользователя.
//
// @Summary Удалить трек из своей библиотеки
// @Tags Library
// @Param publicID path string true "Публичный ULID"
// @Success 204
// @Failure 404 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /lib/tracks/{publicID} [delete]
func (h *LibraryHandler) RemoveTrack(w http.ResponseWriter, r *http.Request) {
	h.removeTracks(w, r, h.library.RemoveTrack)
}

// RemoveAlbumTracks обрабатывает DELETE /lib/albums/{publicID}
// и удаляет треки альбома из библиотеки текущего пользователя.
//
// @Summary Удалить треки альбома из своей библиотеки
// @Tags Library
// @Param publicID path string true "Публичный ULID"
// @Success 204
// @Failure 404 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /lib/albums/{publicID} [delete]
func (h *LibraryHandler) RemoveAlbumTracks(w http.ResponseWriter, r *http.Request) {
	h.removeTracks(w, r, h.library.RemoveAlbumTracks)
}

// RemoveArtistTracks обрабатывает DELETE /lib/artists/{publicID}
// и удаляет треки исполнителя из библиотеки текущего пользователя.
//
// @Summary Удалить треки исполнителя из своей библиотеки
// @Tags Library
// @Param publicID path string true "Публичный ULID"
// @Success 204
// @Failure 404 {object} respond.ErrorBody
// @Security DevAuth
// @Security BasicAuth
// @Router /lib/artists/{publicID} [delete]
func (h *LibraryHandler) RemoveArtistTracks(w http.ResponseWriter, r *http.Request) {
	h.removeTracks(w, r, h.library.RemoveArtistTracks)
}

func (h *LibraryHandler) removeTracks(
	w http.ResponseWriter,
	r *http.Request,
	remove func(context.Context, domain.ID, domain.PublicID) (int, error),
) {
	ctx := r.Context()
	owner, ok := middleware.UserFrom(ctx)
	if !ok {
		respond.WriteError(w, h.log, respond.ErrUnauthorized)
		return
	}

	publicID := domain.PublicID(pathParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	removed, err := remove(ctx, owner.ID, publicID)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	if removed == 0 {
		respond.WriteError(w, h.log, domain.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
