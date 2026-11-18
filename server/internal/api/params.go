package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// pathParam возвращает URL-параметр из chi-роутера.
func pathParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

// parsePagination читает limit и offset. Пропущенные параметры получают значения по умолчанию.
// Пустые, повторяющиеся, нечисловые и выходящие за границы значения отклоняются.
func parsePagination(r *http.Request) (limit, offset int, err error) {
	query := r.URL.Query()
	limit = defaultLimit
	if values, present := query["limit"]; present {
		if len(values) != 1 {
			return 0, 0, domain.Invalid("limit", "expected one value")
		}
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > maxLimit {
			return 0, 0, domain.Invalid("limit", "expected an integer between 1 and 200")
		}
	}
	if values, present := query["offset"]; present {
		if len(values) != 1 {
			return 0, 0, domain.Invalid("offset", "expected one value")
		}
		offset, err = strconv.Atoi(values[0])
		if err != nil || offset < 0 {
			return 0, 0, domain.Invalid("offset", "expected a nonnegative integer")
		}
	}
	return limit, offset, nil
}
