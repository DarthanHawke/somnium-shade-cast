package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.User, error)
	Deactivate(ctx context.Context, publicID domain.PublicID) error
	Activate(ctx context.Context, publicID domain.PublicID) error
}

type UserHandler struct {
	users UserRepository
}

func NewUserHandler(users UserRepository) *UserHandler {
	return &UserHandler{
		users: users,
	}
}

// Routes возвращает роутер с эндпоинтами пользователей
// @tag.name Users
// @tag.description Управление пользователями системы
func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateUser)
	r.Get("/{publicID}", h.GetUser)
	r.Post("/{publicID}/activate", h.ActivateUser)
	r.Post("/{publicID}/deactivate", h.DeactivateUser)

	return r
}

// CreateUser создаёт нового пользователя
// @Summary Создать пользователя
// @Description Создаёт нового пользователя с указанными данными
// @Tags Users
// @Accept json
// @Produce json
// @Param request body domain.CreateUserRequest true "Данные пользователя"
// @Success 201 {object} domain.GetUserResponse "Пользователь создан"
// @Failure 400 {object} domain.ErrorResponse "Неверный запрос"
// @Failure 409 {object} domain.ErrorResponse "Пользователь уже существует"
// @Failure 500 {object} domain.ErrorResponse "Внутренняя ошибка сервера"
// @Router /users [post]
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON", err.Error())
		return
	}

	user, err := domain.NewUser(req.Name, req.DisplayName, domain.RoleMember)
	if err != nil {
		if domain.IsInvalid(err) {
			writeError(w, http.StatusBadRequest, "validation failed", "VALIDATION_ERROR", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "failed to create user", "DOMAIN_ERROR", err.Error())
		return
	}

	if err := h.users.Create(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user", "DB_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// GetUser возвращает пользователя по публичному ID
// @Summary Получить пользователя
// @Description Возвращает информацию о пользователе по его ID
// @Tags Users
// @Produce json
// @Param publicID path string true "ID пользователя"
// @Success 200 {object} domain.GetUserResponse "Пользователь найден"
// @Failure 400 {object} domain.ErrorResponse "Неверный ID"
// @Failure 404 {object} domain.ErrorResponse "Пользователь не найден"
// @Failure 500 {object} domain.ErrorResponse "Внутренняя ошибка сервера"
// @Router /users/{publicID} [get]
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	publicID := domain.PublicID(chi.URLParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid public ID", "INVALID_ID", err.Error())
		return
	}

	user, err := h.users.GetByPublicID(r.Context(), publicID)
	if err != nil {
		if err == domain.ErrNotFound {
			writeError(w, http.StatusNotFound, "user not found", "NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get user", "DB_ERROR", err.Error())
		return
	}

	json.NewEncoder(w).Encode(domain.GetUserResponse{
		PublicID:    user.PublicID,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		IsActive:    user.IsActive,
		CreatedAt:   user.CreatedAt.UnixMilli(),
	})
}

// ActivateUser активирует пользователя
// @Summary Активировать пользователя
// @Description Активирует ранее деактивированного пользователя
// @Tags Users
// @Produce json
// @Param publicID path string true "ID пользователя"
// @Success 200 {object} domain.SuccessResponse "Пользователь активирован"
// @Failure 400 {object} domain.ErrorResponse "Неверный ID"
// @Failure 404 {object} domain.ErrorResponse "Пользователь не найден"
// @Failure 500 {object} domain.ErrorResponse "Внутренняя ошибка сервера"
// @Router /users/{publicID}/activate [post]
func (h *UserHandler) ActivateUser(w http.ResponseWriter, r *http.Request) {
	publicID := domain.PublicID(chi.URLParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid public ID", "INVALID_ID", err.Error())
		return
	}

	if err := h.users.Activate(r.Context(), publicID); err != nil {
		if err == domain.ErrNotFound {
			writeError(w, http.StatusNotFound, "user not found", "NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to activate user", "DB_ERROR", err.Error())
		return
	}

	json.NewEncoder(w).Encode(domain.SuccessResponse{Message: "user activated successfully"})
}

// DeactivateUser деактивирует пользователя
// @Summary Деактивировать пользователя
// @Description Деактивирует пользователя
// @Tags Users
// @Produce json
// @Param publicID path string true "ID пользователя"
// @Success 200 {object} domain.SuccessResponse "Пользователь деактивирован"
// @Failure 400 {object} domain.ErrorResponse "Неверный ID"
// @Failure 404 {object} domain.ErrorResponse "Пользователь не найден"
// @Failure 500 {object} domain.ErrorResponse "Внутренняя ошибка сервера"
// @Router /users/{publicID}/deactivate [post]
func (h *UserHandler) DeactivateUser(w http.ResponseWriter, r *http.Request) {
	publicID := domain.PublicID(chi.URLParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid public ID", "INVALID_ID", err.Error())
		return
	}

	if err := h.users.Deactivate(r.Context(), publicID); err != nil {
		if err == domain.ErrNotFound {
			writeError(w, http.StatusNotFound, "user not found", "NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to deactivate user", "DB_ERROR", err.Error())
		return
	}

	json.NewEncoder(w).Encode(domain.SuccessResponse{Message: "user deactivated successfully"})
}

func writeError(w http.ResponseWriter, status int, message, code, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(domain.ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	})
}
