package inspect

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
)

// file writes content to a fresh input file.
func file(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "input")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// imageOf builds an image from layers, bottom first, on disk as a build
// leaves it.
func imageOf(t *testing.T, specs ...layer.Spec) publish.Image {
	t.Helper()
	dir := t.TempDir()
	var metas []image.LayerMeta
	var blobs []string
	for i, spec := range specs {
		var blob bytes.Buffer
		meta, err := layer.Write(spec, &blob)
		require.NoError(t, err)
		p := filepath.Join(dir, "layer"+string(rune('0'+i))+".tar.gz")
		require.NoError(t, os.WriteFile(p, blob.Bytes(), 0o644))
		blobs = append(blobs, p)
		metas = append(metas, image.LayerMeta{SchemaVersion: 1, MediaType: meta.MediaType, Digest: meta.Digest, Size: meta.Size, DiffID: meta.DiffID})
	}
	built, err := image.Build(nil, metas, image.Spec{Platform: "linux/amd64"})
	require.NoError(t, err)
	manifest := filepath.Join(dir, "manifest.json")
	config := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(manifest, built.Manifest, 0o644))
	require.NoError(t, os.WriteFile(config, built.Config, 0o644))
	loaded, err := publish.Load(manifest, config, blobs)
	require.NoError(t, err)
	return loaded
}

func TestFlatten_AppliesWhiteoutsAndOpaqueDirectoriesFromTheLayersAbove(t *testing.T) {
	// GIVEN a base layer with files under /a, /b and /c, and a layer above
	// that deletes /b, hides /a's contents while adding /a/new, and replaces
	// /c/file with one owned 7:7
	base := layer.Spec{Entries: []layer.Entry{
		{Path: "/a/x", Kind: layer.KindFile, Source: file(t, "x")},
		{Path: "/a/y", Kind: layer.KindFile, Source: file(t, "y")},
		{Path: "/b/z", Kind: layer.KindFile, Source: file(t, "z")},
		{Path: "/c/file", Kind: layer.KindFile, Source: file(t, "old")},
	}}
	upper := layer.Spec{Entries: []layer.Entry{
		{Path: "/b", Kind: layer.KindWhiteout},
		{Path: "/a", Kind: layer.KindOpaque},
		{Path: "/a/new", Kind: layer.KindFile, Source: file(t, "new")},
		{Path: "/c/file", Kind: layer.KindFile, Source: file(t, "replaced"), UID: 7, GID: 7},
	}}

	// WHEN the image's filesystem is flattened
	fs, err := Flatten(imageOf(t, base, upper))

	// THEN /b and /a's old files are gone, /a/new is there, and /c/file is
	// the upper layer's, with its owner; no marker file survives
	require.NoError(t, err)
	paths := map[string]bool{}
	for p := range fs {
		paths[p] = true
	}
	require.Equal(t, map[string]bool{"/a": true, "/a/new": true, "/c": true, "/c/file": true}, paths)
	require.Equal(t, Node{Typeflag: tar.TypeReg, Mode: 0o644, UID: 7, GID: 7, Size: int64(len("replaced"))}, fs["/c/file"])
}

func TestCheckOwners_NamesEveryPathMissingOrOwnedBySomeoneElse(t *testing.T) {
	// GIVEN a filesystem with /app owned 0:0 and /data owned 65534:65534
	fs := map[string]Node{
		"/app":  {Typeflag: tar.TypeReg, UID: 0, GID: 0},
		"/data": {Typeflag: tar.TypeDir, UID: 65534, GID: 65534},
	}

	// WHEN it is checked against owners for three paths
	problems := CheckOwners(fs, map[string]Owner{
		"/app":     {UID: 0, GID: 0},
		"/data":    {UID: 0, GID: 0},
		"/missing": {UID: 0, GID: 0},
	})

	// THEN the two that are not as declared are named, in path order
	require.Equal(t, []string{
		"/data is owned 65534:65534, want 0:0",
		"/missing is not in the image",
	}, problems)
}
