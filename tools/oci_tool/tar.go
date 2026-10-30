package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
)

// kindTar is a rule entry that is not a layer.Entry itself: every entry of
// a tarball (plain or gzipped), as rules_oci's oci_image takes any tar as a
// layer. Each keeps its own mode (setuid, setgid and sticky bits included,
// as rules_oci keeps a tar's and dpkg and apk install a package's), owner
// and link target; the layer still sets every time to the epoch and refuses
// what it refuses of any entry (a path declared twice, a whiteout name).
const kindTar layer.Kind = "tar"

// tarEntries is a tarball's entries as layer entries. Regular files are
// extracted under scratch, which the entries' sources point into; a hard
// link becomes a copy of the file it links to, which this layer has no other
// way to express. A device, FIFO or anything else a layer cannot hold is
// refused, naming it.
func tarEntries(source, scratch string) ([]layer.Entry, error) {
	f, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var stream io.Reader = reader
	if magic, err := reader.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		defer gz.Close()
		stream = gz
	}

	var entries []layer.Entry
	extracted := map[string]string{}
	dirs := map[string]bool{}
	tr := tar.NewReader(stream)
	for i := 0; ; i++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		name, err := tarPath(hdr.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		if name == "" {
			continue // the archive's root, ./
		}
		mode := hdr.Mode & 0o7777
		entry := layer.Entry{Path: "/" + name, UID: hdr.Uid, GID: hdr.Gid, Packaged: true}
		switch hdr.Typeflag {
		case tar.TypeDir:
			dirs[name] = true
			entry.Kind = layer.KindDir
			entry.Mode = &mode
		case tar.TypeSymlink:
			// An absolute target resolves against the image's own root, as
			// Debian's /etc/alternatives links do; a relative one must stay
			// inside it, resolved from the link's directory.
			if !strings.HasPrefix(hdr.Linkname, "/") {
				resolved := path.Join("/", path.Dir(name), hdr.Linkname)
				if climbs("/"+path.Dir(name), hdr.Linkname) {
					return nil, fmt.Errorf("%s: /%s links to %s, which climbs above the image root (to %s)", source, name, hdr.Linkname, resolved)
				}
			}
			entry.Kind = layer.KindSymlink
			entry.Target = hdr.Linkname
		case tar.TypeReg:
			out := filepath.Join(scratch, fmt.Sprintf("%d", i))
			if err := extract(tr, out); err != nil {
				return nil, fmt.Errorf("%s: /%s: %w", source, name, err)
			}
			extracted[name] = out
			entry.Kind = layer.KindFile
			entry.Source = out
			entry.Mode = &mode
		case tar.TypeLink:
			target, err := tarPath(hdr.Linkname)
			if err != nil {
				return nil, fmt.Errorf("%s: /%s: %w", source, name, err)
			}
			if dirs[target] {
				return nil, fmt.Errorf("%s: /%s is a hard link to the directory /%s, which no filesystem can hold", source, name, target)
			}
			copied, ok := extracted[target]
			if !ok {
				return nil, fmt.Errorf("%s: /%s is a hard link to /%s, which the archive has not yet given as a file", source, name, target)
			}
			entry.Kind = layer.KindFile
			entry.Source = copied
			entry.Mode = &mode
		default:
			return nil, fmt.Errorf("%s: /%s is tar type %q; a layer holds files, directories and links only", source, name, string(hdr.Typeflag))
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s holds no entries", source)
	}
	return entries, nil
}

// tarPath is a tar member's name as a path inside the image, without a
// leading ./ or /: "" for the root, and an error for a name that escapes it.
func tarPath(name string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimPrefix(name, "./"))
	if strings.Contains(name, "\x00") {
		return "", fmt.Errorf("%q contains a NUL", name)
	}
	for _, part := range strings.Split(strings.TrimPrefix(name, "./"), "/") {
		if part == ".." {
			return "", fmt.Errorf("%q leaves the image root", name)
		}
	}
	return strings.TrimPrefix(cleaned, "/"), nil
}

// climbs reports whether a relative link target, followed from dir, goes
// above the root at any step: path.Join would clamp it to / and hide that.
func climbs(dir, target string) bool {
	depth := 0
	for _, part := range strings.Split(strings.Trim(dir, "/"), "/") {
		if part != "" {
			depth++
		}
	}
	for _, part := range strings.Split(target, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return true
			}
		default:
			depth++
		}
	}
	return false
}

func extract(r io.Reader, out string) error {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// mergeDirs drops a directory entry that repeats an earlier one exactly
// (same mode and owner): several tarballs each holding /usr give the layer
// one /usr. Any other repeat is left for the layer to refuse as declared
// twice.
func mergeDirs(entries []layer.Entry) []layer.Entry {
	seen := map[string]layer.Entry{}
	out := entries[:0:0]
	for _, e := range entries {
		if e.Kind == layer.KindDir {
			if prior, ok := seen[e.Path]; ok && prior.Kind == layer.KindDir && sameDir(prior, e) {
				continue
			}
		}
		if _, ok := seen[e.Path]; !ok {
			seen[e.Path] = e
		}
		out = append(out, e)
	}
	return out
}

func sameDir(a, b layer.Entry) bool {
	modeA, modeB := int64(0o755), int64(0o755)
	if a.Mode != nil {
		modeA = *a.Mode
	}
	if b.Mode != nil {
		modeB = *b.Mode
	}
	return modeA == modeB && a.UID == b.UID && a.GID == b.GID
}
