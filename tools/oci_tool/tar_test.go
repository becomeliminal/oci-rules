package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
)

// member is one tar entry a test archive holds.
type member struct {
	hdr  tar.Header
	body string
}

func writeArchive(t *testing.T, gzipped bool, members ...member) string {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, m := range members {
		hdr := m.hdr
		hdr.Size = int64(len(m.body))
		require.NoError(t, tw.WriteHeader(&hdr))
		_, err := tw.Write([]byte(m.body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	content := raw.Bytes()
	if gzipped {
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		_, err := zw.Write(content)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		content = gz.Bytes()
	}
	out := filepath.Join(t.TempDir(), "archive.tar")
	require.NoError(t, os.WriteFile(out, content, 0o644))
	return out
}

func modeOf(m int64) *int64 { return &m }

// packageArchive is a package's data as dpkg or apk ship it: a ./ root, a
// directory owned by a service account, a file, an executable, a symlink and
// a hard link.
var packageArchive = []member{
	{hdr: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}},
	{hdr: tar.Header{Name: "./usr/", Typeflag: tar.TypeDir, Mode: 0o755}},
	{hdr: tar.Header{Name: "./usr/bin/", Typeflag: tar.TypeDir, Mode: 0o755}},
	{hdr: tar.Header{Name: "./usr/bin/pgbackrest", Typeflag: tar.TypeReg, Mode: 0o755}, body: "binary"},
	{hdr: tar.Header{Name: "./usr/bin/pgbackrest-alias", Typeflag: tar.TypeLink, Linkname: "./usr/bin/pgbackrest", Mode: 0o755}},
	{hdr: tar.Header{Name: "./etc/", Typeflag: tar.TypeDir, Mode: 0o755}},
	{hdr: tar.Header{Name: "./etc/pgbackrest.conf", Typeflag: tar.TypeReg, Mode: 0o640, Uid: 999, Gid: 999}, body: "[global]\n"},
	{hdr: tar.Header{Name: "./var/lib/pgbackrest/", Typeflag: tar.TypeDir, Mode: 0o750, Uid: 999, Gid: 999}},
	{hdr: tar.Header{Name: "./usr/lib/libssh2.so.1", Typeflag: tar.TypeSymlink, Linkname: "libssh2.so.1.0.1", Mode: 0o777}},
	{hdr: tar.Header{Name: "./usr/bin/editor", Typeflag: tar.TypeSymlink, Linkname: "/etc/alternatives/editor", Mode: 0o777}},
	{hdr: tar.Header{Name: "./usr/lib/x86_64-linux-gnu/libpq.so", Typeflag: tar.TypeSymlink, Linkname: "../../lib/libpq.so.5", Mode: 0o777}},
}

func TestTarEntries_KeepsEachEntrysModeOwnerAndLink(t *testing.T) {
	for _, gzipped := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gzipped"}[gzipped], func(t *testing.T) {
			// GIVEN a package's data archive
			archive := writeArchive(t, gzipped, packageArchive...)
			scratch := t.TempDir()

			// WHEN its entries are read for a layer
			entries, err := tarEntries(archive, scratch)

			// THEN each entry is at its image path with its own mode, owner
			// and link target, the root is skipped, and the hard link is a
			// copy of the file it names
			require.NoError(t, err)
			require.Len(t, entries, 10)
			for i := range entries {
				if entries[i].Kind == layer.KindFile {
					content, err := os.ReadFile(entries[i].Source)
					require.NoError(t, err)
					entries[i].Source = string(content)
				}
			}
			require.Equal(t, []layer.Entry{
				{Path: "/usr", Kind: layer.KindDir, Mode: modeOf(0o755), Packaged: true},
				{Path: "/usr/bin", Kind: layer.KindDir, Mode: modeOf(0o755), Packaged: true},
				{Path: "/usr/bin/pgbackrest", Kind: layer.KindFile, Source: "binary", Mode: modeOf(0o755), Packaged: true},
				{Path: "/usr/bin/pgbackrest-alias", Kind: layer.KindFile, Source: "binary", Mode: modeOf(0o755), Packaged: true},
				{Path: "/etc", Kind: layer.KindDir, Mode: modeOf(0o755), Packaged: true},
				{Path: "/etc/pgbackrest.conf", Kind: layer.KindFile, Source: "[global]\n", Mode: modeOf(0o640), UID: 999, GID: 999, Packaged: true},
				{Path: "/var/lib/pgbackrest", Kind: layer.KindDir, Mode: modeOf(0o750), UID: 999, GID: 999, Packaged: true},
				{Path: "/usr/lib/libssh2.so.1", Kind: layer.KindSymlink, Target: "libssh2.so.1.0.1", Packaged: true},
				{Path: "/usr/bin/editor", Kind: layer.KindSymlink, Target: "/etc/alternatives/editor", Packaged: true},
				{Path: "/usr/lib/x86_64-linux-gnu/libpq.so", Kind: layer.KindSymlink, Target: "../../lib/libpq.so.5", Packaged: true},
			}, entries)
		})
	}
}

func TestTarEntries_RefusesWhatALayerCannotHold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members []member
		want    string
	}{
		{"a device", []member{{hdr: tar.Header{Name: "./dev/null", Typeflag: tar.TypeChar, Mode: 0o666}}}, "/dev/null is tar type"},
		{"a name that leaves the image root", []member{{hdr: tar.Header{Name: "../etc/passwd", Typeflag: tar.TypeReg, Mode: 0o644}, body: "x"}}, "leaves the image root"},
		{"a relative symlink that climbs above the image root", []member{
			{hdr: tar.Header{Name: "./app/link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/shadow"}},
		}, "/app/link links to ../../etc/shadow, which climbs above the image root"},
		{"a hard link to a directory", []member{
			{hdr: tar.Header{Name: "./usr/", Typeflag: tar.TypeDir, Mode: 0o755}},
			{hdr: tar.Header{Name: "./opt", Typeflag: tar.TypeLink, Linkname: "./usr"}},
		}, "/opt is a hard link to the directory /usr"},
		{"a hard link to nothing given before it", []member{{hdr: tar.Header{Name: "./a", Typeflag: tar.TypeLink, Linkname: "./b"}}}, "/a is a hard link to /b"},
		{"nothing but the root", []member{{hdr: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}}}, "holds no entries"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN an archive holding it
			archive := writeArchive(t, false, tc.members...)

			// WHEN its entries are read for a layer
			_, err := tarEntries(archive, t.TempDir())

			// THEN it is refused, naming why
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestTarEntries_KeepsSetuidSetgidAndStickyBitsInTheLayer(t *testing.T) {
	// GIVEN a package archive holding a setuid file, a setgid directory and
	// a sticky, setgid directory, as Debian's /var/mail and postgres's run
	// directory are
	archive := writeArchive(t, false,
		member{hdr: tar.Header{Name: "./usr/bin/su", Typeflag: tar.TypeReg, Mode: 0o4755}, body: "x"},
		member{hdr: tar.Header{Name: "./var/mail/", Typeflag: tar.TypeDir, Mode: 0o2775, Gid: 8}},
		member{hdr: tar.Header{Name: "./var/run/postgresql/", Typeflag: tar.TypeDir, Mode: 0o3777, Uid: 70, Gid: 70}},
	)
	entries, err := tarEntries(archive, t.TempDir())
	require.NoError(t, err)

	// WHEN a layer is written from it
	var blob bytes.Buffer
	_, err = layer.Write(layer.Spec{Entries: entries}, &blob)

	// THEN each keeps its whole mode, as dpkg and apk would install it
	require.NoError(t, err)
	zr, err := gzip.NewReader(&blob)
	require.NoError(t, err)
	modes := map[string]int64{}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		modes[hdr.Name] = hdr.Mode
	}
	require.Equal(t, int64(0o4755), modes["usr/bin/su"])
	require.Equal(t, int64(0o2775), modes["var/mail/"])
	require.Equal(t, int64(0o3777), modes["var/run/postgresql/"])
}

func TestTarEntries_ADeclaredEntryMayStillSetOnlyPermissionBits(t *testing.T) {
	// GIVEN a setuid file a rule declares itself, not one from a tarball
	source := filepath.Join(t.TempDir(), "su")
	require.NoError(t, os.WriteFile(source, []byte("x"), 0o644))
	entries := []layer.Entry{{Path: "/usr/bin/su", Kind: layer.KindFile, Source: source, Mode: modeOf(0o4755)}}

	// WHEN a layer is written from it
	_, err := layer.Write(layer.Spec{Entries: entries}, &bytes.Buffer{})

	// THEN it is refused, naming the file
	require.Error(t, err)
	require.Contains(t, err.Error(), "/usr/bin/su has mode 04755")
}

func TestMergeDirs_WritesADirectoryTwoArchivesHoldAlikeOnce(t *testing.T) {
	// GIVEN two archives' entries that both hold /usr alike, and /var/lib
	// with different owners
	entries := []layer.Entry{
		{Path: "/usr", Kind: layer.KindDir, Mode: modeOf(0o755)},
		{Path: "/var/lib", Kind: layer.KindDir, Mode: modeOf(0o755)},
		{Path: "/usr", Kind: layer.KindDir, Mode: modeOf(0o755)},
		{Path: "/var/lib", Kind: layer.KindDir, Mode: modeOf(0o755), UID: 999, GID: 999},
	}

	// WHEN they are merged
	merged := mergeDirs(entries)

	// THEN /usr is written once, and both /var/lib stay for the layer to
	// refuse as declared twice
	require.Equal(t, []layer.Entry{
		{Path: "/usr", Kind: layer.KindDir, Mode: modeOf(0o755)},
		{Path: "/var/lib", Kind: layer.KindDir, Mode: modeOf(0o755)},
		{Path: "/var/lib", Kind: layer.KindDir, Mode: modeOf(0o755), UID: 999, GID: 999},
	}, merged)
	_, err := layer.Write(layer.Spec{Entries: merged}, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "/var/lib is declared twice")
}
