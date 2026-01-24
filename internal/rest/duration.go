package rest

import (
	"encoding/json"
	"time"
)

// Duration is a custom type that unmarshals from JSON duration strings like "5s", "10m", "1h"
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler to parse Go duration strings
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}
