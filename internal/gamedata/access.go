package gamedata

import (
	"fmt"
	"strconv"
	"strings"
)

// asMap returns v as a map, or nil.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// asList returns v as a slice. Odin List/array nodes are decoded as a map with
// the payload under "$items"; asList transparently unwraps that.
func asList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		if items, ok := t["$items"]; ok {
			return asList(items)
		}
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// asInt coerces a decoded numeric (int64/uint64/float64/int/string) to int.
func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case uint64:
		return int(t)
	case float64:
		return int(t)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(t))
		return i
	}
	return 0
}

// asFloat coerces a decoded numeric to float64.
func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	}
	return 0
}

// firstField returns the value of the first present key (case-sensitive first,
// then a case-insensitive fallback).
func firstField(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	for _, k := range keys {
		for mk, mv := range m {
			if strings.EqualFold(mk, k) {
				return mv
			}
		}
	}
	return nil
}

// mdEsc escapes a markdown table cell.
func mdEsc(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}
