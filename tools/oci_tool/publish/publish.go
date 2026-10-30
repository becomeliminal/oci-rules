// Package publish uploads an image the build already assembled. It never
// builds anything: the manifest, config and layer blobs are build outputs, and
// what reaches the registry is exactly those bytes.
//
// The order is fixed. Missing blobs, then the manifest by its digest, then the
// retention tag naming that digest. A retention tag that already names
// another image is an error: tags in the registry never move, so publishing
// cannot fix it, and a person must look.
package publish

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Image is a built image's files.
type Image struct {
	Manifest []byte
	Config   []byte
	// Blobs maps a layer digest to the file holding its compressed bytes.
	Blobs map[v1.Hash]string
}

// Load reads an image's manifest and config, and indexes its layer blob files
// by the sha256 of their bytes, so a blob is matched by content, never by name.
func Load(manifestPath, configPath string, blobPaths []string) (Image, error) {
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return Image{}, err
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return Image{}, err
	}
	files, err := expandBlobDirs(blobPaths)
	if err != nil {
		return Image{}, err
	}
	blobs := map[v1.Hash]string{}
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			return Image{}, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return Image{}, err
		}
		blobs[v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(h.Sum(nil))}] = p
	}
	return Image{Manifest: manifest, Config: config, Blobs: blobs}, nil
}

// expandBlobDirs replaces each directory among paths with the files in it,
// in name order: an oci_import's blobs are a directory, as many as its
// image has layers. A directory inside one is refused, not walked.
func expandBlobDirs(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				return nil, fmt.Errorf("%s holds a directory, %s; a blob directory holds only blobs", p, e.Name())
			}
			files = append(files, filepath.Join(p, e.Name()))
		}
	}
	return files, nil
}

// Assemble is the image as go-containerregistry's v1.Image, its manifest and
// config the bytes the build wrote. It fails, as a publish does, on a config
// that is not the manifest's or a layer with no blob.
func (img Image) Assemble() (v1.Image, error) {
	c, err := newCore(img)
	if err != nil {
		return nil, err
	}
	return partial.CompressedToImage(c)
}

// Result is what a publish did, for the caller to report.
type Result struct {
	Digest        string
	ImageExisted  bool
	TagExisted    bool
	UploadedImage bool
	WroteTag      bool
}

// Publish makes the image exist at ref (a reference by digest) and retain (a
// tag in the same repository) points at it.
func Publish(img Image, ref name.Digest, retain name.Tag, options ...remote.Option) (Result, error) {
	core, err := newCore(img)
	if err != nil {
		return Result{}, err
	}
	image, err := partial.CompressedToImage(core)
	if err != nil {
		return Result{}, err
	}
	digest, err := image.Digest()
	if err != nil {
		return Result{}, err
	}
	if digest.String() != ref.DigestStr() {
		return Result{}, fmt.Errorf("the manifest's digest is %s, but the reference to publish names %s", digest, ref.DigestStr())
	}
	if ref.Context().Name() != retain.Context().Name() {
		return Result{}, fmt.Errorf("retain %s is not in the image's repository %s", retain, ref.Context().Name())
	}
	result := Result{Digest: digest.String()}

	existing, found, err := head(ref, options...)
	if err != nil {
		return result, err
	}
	result.ImageExisted = found
	if found && existing != digest.String() {
		return result, fmt.Errorf("%s resolves to %s; a registry serving another digest under a digest reference is broken", ref, existing)
	}
	tagged, tagFound, err := head(retain, options...)
	if err != nil {
		return result, err
	}
	result.TagExisted = tagFound
	if tagFound && tagged != digest.String() {
		return result, fmt.Errorf("retain %s already names %s, but this image is %s; a retention tag never moves, so this needs a person", retain, tagged, digest)
	}

	if !found {
		// Uploads only the blobs the registry lacks, then the manifest.
		if err := remote.Write(ref, image, options...); err != nil {
			return result, fmt.Errorf("publishing %s: %w", ref, err)
		}
		result.UploadedImage = true
	}
	if !tagFound {
		if err := remote.Tag(retain, image, options...); err != nil {
			return result, fmt.Errorf("tagging %s as %s: %w", ref, retain, err)
		}
		result.WroteTag = true
	}

	// Verified, never assumed: both references resolve to this digest.
	for _, r := range []name.Reference{ref, retain} {
		got, ok, err := head(r, options...)
		if err != nil {
			return result, err
		}
		if !ok || got != digest.String() {
			return result, fmt.Errorf("after publishing, %s resolves to %q (found %v), not %s", r, got, ok, digest)
		}
	}
	return result, nil
}

// head asks the registry what a reference resolves to. Only a 404 is absent;
// any other failure, an authorisation error included, is an error.
func head(ref name.Reference, options ...remote.Option) (string, bool, error) {
	descriptor, err := remote.Head(ref, options...)
	if err == nil {
		return descriptor.Digest.String(), true, nil
	}
	var answered *transport.Error
	if errors.As(err, &answered) && answered.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	return "", false, fmt.Errorf("looking up %s: %w", ref, err)
}

// core is the image as go-containerregistry needs it, over the build's files.
type core struct {
	img      Image
	manifest *v1.Manifest
}

func newCore(img Image) (*core, error) {
	manifest, err := v1.ParseManifest(bytes.NewReader(img.Manifest))
	if err != nil {
		return nil, fmt.Errorf("parsing the manifest: %w", err)
	}
	sum := sha256.Sum256(img.Config)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != manifest.Config.Digest.String() {
		return nil, fmt.Errorf("the config is %s, but the manifest names %s", got, manifest.Config.Digest)
	}
	for _, layer := range manifest.Layers {
		if _, ok := img.Blobs[layer.Digest]; !ok {
			return nil, fmt.Errorf("no blob was given for layer %s", layer.Digest)
		}
	}
	return &core{img: img, manifest: manifest}, nil
}

func (c *core) RawConfigFile() ([]byte, error)      { return c.img.Config, nil }
func (c *core) RawManifest() ([]byte, error)        { return c.img.Manifest, nil }
func (c *core) MediaType() (types.MediaType, error) { return c.manifest.MediaType, nil }

func (c *core) LayerByDigest(h v1.Hash) (partial.CompressedLayer, error) {
	if h == c.manifest.Config.Digest {
		return &blob{digest: h, size: c.manifest.Config.Size, mediaType: c.manifest.Config.MediaType, bytes: c.img.Config}, nil
	}
	for _, layer := range c.manifest.Layers {
		if layer.Digest == h {
			return &blob{digest: h, size: layer.Size, mediaType: layer.MediaType, path: c.img.Blobs[h]}, nil
		}
	}
	return nil, fmt.Errorf("the manifest has no blob %s", h)
}

// blob is one layer or the config, from a file or from memory.
type blob struct {
	digest    v1.Hash
	size      int64
	mediaType types.MediaType
	path      string
	bytes     []byte
}

func (b *blob) Digest() (v1.Hash, error)            { return b.digest, nil }
func (b *blob) Size() (int64, error)                { return b.size, nil }
func (b *blob) MediaType() (types.MediaType, error) { return b.mediaType, nil }
func (b *blob) Compressed() (io.ReadCloser, error) {
	if b.path == "" {
		return io.NopCloser(bytes.NewReader(b.bytes)), nil
	}
	return os.Open(b.path)
}
