package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// AdminAuth проверяет Basic Auth для настроенного администратора.
// Для каждого запроса проверяет, что запись по-прежнему активна и имеет роль администратора.
func AdminAuth(users UserRepository, adminID domain.PublicID, adminName, password string, log *slog.Logger) func(http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(password))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name, password, ok := r.BasicAuth()
			actual := sha256.Sum256([]byte(password))
			matches := subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
			if !ok || name != adminName || !matches {
				w.Header().Set("WWW-Authenticate", `Basic realm="Somnium Shade Cast"`)
				respond.JSON(w, http.StatusUnauthorized, respond.ErrorBody{Error: "invalid credentials"})
				return
			}

			user, err := users.GetByPublicID(r.Context(), adminID)
			if errors.Is(err, domain.ErrNotFound) {
				respond.WriteError(w, log, domain.ErrForbidden)
				return
			}
			if err != nil {
				respond.WriteError(w, log, err)
				return
			}
			if !user.IsActive || user.Role != domain.RoleAdmin {
				respond.WriteError(w, log, domain.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}
