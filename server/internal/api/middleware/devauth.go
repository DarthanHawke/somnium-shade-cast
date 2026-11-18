package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// UserRepository загружает пользователя, связанного с запросом.
type UserRepository interface {
	GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.User, error)
}

// DevAuth подставляет активного пользователя по заголовку X-User в режиме разработки.
func DevAuth(users UserRepository, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("X-User")
			if raw == "" {
				respond.JSON(w, http.StatusUnauthorized, respond.ErrorBody{
					Error: "X-User required (dev mode)",
				})
				return
			}

			publicID := domain.PublicID(raw)
			if err := publicID.Validate(); err != nil {
				respond.JSON(w, http.StatusUnauthorized, respond.ErrorBody{
					Error: "invalid X-User",
				})
				return
			}

			user, err := users.GetByPublicID(r.Context(), publicID)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					respond.JSON(w, http.StatusNotFound, respond.ErrorBody{
						Error: "user not found",
					})
				} else {
					respond.WriteError(w, log, err)
				}
				return
			}
			if !user.IsActive {
				respond.JSON(w, http.StatusForbidden, respond.ErrorBody{
					Error: "user is not active",
				})
				return
			}

			ctx := WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
