// Package api связывает HTTP-маршруты с сервисами и репозиториями сервера.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger"
)

// Routes собирает HTTP-маршруты сервера.
// Проверка состояния, Swagger и POST /dev/users доступны без auth.
// Остальные маршруты проходят через auth. limit применяется только к загрузке треков.
func (h *Handlers) Routes(
	auth func(http.Handler) http.Handler,
	limit func(http.Handler) http.Handler,
) http.Handler {
	r := chi.NewRouter()

	// Публичные роуты
	r.Get("/health", handleHealth)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	// TODO: удалить для проды
	// Локальный bootstrap пользователя без авторизации
	r.Post("/dev/users", h.User.CreateUser)

	// Защищённые роуты
	r.Group(func(r chi.Router) {
		r.Use(auth)

		// Ingest — CPU-bound, под семафором.
		r.Route("/ingest", func(r chi.Router) {
			r.Use(limit)
			r.Post("/tracks", h.Ingest.Upload)
		})

		// Library — read-only, без лимита.
		r.Route("/lib", func(r chi.Router) {
			r.Get("/tracks", h.Library.ListTracks)
			r.Get("/tracks/{publicID}", h.Library.GetTrack)
			r.Get("/tracks/{publicID}/cover", h.Stream.Cover)
		})

		// Streaming — I/O-bound, без лимита.
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

// handleHealth возвращает статус сервиса
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","version":"0.1.0"}`))
}
