package dto

import (
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// UserRequest - запрос на создание пользователя
type UserRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
}

// UserResponse - ответ с данными пользователя
type UserResponse struct {
	PublicID    string `json:"public_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	IsActive    bool   `json:"is_active"`
	CreatedAt   int64  `json:"created_at"`
}

// NewUserResponse - создает UserResponse из domain.User
func NewUserResponse(user *domain.User) UserResponse {
	return UserResponse{
		PublicID:    string(user.PublicID),
		Name:        user.Name,
		DisplayName: user.DisplayName,
		Role:        string(user.Role),
		IsActive:    user.IsActive,
		CreatedAt:   user.CreatedAt.UnixMilli(),
	}
}

const (
	Activate   string = "user activated successfully"
	Deactivate string = "user deactivated successfully"
)
