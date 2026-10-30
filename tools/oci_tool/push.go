package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
)

// runPush publishes an image the build assembled: missing blobs, the manifest
// by digest, then its retention tag. It is a runtime operation, run by
// `plz run <image>_push` with the caller's registry credentials, never a
// build action, and it builds nothing.
func runPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "the image's manifest")
	configPath := fs.String("config", "", "the image's config")
	fqnPath := fs.String("fqn", "", "the file holding the image's reference by digest")
	retainPath := fs.String("retain", "", "the file holding its retention tag")
	var blobs stringList
	fs.Var(&blobs, "blob", "a layer blob, once per blob")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" || *configPath == "" || *fqnPath == "" || *retainPath == "" {
		return fmt.Errorf("--manifest, --config, --fqn and --retain are all required")
	}
	ref, err := readDigest(*fqnPath)
	if err != nil {
		return err
	}
	retain, err := readTag(*retainPath)
	if err != nil {
		return err
	}
	img, err := publish.Load(*manifestPath, *configPath, blobs)
	if err != nil {
		return err
	}
	result, err := publish.Publish(img, ref, retain, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}
	fmt.Printf("%s: image %s, tag %s\n", ref, said(result.UploadedImage, result.ImageExisted), said(result.WroteTag, result.TagExisted))
	return nil
}

func said(wrote, existed bool) string {
	switch {
	case wrote:
		return "published"
	case existed:
		return "already present"
	}
	return "unchanged"
}

func readDigest(path string) (name.Digest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return name.Digest{}, err
	}
	return name.NewDigest(strings.TrimSpace(string(raw)), name.StrictValidation)
}

func readTag(path string) (name.Tag, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return name.Tag{}, err
	}
	return name.NewTag(strings.TrimSpace(string(raw)), name.StrictValidation)
}
