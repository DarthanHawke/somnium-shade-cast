// Package middleware предоставляет промежуточные обработчики HTTP-запросов.
package middleware

import (
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
)

// Limit ограничивает число выполняющихся и ожидающих запросов.
// Если все слоты выполнения заняты и в очереди нет места,
// отвечает 503 Service Unavailable с заголовком Retry-After.
func Limit(maxConcurrent, maxWaiting int) func(http.Handler) http.Handler {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	if maxWaiting < 0 {
		maxWaiting = 0
	}

	sem := make(chan struct{}, maxConcurrent)
	waiting := make(chan struct{}, maxWaiting)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if ctx.Err() != nil {
				return
			}

			// Сначала пробуем начать выполнение без ожидания.
			select {
			case sem <- struct{}{}:
			default:
				// Все исполнители заняты: пробуем встать в очередь.
				select {
				case waiting <- struct{}{}:
				default:
					w.Header().Set("Retry-After", "5")
					respond.JSON(w, http.StatusServiceUnavailable, respond.ErrorBody{
						Error: "too many requests, retry later",
					})
					return
				}

				select {
				case sem <- struct{}{}:
					// Ожидание закончилось, место в очереди свободно.
					<-waiting

				case <-ctx.Done():
					<-waiting
					return
				}
			}

			defer func() { <-sem }()

			// При одновременной отмене и освобождении слота
			// select мог выбрать получение слота.
			if ctx.Err() != nil {
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
