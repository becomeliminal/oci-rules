package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/archive"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/spec"
)

// Format is the kind of archive `oci-tool archive` writes.
type Format string

const (
	// FormatDocker is a Docker archive: what `docker load` reads.
	FormatDocker Format = "docker"
	// FormatOCI is an OCI image layout as a tar.
	FormatOCI Format = "oci"
)

// archiveSpec is what the oci_archive and oci_load rules write.
type archiveSpec struct {
	Format Format `json:"format"`
	// Tags are the references a Docker archive loads as.
	Tags []string `json:"tags,omitempty"`
	// RefName is an OCI archive's ref.name annotation.
	RefName string `json:"ref_name,omitempty"`
}

// runArchive writes an image the build assembled as an archive. It is a
// build action: it reads only its inputs and touches no registry.
func runArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the archive spec, as JSON")
	manifestPath := fs.String("manifest", "", "the image's manifest")
	configPath := fs.String("config", "", "the image's config")
	outPath := fs.String("out", "", "where to write the archive")
	var blobs, tagFiles stringList
	fs.Var(&blobs, "blob", "a layer blob, once per blob")
	fs.Var(&tagFiles, "tag-file", "a file holding one more tag a Docker archive loads as, once per file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *manifestPath == "" || *configPath == "" || *outPath == "" {
		return fmt.Errorf("--spec, --manifest, --config and --out are all required")
	}
	var archiveSpec archiveSpec
	if err := spec.Decode(*specPath, &archiveSpec); err != nil {
		return err
	}
	img, err := publish.Load(*manifestPath, *configPath, blobs)
	if err != nil {
		return err
	}

	out, err := os.Create(*outPath)
	if err != nil {
		return err
	}
	switch archiveSpec.Format {
	case FormatDocker:
		if archiveSpec.RefName != "" {
			err = fmt.Errorf("a Docker archive names its image by its tags, not a ref_name")
			break
		}
		var tags []name.Tag
		tags, err = archiveTags(archiveSpec.Tags, tagFiles)
		if err == nil {
			err = archive.WriteDocker(img, tags, out)
		}
	case FormatOCI:
		if len(archiveSpec.Tags) > 0 || len(tagFiles) > 0 {
			err = fmt.Errorf("an OCI archive names its image by ref_name, not tags")
			break
		}
		err = archive.WriteOCI(img, archiveSpec.RefName, out)
	default:
		err = fmt.Errorf("format %q is not %q or %q", archiveSpec.Format, FormatDocker, FormatOCI)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// runLayout writes an image the build assembled as an OCI image layout
// directory. A build action, like archive.
func runLayout(args []string) error {
	fs := flag.NewFlagSet("layout", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the layout spec, as JSON: {\"ref_name\": ...}")
	manifestPath := fs.String("manifest", "", "the image's manifest")
	configPath := fs.String("config", "", "the image's config")
	outPath := fs.String("out", "", "the directory to write the layout into; it must not exist")
	var blobs stringList
	fs.Var(&blobs, "blob", "a layer blob, once per blob")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *manifestPath == "" || *configPath == "" || *outPath == "" {
		return fmt.Errorf("--spec, --manifest, --config and --out are all required")
	}
	var archiveSpec archiveSpec
	if err := spec.Decode(*specPath, &archiveSpec); err != nil {
		return err
	}
	if archiveSpec.Format != FormatOCI || len(archiveSpec.Tags) > 0 {
		return fmt.Errorf("a layout spec is format %q with no tags", FormatOCI)
	}
	img, err := publish.Load(*manifestPath, *configPath, blobs)
	if err != nil {
		return err
	}
	return archive.WriteLayout(img, archiveSpec.RefName, *outPath)
}

// runImport reads an OCI image layout another rule built as a base: the
// base's metadata, and its layers' compressed bytes into a directory, each
// named by its digest. A build action: it reads only the layout.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	layoutPath := fs.String("layout", "", "the OCI image layout directory")
	platform := fs.String("platform", "", "the platform of the image to import (linux/amd64)")
	baseOut := fs.String("base", "", "where to write the base's metadata")
	blobsOut := fs.String("blobs", "", "the directory to write the layers' blobs into; it must not exist")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *layoutPath == "" || *platform == "" || *baseOut == "" || *blobsOut == "" {
		return fmt.Errorf("--layout, --platform, --base and --blobs are all required")
	}
	base, layers, err := archive.ReadLayout(*layoutPath, *platform)
	if err != nil {
		return err
	}
	if err := os.Mkdir(*blobsOut, 0o755); err != nil {
		return err
	}
	for i, layer := range layers {
		digest, err := layer.Digest()
		if err != nil {
			return err
		}
		blob, err := layer.Compressed()
		if err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
		out, err := os.Create(filepath.Join(*blobsOut, digest.Hex))
		if err != nil {
			blob.Close()
			return err
		}
		_, err = io.Copy(out, blob)
		blob.Close()
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
	}
	return writeJSON(*baseOut, base)
}

// archiveTags is every tag a Docker archive loads as: those its spec names,
// then those in the given files, each a complete reference.
func archiveTags(named []string, files []string) ([]name.Tag, error) {
	var tags []name.Tag
	for _, t := range named {
		tag, err := name.NewTag(t, name.StrictValidation)
		if err != nil {
			return nil, fmt.Errorf("tag %q: %w", t, err)
		}
		tags = append(tags, tag)
	}
	for _, f := range files {
		tag, err := readTag(f)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}
