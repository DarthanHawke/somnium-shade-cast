package domain

import (
	"database/sql"
	"database/sql/driver"
	"time"
)

type UnixTimeMillis time.Time

func NewUnixTimeMillis(millis int64) UnixTimeMillis {
	return UnixTimeMillis(time.Unix(millis/1000, (millis%1000)*int64(time.Millisecond)))
}

func (ut *UnixTimeMillis) Scan(value any) error {
	var v sql.NullInt64
	if err := v.Scan(value); err != nil {
		return err
	}

	*ut = UnixTimeMillis(time.Unix(v.Int64/1000, (v.Int64%1000)*int64(time.Millisecond)))
	return nil
}

func (ut UnixTimeMillis) Value() (driver.Value, error) {
	return int64(time.Time(ut).Unix() * 1000), nil
}

func (ut UnixTimeMillis) UnixMilli() int64 {
	return time.Time(ut).Unix() * 1000
}
