package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// pathParam возвращает URL-параметр из chi-роутера.
func pathParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

// queryInt читает целое число из параметра запроса name.
// Если параметр отсутствует, пуст или не разбирается как int, возвращает fallback.
func queryInt(r *http.Request, name string, fallback int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
