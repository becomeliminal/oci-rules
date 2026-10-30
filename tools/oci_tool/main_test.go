package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
)

func TestTreeEntries_PlacesEveryFileAtItsPathInThePackage(t *testing.T) {
	// GIVEN a tree whose source group holds two files of the package docs,
	// one a level down
	t.Setenv("SRCS_T0", "docs/a.md docs/sub/b.md")
	tree := ruleEntry{Path: "/data", Kind: kindTree, Source: "t0", Strip: "docs", UID: 7, GID: 8}

	// WHEN it is expanded
	entries, err := treeEntries(tree)

	// THEN each file is under /data at its path in the package, 0644, owned
	// as the tree is
	require.NoError(t, err)
	mode := int64(0o644)
	require.Equal(t, []layer.Entry{
		{Path: "/data/a.md", Kind: layer.KindFile, Source: "docs/a.md", Mode: &mode, UID: 7, GID: 8},
		{Path: "/data/sub/b.md", Kind: layer.KindFile, Source: "docs/sub/b.md", Mode: &mode, UID: 7, GID: 8},
	}, entries)
}

func TestTreeEntries_RefusesWhatItCannotPlace(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group string
		strip string
		want  string
	}{
		{"a file outside the package", "docs/a.md other/b.md", "docs", "other/b.md is not in docs"},
		{"a package that is only a prefix of the file's", "docsextra/a.md", "docs", "docsextra/a.md is not in docs"},
		{"no package to place relative to", "docs/a.md", "", "names no package"},
		{"no files", "", "docs", "holds no files"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN the tree's source group
			t.Setenv("SRCS_T0", tc.group)

			// WHEN it is expanded
			_, err := treeEntries(ruleEntry{Path: "/data", Kind: kindTree, Source: "t0", Strip: tc.strip})

			// THEN it is refused, saying why
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
