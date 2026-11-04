package source

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// FlexInt decodes numbers that APIs sometimes send as strings, null or floats.
type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	if n, err := strconv.Atoi(string(b)); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var fl float64
	if err := json.Unmarshal(b, &fl); err == nil {
		*f = FlexInt(int(fl))
		return nil
	}
	*f = 0
	return nil
}

// FlexString decodes strings that may arrive as numbers or null.
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(bytes.Trim(b, `"`))
	return nil
}
