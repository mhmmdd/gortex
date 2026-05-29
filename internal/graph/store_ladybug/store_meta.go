package store_ladybug

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
)

// encodeMeta serialises a Meta map to a base64-encoded gob frame.
// Empty / nil maps become the empty string so the common case stays
// cheap to store. base64 is required because the Go binding reads
// BLOB columns through strlen(), which would truncate at the first
// NUL byte that gob encoding routinely emits.
func encodeMeta(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(m); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// decodeMeta is the inverse of encodeMeta.
func decodeMeta(s string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
