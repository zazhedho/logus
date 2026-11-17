package core

import (
	"encoding/json"
	"os"
	"strings"
)

// getEnv returns the trimmed value of key, or def when key is not set.
func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return def
}

// normalizeKey lower-cases and trims v.
func normalizeKey(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// interfaceString renders v as a string: nil is empty, strings and byte
// slices are used as-is, anything else is JSON-encoded.
func interfaceString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
