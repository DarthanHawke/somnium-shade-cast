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

// NewUser создаёт активного пользователя. Если displayName пусто, использует name.
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
