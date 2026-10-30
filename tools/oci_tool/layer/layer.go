// Package layer writes an OCI filesystem layer, deterministically.
//
// The same spec and the same input bytes always give the same layer, whatever
// the host: input modification times, owners, permissions, the umask and the
// order inputs arrive in never reach the output. Entries are sorted by path,
// every parent directory is written before its children, every timestamp is
// the Unix epoch, owners are numeric and default to root, and nothing reads
// the host's user database. A regular file is 0644 unless the spec says
// otherwise; an executable input must have its mode declared, because the
// executable bit on an input is not stable (remote executors mark build
// outputs 0555, a local build leaves them 0755).
//
// The layer is a tar compressed with Go's compress/gzip at level 6, so its
// bytes are fixed by the Go toolchain that builds this tool. Upgrading Go can
// change the compressed bytes, and therefore every digest; the uncompressed
// diff ID depends only on the spec and the inputs.
package layer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// MediaType is the OCI media type of every layer this package writes.
const MediaType = "application/vnd.oci.image.layer.v1.tar+gzip"

// GzipLevel is fixed: changing it changes every layer's digest.
const GzipLevel = 6

// Kind is what an entry places in the layer.
type Kind string

// The kinds of entry a layer holds: a regular file copied from a source, a
// directory, and a symbolic link.
const (
	KindFile    Kind = "file"
	KindDir     Kind = "dir"
	KindSymlink Kind = "symlink"
	// KindWhiteout deletes its path, and everything under it, from the
	// layers below: an empty `.wh.<name>` file beside it, as the OCI image
	// spec defines.
	KindWhiteout Kind = "whiteout"
	// KindOpaque hides everything the layers below put in its directory,
	// keeping only what this layer puts there: an empty `.wh..wh..opq` file
	// inside it.
	KindOpaque Kind = "opaque"
)

// The OCI image spec's whiteout prefix and opaque marker. A declared name may
// not start with the prefix: a reader would take it for a whiteout.
const (
	whiteoutPrefix = ".wh."
	opaqueMarker   = ".wh..wh..opq"
)

// Entry is one path in the layer. Path is absolute in the spec ("/app/bin")
// and stored relative to the image root ("app/bin").
type Entry struct {
	Path string `json:"path"`
	Kind Kind   `json:"kind"`
	// Source is the file a KindFile entry copies, already resolved to a path.
	Source string `json:"source,omitempty"`
	// Target is a KindSymlink entry's link target, written as given.
	Target string `json:"target,omitempty"`
	// Mode is the permission bits; nil means the default for the kind.
	Mode *int64 `json:"mode,omitempty"`
	// Packaged marks an entry taken from a tarball (a package's data), whose
	// mode is kept whole, setuid, setgid and sticky bits included, as
	// rules_oci keeps a tar's and dpkg and apk install one. An entry a rule
	// declares itself may set permission bits only.
	Packaged bool `json:"packaged,omitempty"`
	UID      int  `json:"uid"`
	GID      int  `json:"gid"`
}

// Spec is everything a layer is made from.
type Spec struct {
	Entries []Entry `json:"entries"`
	// Platform, when set, is the platform every executable in the layer must
	// be built for ("linux/amd64"). A layer holding one built for another is
	// refused, naming it.
	Platform string `json:"platform,omitempty"`
}

// Meta describes a written layer: what an image's config and manifest need
// without reading the blob.
type Meta struct {
	SchemaVersion int    `json:"schema_version"`
	MediaType     string `json:"media_type"`
	// Digest identifies the compressed bytes: the blob a registry stores.
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	// DiffID identifies the uncompressed tar: what an image config lists.
	DiffID string `json:"diff_id"`
	// Binaries maps each executable or shared object in the layer, by its
	// absolute image path, to the platform its header names, so that an
	// image can refuse one built for another platform.
	Binaries map[string]string `json:"binaries,omitempty"`
}

const (
	defaultFileMode int64 = 0o644
	defaultDirMode  int64 = 0o755
	symlinkMode     int64 = 0o777
)

// Write validates the spec, then writes the layer's compressed bytes to w and
// returns its metadata. Nothing is written if the spec is invalid.
func Write(spec Spec, w io.Writer) (Meta, error) {
	entries, err := normalise(spec)
	if err != nil {
		return Meta{}, err
	}

	var tarBytes bytes.Buffer
	binaries, err := writeTar(entries, &tarBytes)
	if err != nil {
		return Meta{}, err
	}
	if spec.Platform != "" {
		for _, path := range sortedKeys(binaries) {
			if binaries[path] != spec.Platform {
				return Meta{}, fmt.Errorf("%s is built for %s, but the layer is for %s; build it for %s", path, binaries[path], spec.Platform, spec.Platform)
			}
		}
	}
	diffID := sha256.Sum256(tarBytes.Bytes())

	var gz bytes.Buffer
	zw, err := gzip.NewWriterLevel(&gz, GzipLevel)
	if err != nil {
		return Meta{}, err
	}
	if _, err := zw.Write(tarBytes.Bytes()); err != nil {
		return Meta{}, err
	}
	if err := zw.Close(); err != nil {
		return Meta{}, err
	}
	digest := sha256.Sum256(gz.Bytes())
	if _, err := w.Write(gz.Bytes()); err != nil {
		return Meta{}, err
	}
	return Meta{
		SchemaVersion: 1,
		MediaType:     MediaType,
		Digest:        "sha256:" + hex.EncodeToString(digest[:]),
		Size:          int64(gz.Len()),
		DiffID:        "sha256:" + hex.EncodeToString(diffID[:]),
		Binaries:      binaries,
	}, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// normalise checks every entry, adds the parent directories nothing declared,
// and sorts the result so that each parent precedes its children.
func normalise(spec Spec) ([]Entry, error) {
	if len(spec.Entries) == 0 {
		return nil, fmt.Errorf("a layer needs at least one entry")
	}
	byPath := map[string]Entry{}
	for _, e := range spec.Entries {
		p, err := cleanPath(e.Path)
		if err != nil {
			return nil, err
		}
		if _, dup := byPath[p]; dup {
			return nil, fmt.Errorf("/%s is declared twice", p)
		}
		if e.UID < 0 || e.GID < 0 {
			return nil, fmt.Errorf("/%s has a negative owner %d:%d", p, e.UID, e.GID)
		}
		for _, part := range strings.Split(p, "/") {
			if strings.HasPrefix(part, whiteoutPrefix) {
				return nil, fmt.Errorf("/%s: a name starting with %q is a whiteout to any reader; declare a whiteout or opaque directory instead", p, whiteoutPrefix)
			}
		}
		if e.Mode != nil && e.Packaged && (*e.Mode < 0 || *e.Mode > 0o7777) {
			return nil, fmt.Errorf("/%s has mode %#o, which is more than a file mode's twelve bits", p, *e.Mode)
		}
		if e.Mode != nil && !e.Packaged && (*e.Mode < 0 || *e.Mode > 0o777) {
			return nil, fmt.Errorf("/%s has mode %#o; only permission bits (0 to 0777) may be set, no setuid, setgid or sticky bit", p, *e.Mode)
		}
		switch e.Kind {
		case KindFile:
			if e.Source == "" {
				return nil, fmt.Errorf("/%s is a file with no source", p)
			}
			if e.Target != "" {
				return nil, fmt.Errorf("/%s is a file and cannot have a link target", p)
			}
		case KindDir:
			if e.Source != "" || e.Target != "" {
				return nil, fmt.Errorf("/%s is a directory and cannot have a source or link target", p)
			}
		case KindSymlink:
			if e.Target == "" {
				return nil, fmt.Errorf("/%s is a symlink with no target", p)
			}
			if e.Source != "" || e.Mode != nil {
				return nil, fmt.Errorf("/%s is a symlink and cannot have a source or mode", p)
			}
		case KindWhiteout, KindOpaque:
			if e.Source != "" || e.Target != "" || e.Mode != nil || e.UID != 0 || e.GID != 0 {
				return nil, fmt.Errorf("/%s is a %s and has no source, target, mode or owner", p, e.Kind)
			}
		default:
			return nil, fmt.Errorf("/%s has kind %q; want %q, %q, %q, %q or %q", p, e.Kind, KindFile, KindDir, KindSymlink, KindWhiteout, KindOpaque)
		}
		e.Path = p
		byPath[p] = e
	}

	// A whiteout deletes from the layers below, so the same layer may not
	// also add the path or anything under it: which would win is not a
	// question a reader should have to answer.
	for p, e := range byPath {
		if e.Kind != KindWhiteout {
			continue
		}
		for other := range byPath {
			if other == p {
				continue
			}
			if strings.HasPrefix(other, p+"/") {
				return nil, fmt.Errorf("/%s is whited out, but this layer adds /%s inside it", p, other)
			}
		}
	}
	withMarkers, err := markers(byPath)
	if err != nil {
		return nil, err
	}
	return parentsAndSort(withMarkers)
}

// markers replaces each whiteout and opaque entry with the empty file that
// expresses it in a tar. An opaque directory keeps its directory entry.
func markers(byPath map[string]Entry) (map[string]Entry, error) {
	out := map[string]Entry{}
	for p, e := range byPath {
		switch e.Kind {
		case KindWhiteout:
			marker := path.Join(path.Dir(p), whiteoutPrefix+path.Base(p))
			out[marker] = Entry{Path: marker, Kind: KindWhiteout}
		case KindOpaque:
			marker := path.Join(p, opaqueMarker)
			out[marker] = Entry{Path: marker, Kind: KindOpaque}
		default:
			out[p] = e
		}
	}
	for p, e := range byPath {
		if e.Kind == KindOpaque {
			if declared, ok := out[p]; ok && declared.Kind != KindDir {
				return nil, fmt.Errorf("/%s is opaque, so it is a directory, but this layer declares it a %s", p, declared.Kind)
			}
		}
	}
	return out, nil
}

// parentsAndSort adds every parent directory nothing declared and sorts the
// entries so that each parent precedes its children.
func parentsAndSort(byPath map[string]Entry) ([]Entry, error) {
	// Every parent of every entry must be a directory: declared as one, or
	// added here with the default directory mode and root ownership.
	for p := range byPath {
		for parent := path.Dir(p); parent != "."; parent = path.Dir(parent) {
			existing, ok := byPath[parent]
			if !ok {
				byPath[parent] = Entry{Path: parent, Kind: KindDir}
				continue
			}
			if existing.Kind != KindDir {
				return nil, fmt.Errorf("/%s is a %s but /%s is inside it", parent, existing.Kind, p)
			}
		}
	}

	entries := make([]Entry, 0, len(byPath))
	for _, e := range byPath {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// cleanPath turns an absolute image path into its tar name, refusing anything
// that does not name exactly one place inside the image.
func cleanPath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%q contains a NUL", p)
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%q must be an absolute image path, starting with /", p)
	}
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		return "", fmt.Errorf("%q is the image root, which a layer cannot replace", p)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%q must not contain an empty, . or .. component", p)
		}
	}
	return rel, nil
}

// epoch is every entry's modification time.
var epoch = time.Unix(0, 0).UTC()

func writeTar(entries []Entry, w io.Writer) (map[string]string, error) {
	var binaries map[string]string
	tw := tar.NewWriter(w)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.Path,
			Uid:     e.UID,
			Gid:     e.GID,
			ModTime: epoch,
		}
		var data []byte
		switch e.Kind {
		case KindDir:
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
			hdr.Mode = modeOr(e.Mode, defaultDirMode)
		case KindSymlink:
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = e.Target
			hdr.Mode = symlinkMode
		case KindWhiteout, KindOpaque:
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = defaultFileMode
		case KindFile:
			info, err := os.Stat(e.Source)
			if err != nil {
				return nil, fmt.Errorf("/%s: %w", e.Path, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("/%s: source %s is not a regular file", e.Path, e.Source)
			}
			if e.Mode == nil && info.Mode().Perm()&0o111 != 0 {
				return nil, fmt.Errorf("/%s: source %s is executable; declare its mode, because an input's executable bit differs between local and remote builds", e.Path, e.Source)
			}
			data, err = os.ReadFile(e.Source)
			if err != nil {
				return nil, fmt.Errorf("/%s: %w", e.Path, err)
			}
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = modeOr(e.Mode, defaultFileMode)
			hdr.Size = int64(len(data))
			if platform, ok := platformOf(data); ok {
				if binaries == nil {
					binaries = map[string]string{}
				}
				binaries["/"+e.Path] = platform
			}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("/%s: %w", e.Path, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("/%s: %w", e.Path, err)
		}
	}
	return binaries, tw.Close()
}

func modeOr(mode *int64, fallback int64) int64 {
	if mode != nil {
		return *mode
	}
	return fallback
}
