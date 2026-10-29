package recommender

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// DecodeState unmarshals saved model state into v, a pointer to a struct.
// A missing or unknown field is an error, so state from another layout is
// refused instead of read with zero values.
func DecodeState(raw []byte, v any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	t := reflect.TypeOf(v).Elem()
	for i := range t.NumField() {
		if _, ok := fields[t.Field(i).Name]; !ok {
			return fmt.Errorf("state has no %s", t.Field(i).Name)
		}
	}
	if len(fields) != t.NumField() {
		return fmt.Errorf("state has %d fields, want %d", len(fields), t.NumField())
	}
	return json.Unmarshal(raw, v)
}

// NonNegative reports whether no value is below zero.
func NonNegative(xs ...float64) bool {
	for _, x := range xs {
		if x < 0 {
			return false
		}
	}
	return true
}
