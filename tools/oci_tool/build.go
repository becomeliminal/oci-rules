package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/spec"
)

// stringList collects a repeated flag in order.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// runImage builds an image's config and manifest over a base and layers.
func runImage(args []string) error {
	fs := flag.NewFlagSet("image", flag.ContinueOnError)
	basePath := fs.String("base", "", "the base's metadata; none builds from scratch")
	var layerPaths stringList
	fs.Var(&layerPaths, "layer", "an oci_layer's |meta, once per layer, bottom first")
	specPath := fs.String("spec", "", "the image spec, as JSON")
	configOut := fs.String("config", "", "where to write the config")
	manifestOut := fs.String("manifest", "", "where to write the manifest")
	metaOut := fs.String("meta", "", "where to write the image's metadata")
	digestOut := fs.String("digest", "", "where to write the manifest digest")
	baseOut := fs.String("base-out", "", "where to write the image as a base, for an oci_image built on it")
	fqnOut := fs.String("fqn", "", "where to write <repository>@<digest>, for a spec that names a repository")
	retainOut := fs.String("retain", "", "where to write <repository>:keep-sha256-<hex>, the tag that keeps the image in the registry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *configOut == "" || *manifestOut == "" || *metaOut == "" || *digestOut == "" || *baseOut == "" {
		return fmt.Errorf("--spec, --config, --manifest, --meta, --digest and --base-out are all required")
	}

	var base *image.BaseMeta
	if *basePath != "" {
		base = &image.BaseMeta{}
		if err := spec.Decode(*basePath, base); err != nil {
			return err
		}
	}
	layers := make([]image.LayerMeta, len(layerPaths))
	for i, p := range layerPaths {
		if err := spec.Decode(p, &layers[i]); err != nil {
			return err
		}
	}
	var imageSpec image.Spec
	if err := spec.Decode(*specPath, &imageSpec); err != nil {
		return err
	}
	if (imageSpec.Repository == "") != (*fqnOut == "") || (*fqnOut == "") != (*retainOut == "") {
		return fmt.Errorf("a spec's repository, --fqn and --retain come together")
	}
	built, err := image.Build(base, layers, imageSpec)
	if err != nil {
		return err
	}
	digest := built.Meta.Manifest.Digest.String()
	for path, content := range map[string][]byte{
		*configOut:   built.Config,
		*manifestOut: built.Manifest,
		*digestOut:   []byte(digest),
	} {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	if err := writeJSON(*metaOut, built.Meta); err != nil {
		return err
	}
	if err := writeJSON(*baseOut, built.Base); err != nil {
		return err
	}
	if imageSpec.Repository != "" {
		return writeReferences(imageSpec.Repository, digest, *fqnOut, *retainOut)
	}
	return nil
}

// writeReferences writes what a manifest is published as in repository: its
// reference by digest to fqnOut, and its retention tag, which names the
// digest it keeps, to retainOut. The registry's immutable tags hold it there.
func writeReferences(repository, digest, fqnOut, retainOut string) error {
	repo, err := name.NewRepository(repository, name.StrictValidation)
	if err != nil {
		return fmt.Errorf("repository %q is not a registry repository: %w", repository, err)
	}
	ref, err := name.NewDigest(repo.Name()+"@"+digest, name.StrictValidation)
	if err != nil {
		return err
	}
	if err := os.WriteFile(fqnOut, []byte(ref.String()), 0o644); err != nil {
		return err
	}
	hash, err := v1.NewHash(digest)
	if err != nil {
		return err
	}
	tag, err := publish.RetentionTag(repo, hash)
	if err != nil {
		return err
	}
	return os.WriteFile(retainOut, []byte(tag.String()), 0o644)
}
