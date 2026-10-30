package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/archive"
)

// published serves a registry in memory, behind wrap, and pushes to it an
// index of a linux/amd64 and a linux/arm64 image. It returns the
// repository's name and the index's, the amd64 image's and the arm64
// image's digests.
func published(t *testing.T, wrap func(http.Handler) http.Handler) (repo string, index, amd64, arm64 v1.Hash) {
	t.Helper()
	server := httptest.NewServer(wrap(registry.New()))
	t.Cleanup(server.Close)
	repo = strings.TrimPrefix(server.URL, "http://") + "/library/fixture"
	images := map[string]v1.Image{}
	idx := mutate.IndexMediaType(empty.Index, "application/vnd.oci.image.index.v1+json")
	for _, arch := range []string{"amd64", "arm64"} {
		img, err := random.Image(2048, 2)
		require.NoError(t, err)
		cfg, err := img.ConfigFile()
		require.NoError(t, err)
		cfg.OS, cfg.Architecture = "linux", arch
		img, err = mutate.ConfigFile(img, cfg)
		require.NoError(t, err)
		images[arch] = img
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add:        img,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: arch}},
		})
	}
	ref, err := name.ParseReference(repo + ":fixture")
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(ref, idx))
	index, err = idx.Digest()
	require.NoError(t, err)
	amd64, err = images["amd64"].Digest()
	require.NoError(t, err)
	arm64, err = images["arm64"].Digest()
	require.NoError(t, err)
	return repo, index, amd64, arm64
}

func unchanged(h http.Handler) http.Handler { return h }

// importedDigest is the manifest digest of the one linux/amd64 image the
// importer finds in the layout, as oci_import reads it.
func importedDigest(t *testing.T, dir string) v1.Hash {
	t.Helper()
	_, layers, err := archive.ReadLayout(dir, "linux/amd64")
	require.NoError(t, err)
	require.Len(t, layers, 2)
	index, err := layout.ImageIndexFromPath(dir)
	require.NoError(t, err)
	manifest, err := index.IndexManifest()
	require.NoError(t, err)
	require.Len(t, manifest.Manifests, 1)
	return manifest.Manifests[0].Digest
}

func TestPull_TakesThePlatformsImageFromAnIndex(t *testing.T) {
	// GIVEN a registry holding an index of an amd64 and an arm64 image
	repo, index, amd64, _ := published(t, unchanged)
	out := filepath.Join(t.TempDir(), "layout")

	// WHEN it is pulled by the index's digest for linux/amd64
	err := pull(repo, index.String(), "linux/amd64", out)

	// THEN the layout holds exactly the amd64 image, which the importer reads
	require.NoError(t, err)
	require.Equal(t, amd64, importedDigest(t, out))
}

func TestPull_TakesAnImageByItsOwnDigest(t *testing.T) {
	// GIVEN a registry holding an amd64 image
	repo, _, amd64, _ := published(t, unchanged)
	out := filepath.Join(t.TempDir(), "layout")

	// WHEN it is pulled by its own manifest's digest
	err := pull(repo, amd64.String(), "linux/amd64", out)

	// THEN the layout holds that image
	require.NoError(t, err)
	require.Equal(t, amd64, importedDigest(t, out))
}

func TestPull_RefusesBlobsThatDoNotHashToTheirDigest(t *testing.T) {
	// GIVEN a registry that serves every layer (every blob larger than a
	// config) with its last byte flipped
	repo, index, _, _ := published(t, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/blobs/") {
				h.ServeHTTP(w, r)
				return
			}
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, r)
			body := recorder.Body.Bytes()
			if len(body) > 1024 {
				body[len(body)-1] ^= 0xff
			}
			for key, values := range recorder.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(recorder.Code)
			_, _ = w.Write(body)
		})
	})
	out := filepath.Join(t.TempDir(), "layout")

	// WHEN the index is pulled
	err := pull(repo, index.String(), "linux/amd64", out)

	// THEN it fails on the checksum, naming what was pulled, and the digest
	// the layer's bytes hash to and the one they should
	require.Error(t, err)
	require.Contains(t, err.Error(), index.String())
	require.Regexp(t, `error verifying sha256 checksum .*got "sha256:[0-9a-f]{64}", want "sha256:[0-9a-f]{64}"`, err.Error())
}

func TestPull_RefusesWhatItCannotPullByDigest(t *testing.T) {
	repo, index, _, arm64 := published(t, unchanged)
	absent := "sha256:" + strings.Repeat("0", 64)
	for _, tc := range []struct {
		name     string
		digest   string
		platform string
		want     string
	}{
		{"a digest the registry does not hold", absent, "linux/amd64", absent},
		{"a platform the index does not hold", index.String(), "linux/s390x", "holds 0 images for linux/s390x"},
		{"an image of another platform", arm64.String(), "linux/amd64", "is linux/arm64, not linux/amd64"},
		{"a tag in place of a digest", "fixture", "linux/amd64", "not sha256:<hex>"},
		{"a digest that is not sha256", "sha512:" + strings.Repeat("0", 128), "linux/amd64", "not sha256:<hex>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a registry holding an index of an amd64 and an arm64 image
			out := filepath.Join(t.TempDir(), "layout")

			// WHEN something it cannot serve by that digest is pulled
			err := pull(repo, tc.digest, tc.platform, out)

			// THEN it fails, naming why
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
