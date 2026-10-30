package publish

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
)

// countingRegistry is an in-process registry that counts the requests that
// write: blob uploads and manifest puts.
func countingRegistry(t *testing.T) (string, *int64) {
	t.Helper()
	var writes int64
	inner := registry.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut || r.Method == http.MethodPost || r.Method == http.MethodPatch {
			atomic.AddInt64(&writes, 1)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://"), &writes
}

// built is a real image from the plugin's own layer and image code, on disk
// as a build leaves it.
func built(t *testing.T) (Image, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "greeting")
	require.NoError(t, os.WriteFile(src, []byte("hello, registry\n"), 0o644))
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

	loaded, err := Load(manifestPath, configPath, []string{blobPath})
	require.NoError(t, err)
	return loaded, img.Meta.Manifest.Digest.String()
}

func refs(t *testing.T, host, digest string) (name.Digest, name.Tag) {
	t.Helper()
	ref, err := name.NewDigest(host + "/team/app@" + digest)
	require.NoError(t, err)
	tag, err := name.NewTag(host + "/team/app:keep-sha256-" + strings.TrimPrefix(digest, "sha256:"))
	require.NoError(t, err)
	return ref, tag
}

func resolves(t *testing.T, ref name.Reference) string {
	t.Helper()
	descriptor, err := remote.Head(ref)
	require.NoError(t, err)
	return descriptor.Digest.String()
}

func TestPublish_NeitherPresentUploadsTheImageThenTagsIt(t *testing.T) {
	// GIVEN a built image and an empty registry
	host, _ := countingRegistry(t)
	img, digest := built(t)
	ref, tag := refs(t, host, digest)

	// WHEN it is published
	result, err := Publish(img, ref, tag)

	// THEN the image and its retention tag both resolve to the built digest
	require.NoError(t, err)
	require.Equal(t, Result{Digest: digest, UploadedImage: true, WroteTag: true}, result)
	require.Equal(t, digest, resolves(t, ref))
	require.Equal(t, digest, resolves(t, tag))
}

func TestPublish_ADigestWithoutItsTagGetsOnlyTheTag(t *testing.T) {
	// GIVEN the image already in the registry by digest, with no retention tag
	host, writes := countingRegistry(t)
	img, digest := built(t)
	ref, tag := refs(t, host, digest)
	core, err := newCore(img)
	require.NoError(t, err)
	untagged, err := partial.CompressedToImage(core)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, untagged))
	atomic.StoreInt64(writes, 0)

	// WHEN it is published
	result, err := Publish(img, ref, tag)

	// THEN nothing is uploaded but the tag: one manifest put
	require.NoError(t, err)
	require.Equal(t, Result{Digest: digest, ImageExisted: true, WroteTag: true}, result)
	require.Equal(t, int64(1), atomic.LoadInt64(writes))
	require.Equal(t, digest, resolves(t, tag))
}

func TestPublish_BothPresentWritesNothing(t *testing.T) {
	// GIVEN the image published and tagged
	host, writes := countingRegistry(t)
	img, digest := built(t)
	ref, tag := refs(t, host, digest)
	_, err := Publish(img, ref, tag)
	require.NoError(t, err)
	atomic.StoreInt64(writes, 0)

	// WHEN it is published again
	result, err := Publish(img, ref, tag)

	// THEN the registry receives no write at all
	require.NoError(t, err)
	require.Equal(t, Result{Digest: digest, ImageExisted: true, TagExisted: true}, result)
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestPublish_ARetentionTagOnAnotherImageIsRefusedBeforeAnyWrite(t *testing.T) {
	// GIVEN the retention tag already naming a different image
	host, writes := countingRegistry(t)
	img, digest := built(t)
	ref, tag := refs(t, host, digest)
	other, err := random.Image(64, 1)
	require.NoError(t, err)
	require.NoError(t, remote.Write(tag, other))
	otherDigest, err := other.Digest()
	require.NoError(t, err)
	atomic.StoreInt64(writes, 0)

	// WHEN the image is published
	_, err = Publish(img, ref, tag)

	// THEN it is refused, naming both digests, and nothing was written
	require.Error(t, err)
	require.Contains(t, err.Error(), "already names "+otherDigest.String()+", but this image is "+digest)
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestPublish_ARegistryThatRefusesIsAnErrorNotAbsent(t *testing.T) {
	// GIVEN a registry that answers 401 to everything, as a lapsed credential does
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(refusing.Close)
	img, digest := built(t)
	ref, tag := refs(t, strings.TrimPrefix(refusing.URL, "http://"), digest)

	// WHEN the image is published there
	_, err := Publish(img, ref, tag)

	// THEN it is an error, never read as "absent" and pushed blind
	require.Error(t, err)
	require.Contains(t, err.Error(), "looking up "+ref.String())
}

func TestPublish_RefusesAReferenceThatIsNotTheBuiltImage(t *testing.T) {
	// GIVEN a reference naming some other digest
	host, writes := countingRegistry(t)
	img, _ := built(t)
	wrong, err := name.NewDigest(host + "/team/app@sha256:" + strings.Repeat("0", 64))
	require.NoError(t, err)
	tag, err := name.NewTag(host + "/team/app:keep-sha256-" + strings.Repeat("0", 64))
	require.NoError(t, err)

	// WHEN it is published
	_, err = Publish(img, wrong, tag)

	// THEN it is refused before anything is sent
	require.Error(t, err)
	require.Contains(t, err.Error(), "but the reference to publish names sha256:"+strings.Repeat("0", 64))
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestLoad_MatchesBlobsByContentNotByName(t *testing.T) {
	// GIVEN a built image whose manifest names a layer, and a file of other bytes
	img, _ := built(t)
	manifest, err := v1.ParseManifest(bytes.NewReader(img.Manifest))
	require.NoError(t, err)
	stray := filepath.Join(t.TempDir(), "layer.tar.gz")
	require.NoError(t, os.WriteFile(stray, []byte("not the layer"), 0o644))
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.json")
	configPath := filepath.Join(dir, "c.json")
	require.NoError(t, os.WriteFile(manifestPath, img.Manifest, 0o644))
	require.NoError(t, os.WriteFile(configPath, img.Config, 0o644))

	// WHEN it is loaded with only the stray file
	loaded, err := Load(manifestPath, configPath, []string{stray})
	require.NoError(t, err)
	_, err = newCore(loaded)

	// THEN the layer is missing, because no file holds its bytes
	require.Error(t, err)
	require.Contains(t, err.Error(), "no blob was given for layer "+manifest.Layers[0].Digest.String())
}

func TestLoad_ADirectoryOfBlobsIsEveryFileInIt(t *testing.T) {
	// GIVEN a built image whose one layer is in a directory, as an
	// oci_import's blobs are, beside a stray file
	img, _ := built(t)
	manifest, err := v1.ParseManifest(bytes.NewReader(img.Manifest))
	require.NoError(t, err)
	layerDigest := manifest.Layers[0].Digest
	dir := t.TempDir()
	blobs := filepath.Join(dir, "blobs")
	require.NoError(t, os.Mkdir(blobs, 0o755))
	layerBytes, err := os.ReadFile(img.Blobs[layerDigest])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(blobs, layerDigest.Hex), layerBytes, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(blobs, "stray"), []byte("not a layer"), 0o644))
	manifestPath := filepath.Join(dir, "m.json")
	configPath := filepath.Join(dir, "c.json")
	require.NoError(t, os.WriteFile(manifestPath, img.Manifest, 0o644))
	require.NoError(t, os.WriteFile(configPath, img.Config, 0o644))

	// WHEN it is loaded with the directory
	loaded, err := Load(manifestPath, configPath, []string{blobs})
	require.NoError(t, err)

	// THEN the layer is found in it, and the image assembles
	require.Equal(t, filepath.Join(blobs, layerDigest.Hex), loaded.Blobs[layerDigest])
	_, err = newCore(loaded)
	require.NoError(t, err)
}

func TestLoad_RefusesADirectoryInsideABlobDirectory(t *testing.T) {
	// GIVEN a blob directory holding a directory
	img, _ := built(t)
	dir := t.TempDir()
	blobs := filepath.Join(dir, "blobs")
	require.NoError(t, os.MkdirAll(filepath.Join(blobs, "nested"), 0o755))
	manifestPath := filepath.Join(dir, "m.json")
	configPath := filepath.Join(dir, "c.json")
	require.NoError(t, os.WriteFile(manifestPath, img.Manifest, 0o644))
	require.NoError(t, os.WriteFile(configPath, img.Config, 0o644))

	// WHEN it is loaded
	_, err := Load(manifestPath, configPath, []string{blobs})

	// THEN it is refused, naming what is in the way
	require.Error(t, err)
	require.Contains(t, err.Error(), "holds a directory, nested")
}
