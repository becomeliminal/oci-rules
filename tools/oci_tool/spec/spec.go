// Package spec reads the JSON files the oci plugin's rules write for
// oci-tool: specs, locks and metadata.
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Decode reads the file at path, which must hold exactly one JSON document,
// into into. A field into does not know is refused, and so is anything after
// the document, a second document or any text but whitespace: a file the
// rule did not write as it should is an error, never half-read.
func Decode(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var rest json.RawMessage
	switch err := decoder.Decode(&rest); err {
	case io.EOF:
		return nil
	case nil:
		return fmt.Errorf("reading %s: more follows the JSON document", path)
	default:
		return fmt.Errorf("reading %s: after the JSON document: %w", path, err)
	}
}
