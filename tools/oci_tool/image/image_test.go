package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

func sha(b string) string {
	sum := sha256.Sum256([]byte(b))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hash(t *testing.T, s string) v1.Hash {
	t.Helper()
	h, err := v1.NewHash(s)
	require.NoError(t, err)
	return h
}

// threeLayerConfig is an image's config as a registry stores it: three
// layers, the third added by an image built on top of a two-layer base, with
// an empty-layer entry between the first two.
func threeLayerConfig(t *testing.T) []byte {
	t.Helper()
	cfg := v1.ConfigFile{
		Architecture: "amd64",
		OS:           "linux",
		Config: v1.Config{
			Env:        []string{"PATH=/usr/bin:/bin", "LANG=C"},
			Entrypoint: []string{"./app"},
			Cmd:        []string{"sh"},
			WorkingDir: "/",
		},
		RootFS: v1.RootFS{Type: "layers", DiffIDs: []v1.Hash{hash(t, sha("d0")), hash(t, sha("d1")), hash(t, sha("d2"))}},
		History: []v1.History{
			{CreatedBy: "ADD rootfs"},
			{CreatedBy: "CMD sh", EmptyLayer: true},
			{CreatedBy: "RUN add-packages"},
			{CreatedBy: "COPY app"},
			{CreatedBy: "ENTRYPOINT ./app", EmptyLayer: true},
		},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return raw
}

// pulledBase is the bottom layers of threeLayerConfig's image as an imported
// base: its config with only those layers' diff IDs and the history that
// made them, and their descriptors.
func pulledBase(t *testing.T, config []byte, layers int) BaseMeta {
	t.Helper()
	var cfg v1.ConfigFile
	require.NoError(t, json.Unmarshal(config, &cfg))
	cfg.RootFS.DiffIDs = cfg.RootFS.DiffIDs[:layers]
	var history []v1.History
	seen := 0
	for _, h := range cfg.History {
		if !h.EmptyLayer {
			if seen == layers {
				break
			}
			seen++
		}
		history = append(history, h)
	}
	cfg.History = history
	base := BaseMeta{SchemaVersion: 1, Platform: "linux/amd64", Config: cfg}
	for i := 0; i < layers; i++ {
		base.Layers = append(base.Layers, v1.Descriptor{
			MediaType: types.OCILayer,
			Digest:    hash(t, sha("blob"+string(rune('0'+i)))),
			Size:      int64(100 + i),
		})
	}
	return base
}

func appLayer() LayerMeta {
	return LayerMeta{SchemaVersion: 1, MediaType: string(types.OCILayer), Digest: sha("app-blob"), Size: 42, DiffID: sha("app-diff")}
}

func strs(s ...string) *[]string { return &s }

func TestBuild_AppendsLayersAndAppliesTheSpecOverTheBase(t *testing.T) {
	// GIVEN a two-layer base whose config has an entrypoint and env
	config := threeLayerConfig(t)
	base := pulledBase(t, config, 2)

	// WHEN an image adds a layer, a new entrypoint and an env value
	built, err := Build(&base, []LayerMeta{appLayer()}, Spec{
		Platform:   "linux/amd64",
		Entrypoint: strs("/app"),
		Env:        map[string]string{"LANG": "C.UTF-8", "TZ": "UTC"},
	})
	require.NoError(t, err)

	// THEN the config carries every base layer then the new one, epoch
	// times, the entrypoint, the env merged in place, and no inherited cmd
	var cfg v1.ConfigFile
	require.NoError(t, json.Unmarshal(built.Config, &cfg))
	require.Equal(t, []v1.Hash{hash(t, sha("d0")), hash(t, sha("d1")), hash(t, sha("app-diff"))}, cfg.RootFS.DiffIDs)
	require.Equal(t, []string{"/app"}, cfg.Config.Entrypoint)
	require.Nil(t, cfg.Config.Cmd)
	require.Equal(t, []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "TZ=UTC"}, cfg.Config.Env)
	require.Equal(t, time.Unix(0, 0).UTC(), cfg.Created.Time.UTC())
	require.Equal(t, v1.History{Created: v1.Time{Time: time.Unix(0, 0).UTC()}, CreatedBy: "oci_layer"}, cfg.History[len(cfg.History)-1])

	// AND the manifest lists the base's layers then the new one
	var manifest v1.Manifest
	require.NoError(t, json.Unmarshal(built.Manifest, &manifest))
	require.Equal(t, append(append([]v1.Descriptor{}, base.Layers...),
		v1.Descriptor{MediaType: types.OCILayer, Digest: hash(t, sha("app-blob")), Size: 42}), manifest.Layers)
	require.Equal(t, built.Meta.Config, manifest.Config)
	require.Equal(t, sha(string(built.Manifest)), built.Meta.Manifest.Digest.String())
}

// asBase is an image's |base output as the next build reads it: through JSON.
func asBase(t *testing.T, built Built) *BaseMeta {
	encoded, err := json.Marshal(built.Base)
	require.NoError(t, err)
	var base BaseMeta
	require.NoError(t, json.Unmarshal(encoded, &base))
	return &base
}

func TestBuild_AnImageIsABaseForAnother(t *testing.T) {
	// GIVEN an image built on a two-layer pulled base, with an entrypoint
	config := threeLayerConfig(t)
	pulled := pulledBase(t, config, 2)
	shared, err := Build(&pulled, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", Entrypoint: strs("/app")})
	require.NoError(t, err)
	top := appLayer()
	top.Digest, top.DiffID, top.Size = sha("top-blob"), sha("top-diff"), 7

	// WHEN one image is built on it with nothing of its own but a spec that
	// changes nothing, and another adds a layer and an env value
	same, err := Build(asBase(t, shared), nil, Spec{Platform: "linux/amd64"})
	require.NoError(t, err)
	stacked, err := Build(asBase(t, shared), []LayerMeta{top}, Spec{Platform: "linux/amd64", Env: map[string]string{"TZ": "UTC"}})
	require.NoError(t, err)

	// THEN the first is the base image exactly: its base output loses nothing
	require.Equal(t, string(shared.Config), string(same.Config))
	require.Equal(t, shared.Meta.Manifest.Digest, same.Meta.Manifest.Digest)

	// AND the second has the pulled base's layers, then the shared image's,
	// then its own, in order, and keeps the entrypoint it inherits
	var manifest v1.Manifest
	require.NoError(t, json.Unmarshal(stacked.Manifest, &manifest))
	require.Equal(t, append(append([]v1.Descriptor{}, pulled.Layers...),
		v1.Descriptor{MediaType: types.OCILayer, Digest: hash(t, sha("app-blob")), Size: 42},
		v1.Descriptor{MediaType: types.OCILayer, Digest: hash(t, sha("top-blob")), Size: 7},
	), manifest.Layers)
	var cfg v1.ConfigFile
	require.NoError(t, json.Unmarshal(stacked.Config, &cfg))
	require.Equal(t, []v1.Hash{hash(t, sha("d0")), hash(t, sha("d1")), hash(t, sha("app-diff")), hash(t, sha("top-diff"))}, cfg.RootFS.DiffIDs)
	require.Equal(t, []string{"/app"}, cfg.Config.Entrypoint)
	require.Equal(t, []string{"PATH=/usr/bin:/bin", "LANG=C", "TZ=UTC"}, cfg.Config.Env)
}

func TestBuild_RefusesAnImageBaseOfAnotherPlatform(t *testing.T) {
	// GIVEN an arm64 image
	shared, err := Build(nil, []LayerMeta{appLayer()}, Spec{Platform: "linux/arm64"})
	require.NoError(t, err)

	// WHEN an amd64 image is built on it
	_, err = Build(asBase(t, shared), nil, Spec{Platform: "linux/amd64"})

	// THEN it is refused, naming both platforms
	require.Error(t, err)
	require.Contains(t, err.Error(), "the image is linux/amd64 but its base is linux/arm64")
}

func TestBuild_TheDigestChangesWithEveryInputAndWithNothingElse(t *testing.T) {
	// GIVEN a base and a layer
	config := threeLayerConfig(t)
	base := pulledBase(t, config, 2)
	spec := Spec{Platform: "linux/amd64", Entrypoint: strs("/app")}
	digest := func(b *BaseMeta, layers []LayerMeta, s Spec) string {
		built, err := Build(b, layers, s)
		require.NoError(t, err)
		return built.Meta.Manifest.Digest.String()
	}
	original := digest(&base, []LayerMeta{appLayer()}, spec)

	// WHEN the same image is built again, and then with each input changed
	again := digest(&base, []LayerMeta{appLayer()}, spec)
	otherLayer := appLayer()
	otherLayer.Digest = sha("other-blob")
	otherBase := pulledBase(t, config, 1)
	changed := map[string]string{
		"a layer's bytes": digest(&base, []LayerMeta{otherLayer}, spec),
		"the base":        digest(&otherBase, []LayerMeta{appLayer()}, spec),
		"the entrypoint":  digest(&base, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", Entrypoint: strs("/other")}),
		"the env":         digest(&base, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", Entrypoint: strs("/app"), Env: map[string]string{"A": "1"}}),
	}

	// THEN the rebuild is identical, and each change gives a new identity
	require.Equal(t, original, again)
	for input, d := range changed {
		require.NotEqual(t, original, d, input)
	}
}

func TestBuild_RefusesWhatItCannotBuildHonestly(t *testing.T) {
	config := threeLayerConfig(t)
	base := pulledBase(t, config, 2)
	foreign := appLayer()
	foreign.MediaType = string(types.DockerLayer)
	for _, tc := range []struct {
		name   string
		base   *BaseMeta
		layers []LayerMeta
		spec   Spec
		want   string
	}{
		{"a platform the base is not", &base, []LayerMeta{appLayer()}, Spec{Platform: "linux/arm64"}, "the image is linux/arm64 but its base is linux/amd64"},
		{"an unknown platform", nil, []LayerMeta{appLayer()}, Spec{Platform: "windows/amd64"}, "is not linux/amd64 or linux/arm64"},
		{"nothing at all", nil, nil, Spec{Platform: "linux/amd64"}, "needs a base or at least one layer"},
		{"a layer that is not an oci_layer's", nil, []LayerMeta{foreign}, Spec{Platform: "linux/amd64"}, "is not an oci_layer's metadata"},
		{"a relative workdir", nil, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", WorkingDir: func() *string { s := "app"; return &s }()}, "must be absolute"},
		{"an env key with =", nil, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", Env: map[string]string{"A=B": "1"}}, "is not a valid name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN it is built
			_, err := Build(tc.base, tc.layers, tc.spec)

			// THEN it is refused
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestBuild_SetsTheStopSignal(t *testing.T) {
	// GIVEN an image that stops on SIGQUIT, and one with no stop signal of
	// its own
	sigquit := "SIGQUIT"
	stopping, err := Build(nil, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", StopSignal: &sigquit})
	require.NoError(t, err)

	// WHEN an image is built on the first without a stop signal of its own
	inherited, err := Build(asBase(t, stopping), nil, Spec{Platform: "linux/amd64"})
	require.NoError(t, err)

	// THEN both stop on SIGQUIT
	for _, built := range []Built{stopping, inherited} {
		var cfg v1.ConfigFile
		require.NoError(t, json.Unmarshal(built.Config, &cfg))
		require.Equal(t, "SIGQUIT", cfg.Config.StopSignal)
	}
}

func TestBuild_RefusesAStopSignalThatIsNotASignalName(t *testing.T) {
	// GIVEN a stop signal given as a number
	three := "3"

	// WHEN the image is built
	_, err := Build(nil, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64", StopSignal: &three})

	// THEN it is refused
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a signal name")
}

func TestBuild_RefusesALayerHoldingABinaryForAnotherPlatform(t *testing.T) {
	// GIVEN a layer whose executable is darwin/arm64
	layer := appLayer()
	layer.Binaries = map[string]string{"/app": "darwin/arm64"}

	// WHEN a linux/amd64 image is built from it
	_, err := Build(nil, []LayerMeta{layer}, Spec{Platform: "linux/amd64"})

	// THEN it is refused, naming the layer, the file and both platforms
	require.Error(t, err)
	require.Equal(t, "layer 0: /app is built for darwin/arm64, but the image is linux/amd64; build it for linux/amd64 (oci_layer's platform)", err.Error())
}

func TestBuild_AcceptsLayersWhoseBinariesAreTheImagesPlatform(t *testing.T) {
	// GIVEN a layer whose executable is linux/amd64
	layer := appLayer()
	layer.Binaries = map[string]string{"/app": "linux/amd64"}

	// WHEN a linux/amd64 image is built from it
	built, err := Build(nil, []LayerMeta{layer}, Spec{Platform: "linux/amd64"})

	// THEN it is built, and the record of its binaries leaves the image as it
	// would be without it
	require.NoError(t, err)
	without, err := Build(nil, []LayerMeta{appLayer()}, Spec{Platform: "linux/amd64"})
	require.NoError(t, err)
	require.Equal(t, without.Meta.Manifest.Digest, built.Meta.Manifest.Digest)
}
