package sign

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
)

func hashOf(t *testing.T, hex string) v1.Hash {
	t.Helper()
	h, err := v1.NewHash("sha256:" + hex)
	require.NoError(t, err)
	return h
}

func imageMeta(t *testing.T) image.Meta {
	t.Helper()
	return image.Meta{
		SchemaVersion: 1,
		Platform:      "linux/amd64",
		Config:        v1.Descriptor{MediaType: types.OCIConfigJSON, Digest: hashOf(t, "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"), Size: 340},
		Layers: []v1.Descriptor{
			{MediaType: types.OCILayer, Digest: hashOf(t, "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"), Size: 100},
			{MediaType: types.OCILayer, Digest: hashOf(t, "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"), Size: 42},
		},
	}
}

func source() Source {
	return Source{
		BuildType:    "https://example.com/oci-rules/image/v1",
		SourceURI:    "git+https://example.com/repo",
		Commit:       "0123456789abcdef0123456789abcdef01234567",
		BuilderID:    "https://example.com/ci",
		InvocationID: "https://example.com/ci/runs/7",
	}
}

func TestProvenance_NamesTheSourceTheBuilderAndEveryInputByDigest(t *testing.T) {
	// GIVEN an image of a base layer and an app layer, and where it was built
	meta := imageMeta(t)

	// WHEN its provenance is written
	raw, err := Provenance("//app/infra:oci_image", meta, source())
	require.NoError(t, err)

	// THEN it is a SLSA v1 predicate naming the build type, the target, the
	// commit, the config and both layers by digest, and the run that built it
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, map[string]any{
		"buildDefinition": map[string]any{
			"buildType":          "https://example.com/oci-rules/image/v1",
			"externalParameters": map[string]any{"target": "//app/infra:oci_image"},
			"resolvedDependencies": []any{
				map[string]any{"uri": "git+https://example.com/repo", "digest": map[string]any{"gitCommit": "0123456789abcdef0123456789abcdef01234567"}},
				map[string]any{"name": "config", "digest": map[string]any{"sha256": "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"}},
				map[string]any{"name": "layer 0", "digest": map[string]any{"sha256": "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"}},
				map[string]any{"name": "layer 1", "digest": map[string]any{"sha256": "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"}},
			},
		},
		"runDetails": map[string]any{
			"builder":  map[string]any{"id": "https://example.com/ci"},
			"metadata": map[string]any{"invocationId": "https://example.com/ci/runs/7"},
		},
	}, got)

	// AND the same inputs give the same bytes
	again, err := Provenance("//app/infra:oci_image", meta, source())
	require.NoError(t, err)
	require.Equal(t, raw, again)
}

func TestProvenance_RefusesWhatItCannotSayWhereTheImageCameFrom(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		mutate func(*Source)
		want   string
	}{
		{"no commit", "//a:b", func(s *Source) { s.Commit = "" }, "provenance needs a commit"},
		{"no builder or run", "//a:b", func(s *Source) { s.BuilderID, s.InvocationID = "", "" }, "provenance needs a builder ID, a invocation ID"},
		{"no build type", "//a:b", func(s *Source) { s.BuildType = "" }, "provenance needs a build type"},
		{"no target", "", func(*Source) {}, "provenance needs the target that built the image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a source missing something
			src := source()
			tc.mutate(&src)

			// WHEN provenance is written
			_, err := Provenance(tc.target, imageMeta(t), src)

			// THEN it is refused, saying what is missing, never written thinner
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestVerifier_IsExactlyAPublicKeyOrAKeylessIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verifier Verifier
		want     []string
		err      string
	}{
		{"a public key", Verifier{PublicKey: "cosign.pub"}, []string{"--key", "cosign.pub"}, ""},
		{"a keyless identity", Verifier{Identity: "^https://github.com/org/repo/", Issuer: "https://token.actions.githubusercontent.com"},
			[]string{"--certificate-identity-regexp", "^https://github.com/org/repo/", "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com"}, ""},
		{"both", Verifier{PublicKey: "cosign.pub", Identity: "x", Issuer: "y"}, nil, "not both"},
		{"an identity without its issuer", Verifier{Identity: "x"}, nil, "needs both a certificate identity and its OIDC issuer"},
		{"nothing", Verifier{}, nil, "verification needs a public key, or a keyless certificate identity and issuer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN its cosign flags are taken
			args, err := tc.verifier.args()

			// THEN they are exactly one way of verifying, or refused
			if tc.err != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, args)
		})
	}
}

func TestCosign_SignsWithAKeyOnlyWhenOneIsGiven(t *testing.T) {
	// GIVEN cosign with a key and without
	keyed := Cosign{Key: "gcpkms://projects/p/locations/l/keyRings/r/cryptoKeys/k"}
	keyless := Cosign{}

	// WHEN their key flags are taken
	// THEN the keyed one passes its key, and the keyless one none, so cosign
	// signs with the environment's OIDC identity
	require.Equal(t, []string{"--key", "gcpkms://projects/p/locations/l/keyRings/r/cryptoKeys/k"}, keyed.keyArgs())
	require.Nil(t, keyless.keyArgs())
}

// twoPlatforms is an index over an amd64 and an arm64 image, its reference
// in registry.example.com/team/app, and the two images' metadata.
func twoPlatforms(t *testing.T) ([]byte, name.Digest, []image.Meta) {
	t.Helper()
	var metas []image.Meta
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		built, err := image.Build(nil, []image.LayerMeta{{SchemaVersion: 1, MediaType: string(types.OCILayer),
			Digest: "sha256:" + strings.Repeat("ab", 32), Size: 7, DiffID: "sha256:" + strings.Repeat("cd", 32)}},
			image.Spec{Platform: platform, Entrypoint: &[]string{"/" + platform}})
		require.NoError(t, err)
		metas = append(metas, built.Meta)
	}
	index, err := image.Index([]image.ChildMeta{{Meta: metas[0]}, {Meta: metas[1]}})
	require.NoError(t, err)
	ref, err := name.NewDigest("registry.example.com/team/app@" + index.Meta.Manifest.Digest.String())
	require.NoError(t, err)
	return index.Index, ref, metas
}

func TestChildRefs_AreThePublishedIndexsOwnEntries(t *testing.T) {
	// GIVEN a published two-platform index and its children's metadata
	raw, ref, metas := twoPlatforms(t)

	// WHEN the children's references are taken
	refs, err := ChildRefs(raw, ref, metas)

	// THEN each is the index's repository at that child's digest, in the
	// order the children were given
	require.NoError(t, err)
	require.Equal(t, []string{
		"registry.example.com/team/app@" + metas[0].Manifest.Digest.String(),
		"registry.example.com/team/app@" + metas[1].Manifest.Digest.String(),
	}, []string{refs[0].String(), refs[1].String()})
}

func TestChildRefs_RefusesChildrenThatAreNotWhatTheIndexNames(t *testing.T) {
	raw, ref, metas := twoPlatforms(t)
	other := metas[1]
	other.Manifest.Digest = hashOf(t, strings.Repeat("ef", 32))
	resized := metas[1]
	resized.Manifest.Size++
	otherDocument := append(append([]byte{}, raw...), ' ')
	for _, tc := range []struct {
		name     string
		raw      []byte
		children []image.Meta
		want     string
	}{
		{"a child the index doesn't name", raw, []image.Meta{metas[0], other}, "which the index does not name"},
		{"a child the index names differently", raw, []image.Meta{metas[0], resized}, "and the child given is"},
		{"a child the index names, not given", raw, []image.Meta{metas[0]}, "and no child given is it"},
		{"a child given twice", raw, []image.Meta{metas[0], metas[0]}, "is given twice"},
		{"a document other than the one signed", otherDocument, metas, "but the index to sign is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN the children's references are taken
			_, err := ChildRefs(tc.raw, ref, tc.children)

			// THEN nothing is returned: the references would be of something
			// other than what was published
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
