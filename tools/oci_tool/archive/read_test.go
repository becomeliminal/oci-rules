package archive

import (
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/stretchr/testify/require"
)

// imageFor is a random two-layer image whose config is for os/arch.
func imageFor(t *testing.T, os, arch string) v1.Image {
	t.Helper()
	img, err := random.Image(256, 2)
	require.NoError(t, err)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture = os, arch
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)
	return img
}

// layoutOf writes images into a new layout, each under the platform given
// beside it (nil: none).
func layoutOf(t *testing.T, images map[v1.Image]*v1.Platform) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "layout")
	path, err := layout.Write(dir, empty.Index)
	require.NoError(t, err)
	for img, platform := range images {
		var options []layout.Option
		if platform != nil {
			options = append(options, layout.WithPlatform(*platform))
		}
		require.NoError(t, path.AppendImage(img, options...))
	}
	return dir
}

func layerDigests(t *testing.T, img v1.Image) []v1.Hash {
	t.Helper()
	manifest, err := img.Manifest()
	require.NoError(t, err)
	var digests []v1.Hash
	for _, l := range manifest.Layers {
		digests = append(digests, l.Digest)
	}
	return digests
}

func TestReadLayout_TakesThePlatformsImageAsABase(t *testing.T) {
	// GIVEN a layout holding an amd64 and an arm64 image
	amd64, arm64 := imageFor(t, "linux", "amd64"), imageFor(t, "linux", "arm64")
	dir := layoutOf(t, map[v1.Image]*v1.Platform{
		amd64: {OS: "linux", Architecture: "amd64"},
		arm64: {OS: "linux", Architecture: "arm64"},
	})

	// WHEN the amd64 image is read as a base
	base, layers, err := ReadLayout(dir, "linux/amd64")
	require.NoError(t, err)

	// THEN the base is that image's config and its layers, in order, and the
	// layers handed back are those layers' bytes
	want, err := amd64.ConfigFile()
	require.NoError(t, err)
	require.Equal(t, "linux/amd64", base.Platform)
	require.Equal(t, *want, base.Config)
	var got []v1.Hash
	for _, d := range base.Layers {
		got = append(got, d.Digest)
	}
	require.Equal(t, layerDigests(t, amd64), got)
	require.Len(t, layers, 2)
	for i, l := range layers {
		digest, err := l.Digest()
		require.NoError(t, err)
		require.Equal(t, got[i], digest)
	}
}

func TestReadLayout_FindsTheImageOneIndexDown(t *testing.T) {
	// GIVEN a layout whose index names a multi-platform index
	amd64, arm64 := imageFor(t, "linux", "amd64"), imageFor(t, "linux", "arm64")
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amd64, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: arm64, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	dir := filepath.Join(t.TempDir(), "layout")
	path, err := layout.Write(dir, empty.Index)
	require.NoError(t, err)
	require.NoError(t, path.AppendIndex(index))

	// WHEN the arm64 image is read
	base, _, err := ReadLayout(dir, "linux/arm64")

	// THEN it is the arm64 image
	require.NoError(t, err)
	require.Equal(t, "arm64", base.Config.Architecture)
}

func TestReadLayout_RefusesWhatIsNotExactlyOneSoundImage(t *testing.T) {
	amd64 := &v1.Platform{OS: "linux", Architecture: "amd64"}
	corrupted := func(t *testing.T) string {
		img := imageFor(t, "linux", "amd64")
		dir := layoutOf(t, map[v1.Image]*v1.Platform{img: amd64})
		digest := layerDigests(t, img)[0]
		require.NoError(t, os.WriteFile(filepath.Join(dir, "blobs", digest.Algorithm, digest.Hex), []byte("not the layer"), 0o644))
		return dir
	}
	for _, tc := range []struct {
		name   string
		layout func(t *testing.T) string
		want   string
	}{
		{"no image for the platform", func(t *testing.T) string {
			return layoutOf(t, map[v1.Image]*v1.Platform{imageFor(t, "linux", "arm64"): {OS: "linux", Architecture: "arm64"}})
		}, "holds 0 images for linux/amd64"},
		{"two images for the platform", func(t *testing.T) string {
			return layoutOf(t, map[v1.Image]*v1.Platform{imageFor(t, "linux", "amd64"): amd64, imageFor(t, "linux", "amd64"): amd64})
		}, "holds 2 images for linux/amd64"},
		{"a config of another platform than its descriptor", func(t *testing.T) string {
			return layoutOf(t, map[v1.Image]*v1.Platform{imageFor(t, "linux", "arm64"): amd64})
		}, "the image's config is linux/arm64, but the base is for linux/amd64"},
		{"a blob that is not its digest", corrupted, "does not validate"},
		{"no layout at all", func(t *testing.T) string { return t.TempDir() }, "reading the layout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN the layout
			dir := tc.layout(t)

			// WHEN its amd64 image is read as a base
			_, _, err := ReadLayout(dir, "linux/amd64")

			// THEN it is refused, saying why
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
