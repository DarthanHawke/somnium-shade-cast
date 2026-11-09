package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

type UserRepository interface {
	GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.User, error)
}

// DevAuth — middleware для dev-режима, который позволяет аутентифицировать пользователя по заголовку X-User.
func DevAuth(users UserRepository, enabled bool, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}

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
