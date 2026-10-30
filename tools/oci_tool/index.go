package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/spec"
)

// indexSpec is what the oci_image_index rule writes.
type indexSpec struct {
	Repository string `json:"repository,omitempty"`
}

// runIndex builds an image index over images the build assembled. A build
// action: it reads only the children's metadata.
func runIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the index spec, as JSON")
	var childPaths stringList
	fs.Var(&childPaths, "child", "an oci_image's |meta, once per platform")
	indexOut := fs.String("index", "", "where to write the index")
	metaOut := fs.String("meta", "", "where to write the index's metadata")
	digestOut := fs.String("digest", "", "where to write the index's digest")
	fqnOut := fs.String("fqn", "", "where to write <repository>@<digest>, for a spec that names a repository")
	retainOut := fs.String("retain", "", "where to write <repository>:keep-sha256-<hex>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *indexOut == "" || *metaOut == "" || *digestOut == "" {
		return fmt.Errorf("--spec, --index, --meta and --digest are all required")
	}
	var indexSpec indexSpec
	if err := spec.Decode(*specPath, &indexSpec); err != nil {
		return err
	}
	if (indexSpec.Repository == "") != (*fqnOut == "") || (*fqnOut == "") != (*retainOut == "") {
		return fmt.Errorf("a spec's repository, --fqn and --retain come together")
	}
	children := make([]image.ChildMeta, len(childPaths))
	for i, p := range childPaths {
		if err := spec.Decode(p, &children[i]); err != nil {
			return err
		}
	}
	built, err := image.Index(children)
	if err != nil {
		return err
	}
	digest := built.Meta.Manifest.Digest.String()
	if err := os.WriteFile(*indexOut, built.Index, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(*digestOut, []byte(digest), 0o644); err != nil {
		return err
	}
	if err := writeJSON(*metaOut, built.Meta); err != nil {
		return err
	}
	if indexSpec.Repository != "" {
		return writeReferences(indexSpec.Repository, digest, *fqnOut, *retainOut)
	}
	return nil
}

// runPushIndex publishes an image index the build assembled: each child by
// digest with its retention tag, then the index and its own. A runtime
// operation, run by `plz run <index>_push` with the caller's registry
// credentials.
func runPushIndex(args []string) error {
	fs := flag.NewFlagSet("push-index", flag.ContinueOnError)
	indexPath := fs.String("index", "", "the index")
	fqnPath := fs.String("fqn", "", "the file holding the index's reference by digest")
	retainPath := fs.String("retain", "", "the file holding its retention tag")
	var manifests, configs, blobs stringList
	fs.Var(&manifests, "child-manifest", "a child's manifest, once per child")
	fs.Var(&configs, "child-config", "the same child's config, in the same order")
	fs.Var(&blobs, "blob", "a layer blob of any child, once per blob")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *indexPath == "" || *fqnPath == "" || *retainPath == "" {
		return fmt.Errorf("--index, --fqn and --retain are all required")
	}
	if len(manifests) != len(configs) {
		return fmt.Errorf("%d child manifests and %d child configs; each child gives one of each", len(manifests), len(configs))
	}
	ref, err := readDigest(*fqnPath)
	if err != nil {
		return err
	}
	retain, err := readTag(*retainPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*indexPath)
	if err != nil {
		return err
	}
	idx := publish.ImageIndex{Manifest: raw}
	for i := range manifests {
		child, err := publish.Load(manifests[i], configs[i], blobs)
		if err != nil {
			return err
		}
		idx.Children = append(idx.Children, child)
	}
	result, children, err := publish.Index(idx, ref, retain, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	for _, child := range children {
		fmt.Printf("  child %s: image %s, tag %s\n", child.Digest, said(child.UploadedImage, child.ImageExisted), said(child.WroteTag, child.TagExisted))
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s: index %s, tag %s\n", ref, said(result.UploadedImage, result.ImageExisted), said(result.WroteTag, result.TagExisted))
	return nil
}
