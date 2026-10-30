package layer

import (
	"archive/tar"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// longDir is a directory path past the 100 bytes a plain tar header holds.
var longDir = "/" + strings.Repeat("deep/", 22) + "end"

// edgeSpec is a layer of every unusual entry a layer can hold: a whiteout,
// an opaque directory with a file in it, an absolute and a relative
// symlink, a path too long for a plain tar header, and an empty file.
func edgeSpec(t *testing.T) Spec {
	t.Helper()
	return Spec{Entries: []Entry{
		{Path: "/old", Kind: KindWhiteout},
		{Path: "/opt/app", Kind: KindOpaque},
		{Path: "/opt/app/run", Kind: KindFile, Source: writeInput(t, "run\n", 0o644, time.Unix(7, 0))},
		{Path: "/usr/bin/tool", Kind: KindSymlink, Target: "/opt/app/run"},
		{Path: "/usr/bin/again", Kind: KindSymlink, Target: "tool"},
		{Path: longDir + "/file", Kind: KindFile, Source: writeInput(t, "deep\n", 0o644, time.Unix(8, 0))},
		{Path: "/etc/empty", Kind: KindFile, Source: writeInput(t, "", 0o644, time.Unix(9, 0))},
	}}
}

func TestWrite_EveryUnusualEntryIsWrittenAsTheOCISpecSays(t *testing.T) {
	// GIVEN a layer of every unusual entry
	spec := edgeSpec(t)

	// WHEN it is written
	blob, _ := build(t, spec)

	// THEN the whiteout and opaque directory are their empty marker files,
	// the symlinks keep their targets as given, the long path is whole, and
	// the empty file is a regular file of no bytes
	byName := map[string]header{}
	for _, h := range headers(t, blob) {
		byName[h.Name] = h
	}
	require.Equal(t, byte(tar.TypeReg), byName[".wh.old"].Typeflag)
	require.Zero(t, byName[".wh.old"].Size)
	require.Equal(t, byte(tar.TypeReg), byName["opt/app/.wh..wh..opq"].Typeflag)
	require.Zero(t, byName["opt/app/.wh..wh..opq"].Size)
	require.Equal(t, byte(tar.TypeDir), byName["opt/app/"].Typeflag)
	require.Equal(t, "/opt/app/run", byName["usr/bin/tool"].Linkname)
	require.Equal(t, "tool", byName["usr/bin/again"].Linkname)
	require.Equal(t, byte(tar.TypeReg), byName[strings.TrimPrefix(longDir, "/")+"/file"].Typeflag)
	require.Equal(t, byte(tar.TypeReg), byName["etc/empty"].Typeflag)
	require.Zero(t, byName["etc/empty"].Size)
	_, kept := byName["old"]
	require.False(t, kept, "a whiteout writes only its marker")
}

func TestWrite_EdgeCaseGoldenDigests(t *testing.T) {
	// GIVEN the layer of every unusual entry
	spec := edgeSpec(t)

	// WHEN it is built
	_, meta := build(t, spec)

	// THEN its identity is exactly what it has always been
	require.Equal(t, Meta{
		SchemaVersion: 1,
		MediaType:     MediaType,
		Digest:        "sha256:d3d4c22c3d6ffd504fcbdf0fa6ee41ce06fffb6f1a8db7d4e515468fd80d8c8f",
		Size:          624,
		DiffID:        "sha256:f12883a94080541a89fe6276fd98c305f318d9306b8871cb86371d311c4929f0",
	}, meta)
}

func TestWrite_RefusesWhiteoutsAndOpaqueDirectoriesThatAreAmbiguous(t *testing.T) {
	src := writeInput(t, "x", 0o644, time.Unix(0, 0))
	for _, tc := range []struct {
		name    string
		entries []Entry
		want    string
	}{
		{"a whiteout with this layer's file inside it",
			[]Entry{{Path: "/a", Kind: KindWhiteout}, {Path: "/a/b", Kind: KindFile, Source: src}},
			"/a is whited out, but this layer adds /a/b inside it"},
		{"a path both added and whited out",
			[]Entry{{Path: "/a", Kind: KindWhiteout}, {Path: "/a", Kind: KindFile, Source: src}},
			"/a is declared twice"},
		{"a declared name that reads as a whiteout",
			[]Entry{{Path: "/etc/.wh.passwd", Kind: KindFile, Source: src}},
			"a name starting with \".wh.\" is a whiteout to any reader"},
		{"a whiteout with a source",
			[]Entry{{Path: "/a", Kind: KindWhiteout, Source: src}},
			"/a is a whiteout and has no source, target, mode or owner"},
		{"an opaque directory with an owner",
			[]Entry{{Path: "/a", Kind: KindOpaque, UID: 5}},
			"/a is a opaque and has no source, target, mode or owner"},
		{"an opaque directory inside a file",
			[]Entry{{Path: "/a", Kind: KindFile, Source: src}, {Path: "/a/b", Kind: KindOpaque}},
			"/a is a file but /a/b/.wh..wh..opq is inside it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN the layer is written
			_, err := Write(Spec{Entries: tc.entries}, &strings.Builder{})

			// THEN it is refused, saying how
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
