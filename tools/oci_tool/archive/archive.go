// Package archive writes a built image in the forms other tools read: a
// Docker archive (`docker load`, `minikube image load`, container-structure-
// test's tar driver), an OCI image layout, and an OCI archive, which is that
// layout as a tar.
//
// Each is a build output, so each is deterministic. The image's blobs are the
// build's own bytes, never recompressed, and go-containerregistry writes the
// documents around them; this package fixes what it leaves to the host (file
// modes, and the order and times of a tar's entries).
package archive

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/v1/validate"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
)

// RefNameAnnotation names an image in a layout's index, as the OCI image
// layout specification defines it.
const RefNameAnnotation = "org.opencontainers.image.ref.name"

// layerExtensions is the file extension a Docker archive gives each layer
// media type, as go-containerregistry names them. A media type not here is
// refused: an archive that guessed an extension would not load.
var layerExtensions = map[types.MediaType]string{
	types.OCILayer:                       ".tar.gz",
	types.DockerLayer:                    ".tar.gz",
	types.OCILayerZStd:                   ".tar.zst",
	types.OCIUncompressedLayer:           ".tar",
	types.DockerUncompressedLayer:        ".tar",
	types.OCIUncompressedRestrictedLayer: ".tar",
}

// WriteDocker writes img to w as a Docker archive that loads as each of
// tags. A Docker archive names what it loads, so at least one tag is
// required. The archive lists the tags sorted, so its bytes depend on which
// tags there are and never on the order they were given in: a BUILD file's
// formatter sorts the list anyway, and `docker load` loads every tag alike.
//
// It writes go-containerregistry's format, entry for entry: the config, each
// distinct layer, then manifest.json. go-containerregistry's own writer is
// not used, because it collects an image's tags by ranging over a map, so an
// archive with two tags would list them in a different order from one build
// to the next.
func WriteDocker(img publish.Image, tags []name.Tag, w io.Writer) error {
	if len(tags) == 0 {
		return fmt.Errorf("a Docker archive loads as at least one tag, and none was given")
	}
	repoTags := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		ts := tag.String()
		if seen[ts] {
			return fmt.Errorf("tag %s is given twice", ts)
		}
		seen[ts] = true
		repoTags = append(repoTags, ts)
	}
	sort.Strings(repoTags)
	assembled, err := img.Assemble()
	if err != nil {
		return err
	}
	cfgName, err := assembled.ConfigName()
	if err != nil {
		return err
	}
	cfg, err := assembled.RawConfigFile()
	if err != nil {
		return err
	}
	layers, err := assembled.Layers()
	if err != nil {
		return err
	}

	tw := tar.NewWriter(w)
	if err := writeEntry(tw, cfgName.String(), bytes.NewReader(cfg), int64(len(cfg))); err != nil {
		return err
	}
	layerFiles := make([]string, len(layers))
	written := map[string]bool{}
	for i, layer := range layers {
		digest, err := layer.Digest()
		if err != nil {
			return err
		}
		mediaType, err := layer.MediaType()
		if err != nil {
			return err
		}
		if !mediaType.IsDistributable() {
			return fmt.Errorf("layer %s is %s, a foreign layer, which this archive does not carry", digest, mediaType)
		}
		extension, ok := layerExtensions[mediaType]
		if !ok {
			return fmt.Errorf("layer %s has media type %s, which a Docker archive does not name", digest, mediaType)
		}
		layerFiles[i] = digest.Hex + extension
		if written[layerFiles[i]] {
			continue
		}
		written[layerFiles[i]] = true
		size, err := layer.Size()
		if err != nil {
			return err
		}
		rc, err := layer.Compressed()
		if err != nil {
			return err
		}
		err = writeEntry(tw, layerFiles[i], rc, size)
		rc.Close()
		if err != nil {
			return err
		}
	}
	manifest, err := json.Marshal(tarball.Manifest{{Config: cfgName.String(), RepoTags: repoTags, Layers: layerFiles}})
	if err != nil {
		return err
	}
	if err := writeEntry(tw, "manifest.json", bytes.NewReader(manifest), int64(len(manifest))); err != nil {
		return err
	}
	return tw.Close()
}

// writeEntry writes one file into a Docker archive with the header
// go-containerregistry gives it: 0644, and no owner or time.
func writeEntry(tw *tar.Writer, name string, r io.Reader, size int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg, Size: size}); err != nil {
		return err
	}
	_, err := io.Copy(tw, r)
	return err
}

// WriteLayout writes img as an OCI image layout in dir, which must not exist:
// `oci-layout`, `index.json` naming the image, and every blob under
// `blobs/sha256`. refName, when set, is the index entry's ref.name.
//
// The layout is then read back and validated end to end: every descriptor's
// digest and size against its blob, and every layer's diff ID against its
// uncompressed bytes, so a layout that a reader would reject is never an
// output.
func WriteLayout(img publish.Image, refName string, dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists; a layout is written into a new directory", dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	assembled, err := img.Assemble()
	if err != nil {
		return err
	}
	path, err := layout.Write(dir, empty.Index)
	if err != nil {
		return fmt.Errorf("starting the layout: %w", err)
	}
	var options []layout.Option
	if refName != "" {
		options = append(options, layout.WithAnnotations(map[string]string{RefNameAnnotation: refName}))
	}
	if err := path.AppendImage(assembled, options...); err != nil {
		return fmt.Errorf("writing the image into the layout: %w", err)
	}
	if err := fixModes(dir); err != nil {
		return err
	}

	index, err := layout.ImageIndexFromPath(dir)
	if err != nil {
		return fmt.Errorf("reading the layout back: %w", err)
	}
	digest, err := assembled.Digest()
	if err != nil {
		return err
	}
	written, err := index.Image(digest)
	if err != nil {
		return fmt.Errorf("reading the image back from the layout: %w", err)
	}
	if err := validate.Image(written); err != nil {
		return fmt.Errorf("the layout written does not validate: %w", err)
	}
	return nil
}

// WriteOCI writes img's OCI image layout to w as a tar. The entries are in
// path order, every directory before what it holds, with the epoch for every
// time, 0:0 owners, and 0644 files and 0755 directories, so the same image
// gives the same bytes on any host.
func WriteOCI(img publish.Image, refName string, w io.Writer) error {
	staging, err := os.MkdirTemp("", "oci-archive-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	dir := filepath.Join(staging, "layout")
	if err := WriteLayout(img, refName, dir); err != nil {
		return err
	}

	var paths []string
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)

	epoch := time.Unix(0, 0).UTC()
	tw := tar.NewWriter(w)
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hdr := &tar.Header{Name: rel, ModTime: epoch}
		switch {
		case info.IsDir():
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
			hdr.Mode = 0o755
		case info.Mode().IsRegular():
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = 0o644
			hdr.Size = info.Size()
		default:
			return fmt.Errorf("%s in the layout is neither a file nor a directory", rel)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	return tw.Close()
}

// fixModes gives every file in dir 0644 and every directory 0755.
// go-containerregistry creates them with whatever the umask leaves.
func fixModes(dir string) error {
	return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info.IsDir() {
			mode = 0o755
		}
		return os.Chmod(p, mode)
	})
}
