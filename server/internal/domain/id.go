package domain

import (
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"time"

	"github.com/oklog/ulid/v2"
)

// ID — внутренний идентификатор
type ID int64

// NewID создаёт новый ID из int64
func NewID(id int64) ID {
	return ID(id)
}

func (id *ID) Scan(value any) error {
	var v sql.NullInt64
	if err := v.Scan(value); err != nil {
		return err
	}
	*id = ID(v.Int64)
	return nil
}

func (id ID) Value() (driver.Value, error) {
	return int64(id), nil
}

// PublicID — внешний идентификатор (ULID)
type PublicID string

func NewPublicID() PublicID {
	return PublicID(ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String())
}

func (id *PublicID) Scan(value any) error {
	var v sql.NullString
	if err := v.Scan(value); err != nil {
		return err
	}
	*id = PublicID(v.String)
	return nil
}

func (id PublicID) Value() (driver.Value, error) {
	return string(id), nil
}

func (p PublicID) Validate() error {
	if _, err := ulid.ParseStrict(string(p)); err != nil {
		return Invalid("public_id", "malformed id")
	}
	return nil
}
