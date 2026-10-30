package image

import (
	"encoding/json"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

// childOf is the |meta of an image built for platform over one layer.
func childOf(t *testing.T, platform string) ChildMeta {
	t.Helper()
	built, err := Build(nil, []LayerMeta{appLayer()}, Spec{Platform: platform, Entrypoint: strs("/app-" + platform)})
	require.NoError(t, err)
	return ChildMeta{Meta: built.Meta}
}

func TestIndex_NamesEachChildByItsManifestSortedByPlatform(t *testing.T) {
	// GIVEN an arm64 image and an amd64 image, given in that order
	arm := childOf(t, "linux/arm64")
	amd := childOf(t, "linux/amd64")

	// WHEN they are indexed
	built, err := Index([]ChildMeta{arm, amd})
	require.NoError(t, err)

	// THEN the index lists amd64 then arm64, each descriptor exactly its
	// child's manifest with its platform
	var index v1.IndexManifest
	require.NoError(t, json.Unmarshal(built.Index, &index))
	require.Equal(t, types.OCIImageIndex, index.MediaType)
	require.Equal(t, []v1.Descriptor{
		{MediaType: types.OCIManifestSchema1, Digest: amd.Manifest.Digest, Size: amd.Manifest.Size, Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}},
		{MediaType: types.OCIManifestSchema1, Digest: arm.Manifest.Digest, Size: arm.Manifest.Size, Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}},
	}, index.Manifests)

	// AND the metadata's digest is the index document's
	require.Equal(t, sha(string(built.Index)), built.Meta.Manifest.Digest.String())
	require.Equal(t, index.Manifests, built.Meta.Children)
}

func TestIndex_TheOrderTheChildrenAreGivenInChangesNothing(t *testing.T) {
	// GIVEN the same two images
	arm := childOf(t, "linux/arm64")
	amd := childOf(t, "linux/amd64")

	// WHEN they are indexed in each order
	one, err := Index([]ChildMeta{arm, amd})
	require.NoError(t, err)
	other, err := Index([]ChildMeta{amd, arm})
	require.NoError(t, err)

	// THEN the index is the same bytes
	require.Equal(t, one.Index, other.Index)
}

func TestIndex_RefusesWhatIsNotOneImagePerPlatform(t *testing.T) {
	amd := childOf(t, "linux/amd64")
	arm := childOf(t, "linux/arm64")
	nested, err := Index([]ChildMeta{amd, arm})
	require.NoError(t, err)
	asChild := ChildMeta{Meta: Meta{SchemaVersion: 1, Manifest: nested.Meta.Manifest}, Children: nested.Meta.Children}
	otherAmd := childOf(t, "linux/amd64")
	otherAmd.Manifest.Digest = hash(t, sha("another amd64 image"))
	unknown := childOf(t, "linux/amd64")
	unknown.Platform = "linux/s390x"
	for _, tc := range []struct {
		name     string
		children []ChildMeta
		want     string
	}{
		{"one image", []ChildMeta{amd}, "an image index is of at least two platforms; 1 image(s) given"},
		{"two of one platform", []ChildMeta{amd, otherAmd}, "are both linux/amd64; an index holds one image per platform"},
		{"an index as a child", []ChildMeta{amd, asChild}, "child 1 is an image index; an index's children are images"},
		{"an unknown platform", []ChildMeta{arm, unknown}, `child 1: platform "linux/s390x" is not linux/amd64 or linux/arm64`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN they are indexed
			_, err := Index(tc.children)

			// THEN it is refused, saying why
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
