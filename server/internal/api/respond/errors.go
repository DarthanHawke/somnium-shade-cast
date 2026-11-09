// Package respond формирует HTTP-ответы и сопоставляет ошибки со статусами.
package respond

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

var (
	// ErrUnauthorized — не авторизован
	ErrUnauthorized = errors.New("unauthorized")
)

type ErrorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

// WriteError отправляет JSON с публичным сообщением об ошибке.
// Для ошибки валидации добавляет имя поля, если оно задано.
// Для ответов 5xx записывает исходную ошибку в лог.
func WriteError(w http.ResponseWriter, log *slog.Logger, err error) {
	// ValidationError - структурированная, отдаём поле
	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		JSON(w, http.StatusBadRequest, ErrorBody{Error: ve.Message, Field: ve.Field})
		return
	}

	status, msg := mapStatus(err)

	if status >= http.StatusInternalServerError {
		log.Error("request failed", "status", status, "err", err)
	}

	JSON(w, status, ErrorBody{Error: msg})
}

// mapStatus возвращает HTTP-статус и публичное сообщение для ошибки.
func mapStatus(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrInvalidMasterKey):
		return http.StatusInternalServerError, "internal error"
	case errors.Is(err, domain.ErrIncomplete):
		return http.StatusBadRequest, "incomplete upload"
	case errors.Is(err, prober.ErrNotAudio):
		return http.StatusBadRequest, "file is not audio"
	case errors.Is(err, prober.ErrInvalidFile):
		return http.StatusBadRequest, "invalid audio file"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "already exists"
	case errors.Is(err, domain.ErrBlobInUse):
		return http.StatusConflict, "resource in use"
	case errors.Is(err, domain.ErrAlreadyOrphan):
		return http.StatusConflict, "conflict"
	case errors.Is(err, domain.ErrBlobFileMissing):
		return http.StatusGone, "content unavailable"
	case errors.Is(err, domain.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "payload too large"
	case errors.Is(err, domain.ErrRangeNotSatisfiable):
		return http.StatusRequestedRangeNotSatisfiable, "range not satisfiable"
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
