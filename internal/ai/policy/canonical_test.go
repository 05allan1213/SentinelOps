package policy

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestCanonicalJSONStableMapOrderNumbersTimesAndEmptyValues(t *testing.T) {
	instant := time.Date(2026, time.August, 24, 8, 9, 10, 123000000, time.FixedZone("CST", 8*60*60))
	first := map[string]any{
		"time":         instant,
		"number":       json.Number("1.0"),
		"nil":          (*string)(nil),
		"empty_object": map[string]any{},
		"empty_array":  []any{},
	}
	second := map[string]any{
		"empty_array":  []any{},
		"empty_object": map[string]any{},
		"nil":          nil,
		"number":       json.Number("1e0"),
		"time":         instant.UTC(),
	}

	want := `{"empty_array":[],"empty_object":{},"nil":null,"number":1,"time":"2026-08-24T00:09:10.123Z"}`
	for name, input := range map[string]any{"first": first, "second": second} {
		t.Run(name, func(t *testing.T) {
			got, err := CanonicalJSON(input)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != want {
				t.Fatalf("CanonicalJSON() = %s, want %s", got, want)
			}
		})
	}
}

func TestCanonicalJSONRejectsUnsupportedValues(t *testing.T) {
	for name, input := range map[string]any{
		"positive infinity": math.Inf(1),
		"not a number":      math.NaN(),
		"non-string key":    map[int]string{1: "value"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CanonicalJSON(input); err == nil {
				t.Fatal("CanonicalJSON() succeeded, want fail-closed error")
			}
		})
	}
}

func FuzzCanonicalJSONStable(f *testing.F) {
	for _, seed := range []string{
		`null`,
		`{"b":1.0,"a":[1e0,null,"2026-08-24T08:00:00+08:00"]}`,
		`{"nested":{"z":-0,"empty":{}}}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Skip()
		}
		first, err := CanonicalJSON(value)
		if err != nil {
			t.Skip()
		}
		second, err := CanonicalJSON(json.RawMessage(first))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatalf("canonicalization is not idempotent: first=%s second=%s", first, second)
		}
	})
}
