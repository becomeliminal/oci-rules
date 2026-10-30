package archive

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
)

// built is a real image from the plugin's own layer and image code, on disk
// as a build leaves it, with its manifest parsed.
func built(t *testing.T) (publish.Image, *v1.Manifest) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "greeting")
	require.NoError(t, os.WriteFile(src, []byte("hello, archive\n"), 0o644))
	var blob bytes.Buffer
	meta, err := layer.Write(layer.Spec{Entries: []layer.Entry{{Path: "/etc/greeting", Kind: layer.KindFile, Source: src}}}, &blob)
	require.NoError(t, err)
	blobPath := filepath.Join(dir, "layer.tar.gz")
	require.NoError(t, os.WriteFile(blobPath, blob.Bytes(), 0o644))

	img, err := image.Build(nil, []image.LayerMeta{{SchemaVersion: 1, MediaType: meta.MediaType, Digest: meta.Digest, Size: meta.Size, DiffID: meta.DiffID}}, image.Spec{Platform: "linux/amd64"})
	require.NoError(t, err)
	manifestPath := filepath.Join(dir, "manifest.json")
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(manifestPath, img.Manifest, 0o644))
	require.NoError(t, os.WriteFile(configPath, img.Config, 0o644))

	loaded, err := publish.Load(manifestPath, configPath, []string{blobPath})
	require.NoError(t, err)
	manifest, err := v1.ParseManifest(bytes.NewReader(img.Manifest))
	require.NoError(t, err)
	return loaded, manifest
}

func TestWrite_TheSameImageGivesTheSameBytes(t *testing.T) {
	// GIVEN a built image and the tag it loads as
	img, _ := built(t)
	tag, err := name.NewTag("registry.example.com/team/app:keep-sha256-feed", name.StrictValidation)
	require.NoError(t, err)

	// WHEN it is archived twice
	var first, second bytes.Buffer
	require.NoError(t, WriteDocker(img, []name.Tag{tag}, &first))
	require.NoError(t, WriteDocker(img, []name.Tag{tag}, &second))

	// THEN the archives are identical, so the archive is a cacheable output
	require.Equal(t, first.Bytes(), second.Bytes())
}

func TestWrite_LoadsAsItsTagWithTheBuildsOwnConfigAndLayers(t *testing.T) {
	// GIVEN a built image and the tag it loads as
	img, manifest := built(t)
	tag, err := name.NewTag("registry.example.com/team/app:keep-sha256-feed", name.StrictValidation)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "image.tar")
	out, err := os.Create(path)
	require.NoError(t, err)

	// WHEN it is archived
	require.NoError(t, WriteDocker(img, []name.Tag{tag}, out))
	require.NoError(t, out.Close())

	// THEN the archive's manifest names exactly that tag, as docker load reads it
	type entry struct {
		Config   string
		RepoTags []string
		Layers   []string
	}
	var entries []entry
	require.NoError(t, json.Unmarshal(readEntry(t, path, "manifest.json"), &entries))
	require.Len(t, entries, 1)
	require.Equal(t, []string{tag.String()}, entries[0].RepoTags)

	// AND read back, it is the build's config and the build's compressed layer,
	// byte for byte, never recompressed
	loaded, err := tarball.ImageFromPath(path, &tag)
	require.NoError(t, err)
	config, err := loaded.RawConfigFile()
	require.NoError(t, err)
	require.Equal(t, img.Config, config)
	layers, err := loaded.Layers()
	require.NoError(t, err)
	require.Len(t, layers, 1)
	digest, err := layers[0].Digest()
	require.NoError(t, err)
	require.Equal(t, manifest.Layers[0].Digest, digest)
}

func TestWrite_RefusesAnImageWithoutItsLayer(t *testing.T) {
	// GIVEN a built image whose layer blob is not among the files given
	img, manifest := built(t)
	img.Blobs = map[v1.Hash]string{}
	tag, err := name.NewTag("registry.example.com/team/app:keep-sha256-feed", name.StrictValidation)
	require.NoError(t, err)

	// WHEN it is archived
	err = WriteDocker(img, []name.Tag{tag}, io.Discard)

	// THEN it is refused, naming the layer, rather than written incomplete
	require.Error(t, err)
	require.Contains(t, err.Error(), "no blob was given for layer "+manifest.Layers[0].Digest.String())
}

// readEntry is the contents of one file in a tar.
func readEntry(t *testing.T, path, entryName string) []byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	reader := tar.NewReader(f)
	for {
		header, err := reader.Next()
		require.NoError(t, err, "no %s in %s", entryName, path)
		if header.Name == entryName {
			contents, err := io.ReadAll(reader)
			require.NoError(t, err)
			return contents
		}
	}
}

func mustTag(t *testing.T, s string) name.Tag {
	t.Helper()
	tag, err := name.NewTag(s, name.StrictValidation)
	require.NoError(t, err)
	return tag
}

func TestWriteDocker_TagsAreSortedAndTheBytesNeverVary(t *testing.T) {
	// GIVEN a built image and two tags, given out of order
	img, _ := built(t)
	tags := []name.Tag{mustTag(t, "registry.example.com/team/app:v1"), mustTag(t, "registry.example.com/team/app:stable")}

	// WHEN it is archived many times over
	var first bytes.Buffer
	require.NoError(t, WriteDocker(img, tags, &first))
	for i := 0; i < 20; i++ {
		var again bytes.Buffer
		require.NoError(t, WriteDocker(img, tags, &again))
		require.Equal(t, first.Bytes(), again.Bytes(), "write %d", i)
	}

	// THEN every archive is the same bytes, listing the tags sorted
	path := filepath.Join(t.TempDir(), "image.tar")
	require.NoError(t, os.WriteFile(path, first.Bytes(), 0o644))
	var entries []struct{ RepoTags []string }
	require.NoError(t, json.Unmarshal(readEntry(t, path, "manifest.json"), &entries))
	require.Equal(t, []string{"registry.example.com/team/app:stable", "registry.example.com/team/app:v1"}, entries[0].RepoTags)

	// AND the tags given the other way round give the same bytes
	var reversed bytes.Buffer
	require.NoError(t, WriteDocker(img, []name.Tag{tags[1], tags[0]}, &reversed))
	require.Equal(t, first.Bytes(), reversed.Bytes())
}

func TestWriteDocker_OneTagIsGoContainerregistrysOwnFormatByteForByte(t *testing.T) {
	// GIVEN a built image and one tag
	img, _ := built(t)
	tag := mustTag(t, "registry.example.com/team/app:v1")
	assembled, err := img.Assemble()
	require.NoError(t, err)

	// WHEN it is archived here and by go-containerregistry's writer
	var ours, theirs bytes.Buffer
	require.NoError(t, WriteDocker(img, []name.Tag{tag}, &ours))
	require.NoError(t, tarball.Write(tag, assembled, &theirs))

	// THEN the two are identical: only the order of several tags differs
	require.Equal(t, theirs.Bytes(), ours.Bytes())
}

func TestWriteDocker_RefusesNoTagsAndATagGivenTwice(t *testing.T) {
	img, _ := built(t)
	tag := mustTag(t, "registry.example.com/team/app:v1")
	for _, tc := range []struct {
		name string
		tags []name.Tag
		want string
	}{
		{"no tags", nil, "loads as at least one tag, and none was given"},
		{"a tag twice", []name.Tag{tag, tag}, "tag registry.example.com/team/app:v1 is given twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN it is archived
			err := WriteDocker(img, tc.tags, io.Discard)

			// THEN it is refused
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestWriteLayout_WritesAValidLayoutNamingTheImage(t *testing.T) {
	// GIVEN a built image
	img, manifest := built(t)
	dir := filepath.Join(t.TempDir(), "layout")

	// WHEN it is written as a layout named "app"
	require.NoError(t, WriteLayout(img, "app", dir))

	// THEN go-containerregistry reads it back as the same image, named "app"
	index, err := layout.ImageIndexFromPath(dir)
	require.NoError(t, err)
	indexManifest, err := index.IndexManifest()
	require.NoError(t, err)
	require.Len(t, indexManifest.Manifests, 1)
	require.Equal(t, "app", indexManifest.Manifests[0].Annotations[RefNameAnnotation])
	require.Equal(t, sha256Of(t, img.Manifest), indexManifest.Manifests[0].Digest.String())

	// AND every file is 0644 and every directory 0755, whatever the umask
	require.NoError(t, filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		want := os.FileMode(0o644)
		if info.IsDir() {
			want = 0o755
		}
		require.Equal(t, want, info.Mode().Perm(), p)
		return nil
	}))
	// AND the layer's blob is there under its digest
	_, err = os.Stat(filepath.Join(dir, "blobs", "sha256", manifest.Layers[0].Digest.Hex))
	require.NoError(t, err)
}

func TestWriteLayout_RefusesADirectoryThatExists(t *testing.T) {
	// GIVEN a directory already there
	img, _ := built(t)
	dir := t.TempDir()

	// WHEN a layout is written into it
	err := WriteLayout(img, "", dir)

	// THEN it is refused, rather than merged with what is there
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists; a layout is written into a new directory")
}

func TestWriteOCI_IsTheLayoutAsATarWithFixedEntries(t *testing.T) {
	// GIVEN a built image
	img, _ := built(t)

	// WHEN it is written as an OCI archive twice
	var first, second bytes.Buffer
	require.NoError(t, WriteOCI(img, "app", &first))
	require.NoError(t, WriteOCI(img, "app", &second))

	// THEN the bytes are identical, and every entry is in path order with the
	// epoch, 0:0 and a fixed mode
	require.Equal(t, first.Bytes(), second.Bytes())
	reader := tar.NewReader(bytes.NewReader(first.Bytes()))
	var names []string
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
		require.Zero(t, hdr.ModTime.Unix(), hdr.Name)
		require.Zero(t, hdr.Uid, hdr.Name)
		require.Zero(t, hdr.Gid, hdr.Name)
		if hdr.Typeflag == tar.TypeDir {
			require.Equal(t, int64(0o755), hdr.Mode, hdr.Name)
		} else {
			require.Equal(t, int64(0o644), hdr.Mode, hdr.Name)
		}
	}
	require.True(t, sortedStrings(names), "entries in path order: %v", names)
	require.Contains(t, names, "oci-layout")
	require.Contains(t, names, "index.json")
}

func sha256Of(t *testing.T, b []byte) string {
	t.Helper()
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}
