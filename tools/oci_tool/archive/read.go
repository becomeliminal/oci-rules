package archive

import (
	"fmt"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/v1/validate"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
)

// importableLayers are the layer media types an imported base may hold: the
// gzipped tars every runtime and registry takes. Anything else (zstd, a
// foreign layer) is refused rather than carried into an image unexamined.
var importableLayers = map[types.MediaType]bool{
	types.OCILayer:    true,
	types.DockerLayer: true,
}

// ReadLayout reads the one image for platform out of an OCI image layout
// another tool wrote (apko's, say) and returns it as a base: its config and
// its layers' descriptors, and the layers themselves, whose compressed bytes
// are the base's blobs. Every blob is checked against its digest, and the
// layout must hold exactly one image for the platform, in its index or one
// index below it; none, or two, is refused rather than chosen between.
func ReadLayout(dir, platform string) (image.BaseMeta, []v1.Layer, error) {
	wantOS, wantArch, ok := strings.Cut(platform, "/")
	if !ok {
		return image.BaseMeta{}, nil, fmt.Errorf("platform %q is not <os>/<architecture>", platform)
	}
	index, err := layout.ImageIndexFromPath(dir)
	if err != nil {
		return image.BaseMeta{}, nil, fmt.Errorf("reading the layout %s: %w", dir, err)
	}
	var found []v1.Image
	if err := collectImages(index, wantOS, wantArch, 0, &found); err != nil {
		return image.BaseMeta{}, nil, err
	}
	if len(found) != 1 {
		return image.BaseMeta{}, nil, fmt.Errorf("the layout %s holds %d images for %s; an imported base is exactly one", dir, len(found), platform)
	}
	img := found[0]
	if err := validate.Image(img); err != nil {
		return image.BaseMeta{}, nil, fmt.Errorf("the image in %s does not validate: %w", dir, err)
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		return image.BaseMeta{}, nil, err
	}
	if cfg.OS != wantOS || cfg.Architecture != wantArch {
		return image.BaseMeta{}, nil, fmt.Errorf("the image's config is %s/%s, but the base is for %s", cfg.OS, cfg.Architecture, platform)
	}
	manifest, err := img.Manifest()
	if err != nil {
		return image.BaseMeta{}, nil, err
	}
	layers, err := img.Layers()
	if err != nil {
		return image.BaseMeta{}, nil, err
	}
	if len(manifest.Layers) == 0 {
		return image.BaseMeta{}, nil, fmt.Errorf("the image in %s has no layers", dir)
	}
	descriptors := make([]v1.Descriptor, len(manifest.Layers))
	for i, layer := range manifest.Layers {
		if !importableLayers[layer.MediaType] {
			return image.BaseMeta{}, nil, fmt.Errorf("layer %d is %s; an imported base's layers are gzipped tars", i, layer.MediaType)
		}
		// Only what a manifest built on it carries: annotations and URLs
		// stay with the image they came from.
		descriptors[i] = v1.Descriptor{MediaType: layer.MediaType, Digest: layer.Digest, Size: layer.Size}
	}
	return image.BaseMeta{SchemaVersion: 1, Platform: platform, Config: *cfg, Layers: descriptors}, layers, nil
}

// collectImages appends every image in index for wantOS/wantArch, descending into
// nested indexes one level: a multi-platform layout is an index of indexes
// at most. A descriptor with no platform is taken, and its config checked.
func collectImages(index v1.ImageIndex, wantOS, wantArch string, depth int, found *[]v1.Image) error {
	manifest, err := index.IndexManifest()
	if err != nil {
		return err
	}
	for _, desc := range manifest.Manifests {
		if desc.Platform != nil && (desc.Platform.OS != wantOS || desc.Platform.Architecture != wantArch) {
			continue
		}
		switch {
		case desc.MediaType.IsImage():
			img, err := index.Image(desc.Digest)
			if err != nil {
				return fmt.Errorf("reading %s: %w", desc.Digest, err)
			}
			*found = append(*found, img)
		case desc.MediaType.IsIndex():
			if depth > 0 {
				return fmt.Errorf("%s is an index two levels down; a layout is an index of indexes at most", desc.Digest)
			}
			child, err := index.ImageIndex(desc.Digest)
			if err != nil {
				return fmt.Errorf("reading %s: %w", desc.Digest, err)
			}
			if err := collectImages(child, wantOS, wantArch, depth+1, found); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s is %s, which is neither an image nor an index", desc.Digest, desc.MediaType)
		}
	}
	return nil
}
