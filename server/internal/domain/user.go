package domain

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r *Role) Scan(value any) error {
	var v sql.NullString
	if err := v.Scan(value); err != nil {
		return err
	}
	*r = Role(v.String)
	return nil
}

func (r Role) Value() (driver.Value, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("invalid role: %s", r)
	}
	return string(r), nil
}

func (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }

type User struct {
	ID           ID             `db:"id"`
	PublicID     PublicID       `db:"public_id"`
	Name         string         `db:"name"`
	DisplayName  string         `db:"display_name"`
	Role         Role           `db:"role"`
	AvatarBlobID *ID            `db:"avatar_blob_id"`
	IsActive     bool           `db:"is_active"`
	CreatedAt    UnixTimeMillis `db:"created_at"`
}

func NewUser(name, displayName string, role Role) (*User, error) {
	if name == "" {
		return nil, Invalid("name", "required")
	}
	if !role.Valid() {
		return nil, Invalid("role", "unknown role")
	}
	if displayName == "" {
		displayName = name
	}
	return &User{
		PublicID:    NewPublicID(),
		Name:        name,
		DisplayName: displayName,
		Role:        role,
		IsActive:    true,
		CreatedAt:   UnixTimeMillis(time.Now()),
	}, nil
}

// CreateUserRequest - запрос на создание пользователя
type CreateUserRequest struct {
	Name        string `json:"name" example:"example"`
	DisplayName string `json:"display_name,omitempty" example:"Example Example"`
}

// GetUserResponse - ответ с данными пользователя
type GetUserResponse struct {
	PublicID    PublicID `json:"public_id" example:"01H2XJZ9K8F7D6E5C4B3A2Z1Y"`
	Name        string   `json:"name" example:"johndoe"`
	DisplayName string   `json:"display_name" example:"John Doe"`
	Role        Role     `json:"role" example:"member"`
	IsActive    bool     `json:"is_active" example:"true"`
	CreatedAt   int64    `json:"created_at" example:"1735689600000"`
}
