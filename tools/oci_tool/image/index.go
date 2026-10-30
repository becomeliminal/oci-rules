package image

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ChildMeta is an image index child's |meta, read so that an index given as
// a child is recognised and refused rather than misread.
type ChildMeta struct {
	Meta
	// Children is set only on an index's metadata.
	Children []v1.Descriptor `json:"children,omitempty"`
}

// IndexMeta describes a built image index: its own descriptor and its
// children's, in the index's order.
type IndexMeta struct {
	SchemaVersion int             `json:"schema_version"`
	Manifest      v1.Descriptor   `json:"manifest"`
	Children      []v1.Descriptor `json:"children"`
}

// BuiltIndex is an image index's document and metadata.
type BuiltIndex struct {
	Index []byte
	Meta  IndexMeta
}

// Index assembles an OCI image index over single-platform images. Each
// child is named by its manifest's descriptor, with its platform; the
// children are sorted by platform, so the same images give the same index
// whatever order they are given in. Two children of one platform, a child
// that is itself an index, and fewer than two children are refused.
func Index(children []ChildMeta) (BuiltIndex, error) {
	if len(children) < 2 {
		return BuiltIndex{}, fmt.Errorf("an image index is of at least two platforms; %d image(s) given", len(children))
	}
	descriptors := make([]v1.Descriptor, 0, len(children))
	byPlatform := map[string]string{}
	for i, child := range children {
		if child.Children != nil || child.Manifest.MediaType == types.OCIImageIndex {
			return BuiltIndex{}, fmt.Errorf("child %d is an image index; an index's children are images", i)
		}
		if child.SchemaVersion != 1 || child.Manifest.MediaType != types.OCIManifestSchema1 {
			return BuiltIndex{}, fmt.Errorf("child %d is not an oci_image's metadata (schema %d, media type %q)", i, child.SchemaVersion, child.Manifest.MediaType)
		}
		if err := checkPlatform(child.Platform); err != nil {
			return BuiltIndex{}, fmt.Errorf("child %d: %w", i, err)
		}
		if other, dup := byPlatform[child.Platform]; dup {
			return BuiltIndex{}, fmt.Errorf("%s and %s are both %s; an index holds one image per platform", other, child.Manifest.Digest, child.Platform)
		}
		byPlatform[child.Platform] = child.Manifest.Digest.String()
		os, arch, _ := strings.Cut(child.Platform, "/")
		descriptors = append(descriptors, v1.Descriptor{
			MediaType: child.Manifest.MediaType,
			Digest:    child.Manifest.Digest,
			Size:      child.Manifest.Size,
			Platform:  &v1.Platform{OS: os, Architecture: arch},
		})
	}
	sort.Slice(descriptors, func(i, j int) bool {
		a, b := descriptors[i].Platform, descriptors[j].Platform
		if a.OS != b.OS {
			return a.OS < b.OS
		}
		return a.Architecture < b.Architecture
	})

	index, err := json.Marshal(v1.IndexManifest{
		SchemaVersion: 2,
		MediaType:     types.OCIImageIndex,
		Manifests:     descriptors,
	})
	if err != nil {
		return BuiltIndex{}, err
	}
	return BuiltIndex{
		Index: index,
		Meta: IndexMeta{
			SchemaVersion: 1,
			Manifest:      v1.Descriptor{MediaType: types.OCIImageIndex, Digest: mustHash(digestOf(index)), Size: int64(len(index))},
			Children:      descriptors,
		},
	}, nil
}
