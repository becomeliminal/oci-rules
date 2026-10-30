package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// runPull downloads a public image by digest into an OCI image layout, as
// rules_oci's oci.pull does: the registry's token handshake, anonymously,
// and nothing a tag could change. It is the one build action that reaches a
// registry, and it holds no credential. oci_import reads the layout.
func runPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	image := fs.String("image", "", "the image's repository (docker.io/library/postgres, ghcr.io/actions/actions-runner)")
	digest := fs.String("digest", "", "its digest, sha256:<hex>: of an image, or of an index to take --platform from")
	platform := fs.String("platform", "", "the platform to take (linux/amd64)")
	out := fs.String("out", "", "the layout directory to write; it must not exist")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *image == "" || *digest == "" || *platform == "" || *out == "" {
		return fmt.Errorf("--image, --digest, --platform and --out are all required")
	}
	return pull(*image, *digest, *platform, *out, remote.WithAuth(authn.Anonymous))
}

// pull writes the image for platform at digest into a layout at out. The
// document at digest is checked against it, the image's own manifest against
// the index's descriptor, and each blob against its digest as it is written,
// so a registry that serves the wrong bytes fails the build, naming them.
func pull(image, digest, platform, out string, options ...remote.Option) error {
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+sha256.Size*2 {
		return fmt.Errorf("%s@%s: the digest is not sha256:<hex>", image, digest)
	}
	ref, err := name.NewDigest(image+"@"+digest, name.StrictValidation)
	if err != nil {
		return fmt.Errorf("%s@%s: %w", image, digest, err)
	}
	want, err := v1.ParsePlatform(platform)
	if err != nil {
		return fmt.Errorf("--platform: %w", err)
	}
	desc, err := remote.Get(ref, options...)
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}
	sum := sha256.Sum256(desc.Manifest)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != digest {
		return fmt.Errorf("%s: the registry served a document hashing to %s", ref, got)
	}

	var img v1.Image
	switch {
	case desc.MediaType.IsIndex():
		index, err := desc.ImageIndex()
		if err != nil {
			return fmt.Errorf("%s: %w", ref, err)
		}
		manifest, err := index.IndexManifest()
		if err != nil {
			return fmt.Errorf("%s: %w", ref, err)
		}
		var matches []v1.Descriptor
		for _, child := range manifest.Manifests {
			if child.MediaType.IsImage() && child.Platform != nil && child.Platform.Satisfies(*want) {
				matches = append(matches, child)
			}
		}
		if len(matches) != 1 {
			return fmt.Errorf("%s: the index holds %d images for %s; a pull takes exactly one", ref, len(matches), platform)
		}
		img, err = index.Image(matches[0].Digest)
		if err != nil {
			return fmt.Errorf("%s for %s: %w", ref, platform, err)
		}
	case desc.MediaType.IsImage():
		img, err = desc.Image()
		if err != nil {
			return fmt.Errorf("%s: %w", ref, err)
		}
	default:
		return fmt.Errorf("%s is %s, neither an image nor an index", ref, desc.MediaType)
	}
	config, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}
	if config.Platform() == nil || !config.Platform().Satisfies(*want) {
		return fmt.Errorf("%s is %s/%s, not %s", ref, config.OS, config.Architecture, platform)
	}

	// The layout's writer reads each blob from the registry through a reader
	// that fails on bytes that do not hash to the blob's digest.
	path, err := layout.Write(out, empty.Index)
	if err != nil {
		return err
	}
	if err := path.AppendImage(img, layout.WithPlatform(*want)); err != nil {
		return fmt.Errorf("%s for %s: %w", ref, platform, err)
	}
	return nil
}
