package publish

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ImageIndex is a built image index's files: its document and its children, each
// a built image.
type ImageIndex struct {
	Manifest []byte
	Children []Image
}

// rawIndex is an index document as go-containerregistry puts it: the bytes
// the build wrote, under the index media type.
type rawIndex struct{ raw []byte }

func (r rawIndex) RawManifest() ([]byte, error)        { return r.raw, nil }
func (r rawIndex) MediaType() (types.MediaType, error) { return types.OCIImageIndex, nil }

// childReference is the digest reference and retention tag a child is
// published under, in the index's repository.
func childReference(repo name.Repository, digest v1.Hash) (name.Digest, name.Tag, error) {
	ref, err := name.NewDigest(repo.Name()+"@"+digest.String(), name.StrictValidation)
	if err != nil {
		return name.Digest{}, name.Tag{}, err
	}
	tag, err := RetentionTag(repo, digest)
	if err != nil {
		return name.Digest{}, name.Tag{}, err
	}
	return ref, tag, nil
}

// Index makes every child image exist in the index's repository, by
// digest and under its own retention tag, then the index at ref and its
// retention tag retain. A child is tagged as well as referenced, so that a
// registry's cleanup of untagged manifests can never take one from under its
// index.
//
// Nothing is written until every retention tag, the index's and each
// child's, has been looked at: one that already names another digest stops
// the publish before the registry changes.
func Index(idx ImageIndex, ref name.Digest, retain name.Tag, options ...remote.Option) (Result, []Result, error) {
	if ref.Context().Name() != retain.Context().Name() {
		return Result{}, nil, fmt.Errorf("retain %s is not in the index's repository %s", retain, ref.Context().Name())
	}
	manifest, err := v1.ParseIndexManifest(bytes.NewReader(idx.Manifest))
	if err != nil {
		return Result{}, nil, fmt.Errorf("parsing the index: %w", err)
	}
	digest := v1.Hash{Algorithm: "sha256", Hex: sha256Hex(idx.Manifest)}
	if digest.String() != ref.DigestStr() {
		return Result{}, nil, fmt.Errorf("the index's digest is %s, but the reference to publish names %s", digest, ref.DigestStr())
	}
	if len(manifest.Manifests) != len(idx.Children) {
		return Result{}, nil, fmt.Errorf("the index names %d images, and %d were given", len(manifest.Manifests), len(idx.Children))
	}
	// Each child is given once and holds together (its config is its
	// manifest's and every layer has its blob) before anything is written:
	// Publish checks the same, but only as it reaches each child, after the
	// ones before it are in the registry.
	byDigest := map[v1.Hash]Image{}
	for i, child := range idx.Children {
		childDigest := v1.Hash{Algorithm: "sha256", Hex: sha256Hex(child.Manifest)}
		if _, dup := byDigest[childDigest]; dup {
			return Result{}, nil, fmt.Errorf("child manifest %s is given twice; each image of an index is given once", childDigest)
		}
		if _, err := newCore(child); err != nil {
			return Result{}, nil, fmt.Errorf("child %d (%s): %w", i, childDigest, err)
		}
		byDigest[childDigest] = child
	}

	type planned struct {
		img    Image
		ref    name.Digest
		retain name.Tag
	}
	var children []planned
	named := map[v1.Hash]bool{}
	for _, descriptor := range manifest.Manifests {
		if named[descriptor.Digest] {
			return Result{}, nil, fmt.Errorf("the index names %s twice; an index names each image once", descriptor.Digest)
		}
		named[descriptor.Digest] = true
		child, ok := byDigest[descriptor.Digest]
		if !ok {
			return Result{}, nil, fmt.Errorf("the index names %s, and no image given is it", descriptor.Digest)
		}
		if int64(len(child.Manifest)) != descriptor.Size {
			return Result{}, nil, fmt.Errorf("the index says %s is %d bytes, and its manifest is %d", descriptor.Digest, descriptor.Size, len(child.Manifest))
		}
		childRef, childRetain, err := childReference(ref.Context(), descriptor.Digest)
		if err != nil {
			return Result{}, nil, err
		}
		children = append(children, planned{img: child, ref: childRef, retain: childRetain})
	}

	// Every retention tag first, before anything is written.
	checks := []struct {
		tag    name.Tag
		digest string
	}{{retain, digest.String()}}
	for _, child := range children {
		checks = append(checks, struct {
			tag    name.Tag
			digest string
		}{child.retain, child.ref.DigestStr()})
	}
	var refused []string
	for _, check := range checks {
		tagged, found, err := head(check.tag, options...)
		if err != nil {
			return Result{}, nil, err
		}
		if found && tagged != check.digest {
			refused = append(refused, fmt.Sprintf("retain %s already names %s, but this is %s", check.tag, tagged, check.digest))
		}
	}
	if len(refused) > 0 {
		return Result{}, nil, fmt.Errorf("a retention tag never moves, so this needs a person, and nothing was written:\n  %s", strings.Join(refused, "\n  "))
	}

	var childResults []Result
	for _, child := range children {
		result, err := Publish(child.img, child.ref, child.retain, options...)
		if err != nil {
			return Result{}, childResults, fmt.Errorf("publishing child %s: %w", child.ref, err)
		}
		childResults = append(childResults, result)
	}

	result := Result{Digest: digest.String()}
	existing, found, err := head(ref, options...)
	if err != nil {
		return result, childResults, err
	}
	result.ImageExisted = found
	if found && existing != digest.String() {
		return result, childResults, fmt.Errorf("%s resolves to %s; a registry serving another digest under a digest reference is broken", ref, existing)
	}
	if !found {
		if err := remote.Put(ref, rawIndex{idx.Manifest}, options...); err != nil {
			return result, childResults, fmt.Errorf("publishing %s: %w", ref, err)
		}
		result.UploadedImage = true
	}
	_, tagFound, err := head(retain, options...)
	if err != nil {
		return result, childResults, err
	}
	result.TagExisted = tagFound
	if !tagFound {
		if err := remote.Put(retain, rawIndex{idx.Manifest}, options...); err != nil {
			return result, childResults, fmt.Errorf("tagging %s as %s: %w", ref, retain, err)
		}
		result.WroteTag = true
	}

	// Verified, never assumed: the index's references resolve to it, and
	// each child's to the child.
	for _, r := range []name.Reference{ref, retain} {
		got, ok, err := head(r, options...)
		if err != nil {
			return result, childResults, err
		}
		if !ok || got != digest.String() {
			return result, childResults, fmt.Errorf("after publishing, %s resolves to %q (found %v), not %s", r, got, ok, digest)
		}
	}
	return result, childResults, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
