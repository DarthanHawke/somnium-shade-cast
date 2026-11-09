package middleware

import (
	"context"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

type ctxKey struct{}

// WithUser — возвращает новый контекст с пользователем.
func WithUser(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom — извлекает пользователя из контекста.
func UserFrom(ctx context.Context) (*domain.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*domain.User)
	return u, ok && u != nil
}
