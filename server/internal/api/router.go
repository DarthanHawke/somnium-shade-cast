// Package api связывает HTTP-маршруты с сервисами и репозиториями сервера.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger"
)

// Routes собирает HTTP-маршруты сервера.
// Проверка состояния и Swagger открыты. В dev также открыты маршруты управления тестовыми пользователями.
// Остальные маршруты проходят через auth. limit применяется только к приёму файлов.
func (h *Handlers) Routes(
	auth func(http.Handler) http.Handler,
	limit func(http.Handler) http.Handler,
	dev bool,
) http.Handler {
	r := chi.NewRouter()

	// Публичные роуты
	r.Get("/health", handleHealth)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	if dev {
		h.User.devRoutes(r)
	}

	// Защищённые роуты
	r.Group(func(r chi.Router) {
		r.Use(auth)

		// Лимит применяется к приёму файлов. Статус доступен во время обработки.
		r.Route("/ingest", func(r chi.Router) {
			r.With(limit).Post("/uploads", h.Ingest.AcceptUpload)
			r.Get("/uploads", h.Ingest.ListUploads)
			r.Get("/uploads/{publicID}", h.Ingest.GetUpload)
		})

		r.Route("/lib", func(r chi.Router) {
			r.Get("/tracks", h.Library.ListTracks)
			r.Get("/tracks/{publicID}", h.Library.GetTrack)
			r.Get("/tracks/{publicID}/cover", h.Stream.Cover)
			r.Delete("/tracks/{publicID}", h.Library.RemoveTrack)
			r.Delete("/albums/{publicID}", h.Library.RemoveAlbumTracks)
			r.Delete("/artists/{publicID}", h.Library.RemoveArtistTracks)
		})

		r.Route("/stream", func(r chi.Router) {
			r.Get("/{publicID}", h.Stream.Stream)
			r.Head("/{publicID}", h.Stream.Stream)
		})

		r.Route("/tracks/{publicID}", func(r chi.Router) {
			r.Get("/download", h.Stream.Download)
			r.Head("/download", h.Stream.Download)
		})

		r.Route("/users", func(r chi.Router) {
			r.Post("/", h.User.CreateUser)
			r.Get("/{publicID}", h.User.GetUser)
			r.Post("/{publicID}/activate", h.User.ActivateUser)
			r.Post("/{publicID}/deactivate", h.User.DeactivateUser)
		})
	})

	return r
}

// handleHealth возвращает статус сервиса.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","version":"0.1.0"}`))
}

// devRoutes регистрирует открытые маршруты тестовых профилей.
func (h *UserHandler) devRoutes(r chi.Router) {
	// @Summary Создать тестовый профиль в режиме dev
	// @Tags Dev
	// @Accept json
	// @Produce json
	// @Param request body dto.UserRequest true "Данные пользователя"
	// @Success 201 {object} dto.UserResponse
	// @Failure 400 {object} respond.ErrorBody
	// @Router /dev/users [post]
	r.Post("/dev/users", h.CreateUser)
	r.Get("/dev/users", h.ListUsers)
	// @Summary Активировать тестовый профиль в режиме dev
	// @Tags Dev
	// @Param publicID path string true "ULID профиля"
	// @Success 200 {object} dto.SuccessResponse
	// @Failure 404 {object} respond.ErrorBody
	// @Router /dev/users/{publicID}/activate [post]
	r.Post("/dev/users/{publicID}/activate", h.ActivateUser)
	// @Summary Деактивировать тестовый профиль в режиме dev
	// @Tags Dev
	// @Param publicID path string true "ULID профиля"
	// @Success 200 {object} dto.SuccessResponse
	// @Failure 404 {object} respond.ErrorBody
	// @Router /dev/users/{publicID}/deactivate [post]
	r.Post("/dev/users/{publicID}/deactivate", h.DeactivateUser)
	r.Delete("/dev/users/{publicID}", h.DeleteDevUser)
}
