package spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type layerSpec struct {
	Format string   `json:"format"`
	Tags   []string `json:"tags,omitempty"`
}

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spec.json")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestDecode_ReadsOneDocumentWithTrailingWhitespace(t *testing.T) {
	// GIVEN a spec the rule wrote, with the newline a heredoc leaves
	path := write(t, "{\"format\":\"oci\"}\n")

	// WHEN it is read
	var got layerSpec
	err := Decode(path, &got)

	// THEN it is the document
	require.NoError(t, err)
	require.Equal(t, layerSpec{Format: "oci"}, got)
}

func TestDecode_RefusesWhatIsNotExactlyOneKnownDocument(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"a second document", `{"format":"oci"}{"format":"docker","tags":["r.example.com/a:b"]}`, "more follows the JSON document"},
		{"text after the document", `{"format":"oci"} trailing`, "after the JSON document"},
		{"a field it does not know", `{"format":"oci","platfrom":"linux/amd64"}`, `unknown field "platfrom"`},
		{"no document", ``, "EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a spec that is not exactly one known document
			path := write(t, tc.content)

			// WHEN it is read
			var got layerSpec
			err := Decode(path, &got)

			// THEN it is refused, naming the file and what is wrong
			require.Error(t, err)
			require.Contains(t, err.Error(), path)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
