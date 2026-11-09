package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// UserRepository — интерфейс для доступа к данным пользователей.
type UserRepository interface {
	// Insert сохраняет пользователя и при успехе заполняет user.ID.
	// При нарушении уникальности возвращает domain.ErrConflict.
	Insert(ctx context.Context, user *domain.User) error

	// GetByPublicID возвращает пользователя по публичному идентификатору domain.PublicID.
	// Если пользователь отсутствует, возвращает domain.ErrNotFound.
	GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.User, error)

	// Deactivate помечает пользователя с publicID как неактивного, сохраняя его запись.
	// Если пользователь отсутствует, возвращает domain.ErrNotFound.
	Deactivate(ctx context.Context, publicID domain.PublicID) error

	// Activate помечает пользователя с publicID как активного.
	// Если пользователь отсутствует, возвращает domain.ErrNotFound.
	Activate(ctx context.Context, publicID domain.PublicID) error
}

// UserHandler — HTTP-обработчик для работы с пользователями.
type UserHandler struct {
	users UserRepository
	log   *slog.Logger
}

// NewUserHandler создает новый UserHandler.
func NewUserHandler(users UserRepository, log *slog.Logger) *UserHandler {
	return &UserHandler{
		users: users,
		log:   log,
	}
}

// CreateUser создаёт пользователя с ролью member.
//
// @Summary Создать пользователя
// @Description Создаёт нового пользователя с указанными данными
// @Tags Users
// @Accept json
// @Produce json
// @Param request body dto.UserRequest true "Данные пользователя"
// @Success 201 {object} dto.UserResponse "Пользователь создан"
// @Failure 400 {object} respond.ErrorBody "Невалидный JSON или поля"
// @Failure 409 {object} respond.ErrorBody "Пользователь уже существует"
// @Failure 500 {object} respond.ErrorBody "Внутренняя ошибка"
// @Security DevAuth
// @Router /users [post]
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req *dto.UserRequest
	if err := dec.Decode(&req); err != nil {
		respond.WriteError(w, h.log, domain.Invalid("body", "invalid JSON"))
		return
	}
	if req == nil {
		respond.WriteError(w, h.log, domain.Invalid("body", "missing request body"))
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		respond.WriteError(w, h.log, domain.Invalid("body", "expected a single JSON object"))
		return
	}

	user, err := domain.NewUser(req.Name, req.DisplayName, domain.RoleMember)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	if err := h.users.Insert(r.Context(), user); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	respond.JSON(w, http.StatusCreated, dto.NewUserResponse(user))
}

// GetUser возвращает данные пользователя по публичному идентификатору.
//
// @Summary Получить пользователя
// @Description Возвращает информацию о пользователе по его ID
// @Tags Users
// @Produce json
// @Param publicID path string true "ID пользователя"
// @Success 200 {object} dto.UserResponse "Пользователь найден"
// @Failure 400 {object} respond.ErrorBody "Неверный ID"
// @Failure 404 {object} respond.ErrorBody "Пользователь не найден"
// @Failure 500 {object} respond.ErrorBody "Внутренняя ошибка сервера"
// @Security DevAuth
// @Router /users/{publicID} [get]
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	publicID := domain.PublicID(pathParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	user, err := h.users.GetByPublicID(r.Context(), publicID)
	if err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	respond.JSON(w, http.StatusOK, dto.NewUserResponse(user))

}

// ActivateUser помечает пользователя активным.
//
// @Summary Активировать пользователя
// @Tags Users
// @Produce json
// @Param publicID path string true "ID пользователя"
// @Success 200 {object} dto.SuccessResponse "Пользователь активирован"
// @Failure 400 {object} respond.ErrorBody "Неверный ID"
// @Failure 404 {object} respond.ErrorBody "Пользователь не найден"
// @Failure 500 {object} respond.ErrorBody "Внутренняя ошибка сервера"
// @Security DevAuth
// @Router /users/{publicID}/activate [post]
func (h *UserHandler) ActivateUser(w http.ResponseWriter, r *http.Request) {
	publicID := domain.PublicID(pathParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	if err := h.users.Activate(r.Context(), publicID); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.SuccessResponse{Message: dto.Activate})

}

// DeactivateUser помечает пользователя неактивным.
//
// @Summary Деактивировать пользователя
// @Tags Users
// @Produce json
// @Param publicID path string true "ID пользователя"
// @Success 200 {object} dto.SuccessResponse "Пользователь деактивирован"
// @Failure 400 {object} respond.ErrorBody "Неверный ID"
// @Failure 404 {object} respond.ErrorBody "Пользователь не найден"
// @Failure 500 {object} respond.ErrorBody "Внутренняя ошибка сервера"
// @Security DevAuth
// @Router /users/{publicID}/deactivate [post]
func (h *UserHandler) DeactivateUser(w http.ResponseWriter, r *http.Request) {
	publicID := domain.PublicID(pathParam(r, "publicID"))
	if err := publicID.Validate(); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	if err := h.users.Deactivate(r.Context(), publicID); err != nil {
		respond.WriteError(w, h.log, err)
		return
	}

	respond.JSON(w, http.StatusOK, dto.SuccessResponse{Message: dto.Deactivate})
}
