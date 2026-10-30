package publish

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
)

// builtFor is a real image for platform, on disk as a build leaves it, and
// its metadata.
func builtFor(t *testing.T, platform string) (Image, image.Meta) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "greeting")
	require.NoError(t, os.WriteFile(src, []byte("hello from "+platform+"\n"), 0o644))
	var blob bytes.Buffer
	meta, err := layer.Write(layer.Spec{Entries: []layer.Entry{{Path: "/etc/greeting", Kind: layer.KindFile, Source: src}}}, &blob)
	require.NoError(t, err)
	blobPath := filepath.Join(dir, "layer.tar.gz")
	require.NoError(t, os.WriteFile(blobPath, blob.Bytes(), 0o644))
	img, err := image.Build(nil, []image.LayerMeta{{SchemaVersion: 1, MediaType: meta.MediaType, Digest: meta.Digest, Size: meta.Size, DiffID: meta.DiffID}}, image.Spec{Platform: platform})
	require.NoError(t, err)
	manifestPath := filepath.Join(dir, "manifest.json")
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(manifestPath, img.Manifest, 0o644))
	require.NoError(t, os.WriteFile(configPath, img.Config, 0o644))
	loaded, err := Load(manifestPath, configPath, []string{blobPath})
	require.NoError(t, err)
	return loaded, img.Meta
}

// builtIndex is a two-platform index and its references in host's team/app.
func builtIndex(t *testing.T, host string) (ImageIndex, name.Digest, name.Tag, []image.Meta) {
	t.Helper()
	amd, amdMeta := builtFor(t, "linux/amd64")
	arm, armMeta := builtFor(t, "linux/arm64")
	built, err := image.Index([]image.ChildMeta{{Meta: amdMeta}, {Meta: armMeta}})
	require.NoError(t, err)
	digest := built.Meta.Manifest.Digest
	ref, err := name.NewDigest(host + "/team/app@" + digest.String())
	require.NoError(t, err)
	tag, err := name.NewTag(host + "/team/app:keep-sha256-" + digest.Hex)
	require.NoError(t, err)
	return ImageIndex{Manifest: built.Index, Children: []Image{amd, arm}}, ref, tag, []image.Meta{amdMeta, armMeta}
}

func TestPublishIndex_PublishesEveryChildThenTheIndexEachTagged(t *testing.T) {
	// GIVEN a two-platform index and an empty registry
	host, _ := countingRegistry(t)
	idx, ref, tag, children := builtIndex(t, host)

	// WHEN it is published
	result, childResults, err := Index(idx, ref, tag)

	// THEN the index and its tag resolve to it, and each child is in the
	// repository by digest and by its own retention tag
	require.NoError(t, err)
	require.Equal(t, Result{Digest: ref.DigestStr(), UploadedImage: true, WroteTag: true}, result)
	require.Len(t, childResults, 2)
	require.Equal(t, ref.DigestStr(), resolves(t, ref))
	require.Equal(t, ref.DigestStr(), resolves(t, tag))
	for _, child := range children {
		childRef, childTag, err := childReference(ref.Context(), child.Manifest.Digest)
		require.NoError(t, err)
		require.Equal(t, child.Manifest.Digest.String(), resolves(t, childRef))
		require.Equal(t, child.Manifest.Digest.String(), resolves(t, childTag))
	}

	// AND a client reads it as an index whose children are those images
	pulled, err := remote.Index(ref)
	require.NoError(t, err)
	manifest, err := pulled.IndexManifest()
	require.NoError(t, err)
	require.Equal(t, []v1.Hash{children[0].Manifest.Digest, children[1].Manifest.Digest}, []v1.Hash{manifest.Manifests[0].Digest, manifest.Manifests[1].Digest})
}

func TestPublishIndex_PublishedAgainWritesNothing(t *testing.T) {
	// GIVEN an index already published
	host, writes := countingRegistry(t)
	idx, ref, tag, _ := builtIndex(t, host)
	_, _, err := Index(idx, ref, tag)
	require.NoError(t, err)
	atomic.StoreInt64(writes, 0)

	// WHEN it is published again
	result, _, err := Index(idx, ref, tag)

	// THEN the registry receives no write at all
	require.NoError(t, err)
	require.Equal(t, Result{Digest: ref.DigestStr(), ImageExisted: true, TagExisted: true}, result)
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestPublishIndex_AChildsRetentionTagOnAnotherImageStopsEverythingBeforeAnyWrite(t *testing.T) {
	// GIVEN a child's retention tag already naming some other image
	host, writes := countingRegistry(t)
	idx, ref, tag, children := builtIndex(t, host)
	_, childTag, err := childReference(ref.Context(), children[1].Manifest.Digest)
	require.NoError(t, err)
	other, err := random.Image(64, 1)
	require.NoError(t, err)
	require.NoError(t, remote.Write(childTag, other))
	otherDigest, err := other.Digest()
	require.NoError(t, err)
	atomic.StoreInt64(writes, 0)

	// WHEN the index is published
	_, _, err = Index(idx, ref, tag)

	// THEN it is refused, naming the tag and both digests, and nothing at
	// all was written: not the other child, not the index
	require.Error(t, err)
	require.Contains(t, err.Error(), "nothing was written")
	require.Contains(t, err.Error(), "retain "+childTag.String()+" already names "+otherDigest.String())
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestPublishIndex_RefusesChildrenThatAreNotTheIndexs(t *testing.T) {
	// GIVEN an index given one of its children and a stranger
	host, writes := countingRegistry(t)
	idx, ref, tag, _ := builtIndex(t, host)
	stranger, _ := builtFor(t, "linux/arm64")
	// The same image but for one space in its manifest: another digest.
	stranger.Manifest = []byte(strings.Replace(string(stranger.Manifest), `"schemaVersion":2`, `"schemaVersion":2 `, 1))
	idx.Children[1] = stranger

	// WHEN it is published
	_, _, err := Index(idx, ref, tag)

	// THEN it is refused before anything is written
	require.Error(t, err)
	require.Contains(t, err.Error(), "and no image given is it")
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestIndex_RefusesAChildGivenTwiceBeforeAnyWrite(t *testing.T) {
	// GIVEN an index given its amd64 child twice and its arm64 child not at all
	host, writes := countingRegistry(t)
	idx, ref, tag, children := builtIndex(t, host)
	idx.Children[1] = idx.Children[0]

	// WHEN it is published
	_, _, err := Index(idx, ref, tag)

	// THEN it is refused, naming the child, and nothing was written
	require.Error(t, err)
	require.Contains(t, err.Error(), "child manifest "+children[0].Manifest.Digest.String()+" is given twice")
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestIndex_RefusesAnIndexNamingOneImageTwiceBeforeAnyWrite(t *testing.T) {
	// GIVEN an index document listing its amd64 child twice
	host, writes := countingRegistry(t)
	idx, _, _, children := builtIndex(t, host)
	manifest, err := v1.ParseIndexManifest(bytes.NewReader(idx.Manifest))
	require.NoError(t, err)
	manifest.Manifests[1] = manifest.Manifests[0]
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	idx.Manifest = raw
	sum := sha256.Sum256(raw)
	ref, err := name.NewDigest(host + "/team/app@sha256:" + hex.EncodeToString(sum[:]))
	require.NoError(t, err)
	tag, err := name.NewTag(host + "/team/app:keep-sha256-" + hex.EncodeToString(sum[:]))
	require.NoError(t, err)

	// WHEN it is published
	_, _, err = Index(idx, ref, tag)

	// THEN it is refused, naming the image, and nothing was written
	require.Error(t, err)
	require.Contains(t, err.Error(), "the index names "+children[0].Manifest.Digest.String()+" twice")
	require.Zero(t, atomic.LoadInt64(writes))
}

func TestIndex_RefusesAChildWithoutItsLayerBeforeWritingAnyOther(t *testing.T) {
	// GIVEN an index whose second child has lost its layer's blob, while the
	// first is complete
	host, writes := countingRegistry(t)
	idx, ref, tag, children := builtIndex(t, host)
	idx.Children[1].Blobs = map[v1.Hash]string{}

	// WHEN it is published
	_, _, err := Index(idx, ref, tag)

	// THEN it is refused, naming the child and its layer, and not even the
	// complete first child was written
	require.Error(t, err)
	require.Contains(t, err.Error(), "child 1 ("+children[1].Manifest.Digest.String()+"): no blob was given for layer")
	require.Zero(t, atomic.LoadInt64(writes))
}
