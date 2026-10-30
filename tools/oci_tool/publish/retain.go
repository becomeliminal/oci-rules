package publish

import (
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// RetentionTagPrefix starts every retention tag. The tag must not be the
// bare `<alg>-<hex>`: the OCI distribution spec reserves that name (its
// referrers tag schema) for the index of an image's signatures, SBOMs and
// attestations on a registry without the referrers API, so cosign, oras and
// notation would overwrite a retention tag of that name.
const RetentionTagPrefix = "keep-"

// RetentionTag is the tag that keeps the manifest with digest in repo:
// `<repo>:keep-sha256-<hex>`. It is named for the digest, so it can only ever
// name that one manifest.
func RetentionTag(repo name.Repository, digest v1.Hash) (name.Tag, error) {
	return name.NewTag(repo.Name()+":"+RetentionTagPrefix+digest.Algorithm+"-"+digest.Hex, name.StrictValidation)
}
