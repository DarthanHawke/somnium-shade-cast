package domain

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// UnixTimeMillis представляет момент времени.
// В SQL хранится целым числом миллисекунд с начала эпохи Unix.
type UnixTimeMillis time.Time

func NewUnixTimeMillis(millis int64) UnixTimeMillis {
	return UnixTimeMillis(time.UnixMilli(millis).UTC())
}

func (ut *UnixTimeMillis) Scan(value any) error {
	var v sql.NullInt64
	if err := v.Scan(value); err != nil {
		return err
	}

	if !v.Valid {
		return fmt.Errorf("UnixTimeMillis: unexpected SQL NULL")
	}

	*ut = NewUnixTimeMillis(v.Int64)
	return nil
}

func (ut UnixTimeMillis) Value() (driver.Value, error) {
	return ut.UnixMilli(), nil
}

func (ut UnixTimeMillis) UnixMilli() int64 {
	return time.Time(ut).UnixMilli()
}
