package layer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeInput writes content to a fresh file with the given permissions and
// modification time, so a test can vary everything a layer must ignore.
func writeInput(t *testing.T, content string, perm os.FileMode, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "input")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	require.NoError(t, os.Chmod(p, perm))
	require.NoError(t, os.Chtimes(p, mtime, mtime))
	return p
}

func mode(m int64) *int64 { return &m }

// build writes the layer for spec and returns its bytes and metadata.
func build(t *testing.T, spec Spec) ([]byte, Meta) {
	t.Helper()
	var out bytes.Buffer
	meta, err := Write(spec, &out)
	require.NoError(t, err)
	return out.Bytes(), meta
}

type header struct {
	Name     string
	Typeflag byte
	Mode     int64
	UID, GID int
	Size     int64
	Linkname string
	ModTime  int64
	Uname    string
	Gname    string
}

// headers decompresses a layer and lists its tar headers in order.
func headers(t *testing.T, blob []byte) []header {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	require.NoError(t, err)
	tr := tar.NewReader(zr)
	var out []header
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		require.NoError(t, err)
		out = append(out, header{h.Name, h.Typeflag, h.Mode, h.Uid, h.Gid, h.Size, h.Linkname, h.ModTime.Unix(), h.Uname, h.Gname})
	}
}

func TestWrite_TheSameSpecAndBytesGiveTheSameLayerWhateverTheHostDid(t *testing.T) {
	// GIVEN the same content written twice, with different permissions and
	// modification times, as two checkouts or two machines would leave it
	first := writeInput(t, "config", 0o644, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	second := writeInput(t, "config", 0o600, time.Date(1999, 9, 9, 9, 9, 9, 0, time.UTC))

	// WHEN a layer is built from each
	blobA, metaA := build(t, Spec{Entries: []Entry{{Path: "/etc/app/config", Kind: KindFile, Source: first}}})
	blobB, metaB := build(t, Spec{Entries: []Entry{{Path: "/etc/app/config", Kind: KindFile, Source: second}}})

	// THEN the bytes and the metadata are identical
	require.Equal(t, blobA, blobB)
	require.Equal(t, metaA, metaB)
}

func TestWrite_AnExecutableKeepsItsDeclaredModeWhateverTheInputSays(t *testing.T) {
	// GIVEN one binary as a local build leaves it (0755) and as a remote
	// executor does (0555)
	local := writeInput(t, "\x7fELF binary", 0o755, time.Unix(1, 0))
	remote := writeInput(t, "\x7fELF binary", 0o555, time.Unix(2, 0))

	// WHEN each is placed with mode 0755 declared
	blobLocal, _ := build(t, Spec{Entries: []Entry{{Path: "/app", Kind: KindFile, Source: local, Mode: mode(0o755)}}})
	blobRemote, _ := build(t, Spec{Entries: []Entry{{Path: "/app", Kind: KindFile, Source: remote, Mode: mode(0o755)}}})

	// THEN the layers are identical
	require.Equal(t, blobLocal, blobRemote)
}

func TestWrite_TheTarListsParentsFirstWithFixedOwnersModesAndTimes(t *testing.T) {
	// GIVEN a file deep in the tree, a declared directory, a symlink and an
	// owner override, declared out of order
	src := writeInput(t, "hello", 0o644, time.Unix(1234, 0))
	spec := Spec{Entries: []Entry{
		{Path: "/usr/share/app/hello.txt", Kind: KindFile, Source: src, UID: 65532, GID: 65532},
		{Path: "/usr/bin/app", Kind: KindSymlink, Target: "/usr/share/app/hello.txt"},
		{Path: "/var/cache", Kind: KindDir, Mode: mode(0o700)},
	}}

	// WHEN the layer is built
	blob, _ := build(t, spec)

	// THEN every parent precedes its children, missing parents are 0755
	// root-owned directories, and no time, user name or host mode leaks in
	require.Equal(t, []header{
		{Name: "usr/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "usr/bin/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "usr/bin/app", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "/usr/share/app/hello.txt"},
		{Name: "usr/share/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "usr/share/app/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "usr/share/app/hello.txt", Typeflag: tar.TypeReg, Mode: 0o644, UID: 65532, GID: 65532, Size: 5},
		{Name: "var/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "var/cache/", Typeflag: tar.TypeDir, Mode: 0o700},
	}, headers(t, blob))
}

func TestWrite_GoldenDigests(t *testing.T) {
	// GIVEN a fixed spec over fixed bytes
	src := writeInput(t, "hello, layer\n", 0o644, time.Unix(42, 0))
	bin := writeInput(t, "#!/bin/sh\necho hi\n", 0o755, time.Unix(43, 0))

	// WHEN its layer is built
	_, meta := build(t, Spec{Entries: []Entry{
		{Path: "/etc/greeting", Kind: KindFile, Source: src},
		{Path: "/bin/hi", Kind: KindFile, Source: bin, Mode: mode(0o755)},
	}})

	// THEN its identity is exactly what it has always been. A change here
	// changes every image built over this layer: it must be deliberate (a
	// spec change, or a Go upgrade that changed compress/gzip's output).
	require.Equal(t, Meta{
		SchemaVersion: 1,
		MediaType:     MediaType,
		Digest:        "sha256:ec2274edff749988cd992f1912ff9a381551dfe30859bc76b8b714d16d3f1f85",
		Size:          198,
		DiffID:        "sha256:3ea13ecd62cff7c8c7c0a65a30b809c8a274a95e271312b227cd198e33630e39",
	}, meta)
}

func TestWrite_RefusesASpecThatDoesNotNameExactlyOnePlace(t *testing.T) {
	file := writeInput(t, "x", 0o644, time.Unix(0, 0))
	exe := writeInput(t, "x", 0o755, time.Unix(0, 0))
	for _, tc := range []struct {
		name    string
		entries []Entry
		want    string
	}{
		{"no entries", nil, "at least one entry"},
		{"a relative path", []Entry{{Path: "etc/x", Kind: KindFile, Source: file}}, "must be an absolute image path"},
		{"the root", []Entry{{Path: "/", Kind: KindDir}}, "is the image root"},
		{"a .. component", []Entry{{Path: "/etc/../x", Kind: KindFile, Source: file}}, "must not contain an empty, . or .. component"},
		{"a doubled slash", []Entry{{Path: "/etc//x", Kind: KindFile, Source: file}}, "must not contain an empty, . or .. component"},
		{"a NUL", []Entry{{Path: "/etc/x\x00", Kind: KindFile, Source: file}}, "contains a NUL"},
		{"a duplicate", []Entry{{Path: "/x", Kind: KindFile, Source: file}, {Path: "/x", Kind: KindDir}}, "/x is declared twice"},
		{"a file used as a directory", []Entry{{Path: "/x", Kind: KindFile, Source: file}, {Path: "/x/y", Kind: KindFile, Source: file}}, "/x is a file but /x/y is inside it"},
		{"a setuid bit", []Entry{{Path: "/x", Kind: KindFile, Source: file, Mode: mode(0o4755)}}, "only permission bits"},
		{"a symlink with a mode", []Entry{{Path: "/x", Kind: KindSymlink, Target: "/y", Mode: mode(0o755)}}, "is a symlink and cannot have a source or mode"},
		{"a negative owner", []Entry{{Path: "/x", Kind: KindFile, Source: file, UID: -1}}, "negative owner"},
		{"an unknown kind", []Entry{{Path: "/x", Kind: "device"}}, `has kind "device"`},
		{"an executable without a mode", []Entry{{Path: "/x", Kind: KindFile, Source: exe}}, "is executable; declare its mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN a layer is written from it
			var out bytes.Buffer
			_, err := Write(Spec{Entries: tc.entries}, &out)

			// THEN it is refused, saying why, and nothing is written
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.Zero(t, out.Len())
		})
	}
}
