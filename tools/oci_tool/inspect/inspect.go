// Package inspect reads a built image's filesystem as a container would see
// it: every layer applied in order, whiteouts and opaque directories
// honoured. It answers what container-structure-test's tar driver cannot,
// because that driver extracts files as whoever runs it: who owns a path.
package inspect

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/mutate"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
)

// Node is one path in the flattened filesystem, as its tar header says.
type Node struct {
	Typeflag byte
	Mode     int64
	UID      int
	GID      int
	Size     int64
	Linkname string
}

// Owner is a numeric owner, as an image's tar headers hold it.
type Owner struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

// Flatten is img's filesystem by absolute path. go-containerregistry applies
// the layers, so whiteouts and opaque directories follow the OCI image spec
// as one widely used implementation reads it.
func Flatten(img publish.Image) (map[string]Node, error) {
	assembled, err := img.Assemble()
	if err != nil {
		return nil, err
	}
	rc := mutate.Extract(assembled)
	defer rc.Close()
	fs := map[string]Node{}
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading the image's filesystem: %w", err)
		}
		p := "/" + strings.TrimSuffix(strings.TrimPrefix(path.Clean("/"+hdr.Name), "/"), "/")
		fs[p] = Node{Typeflag: hdr.Typeflag, Mode: hdr.Mode & 0o7777, UID: hdr.Uid, GID: hdr.Gid, Size: hdr.Size, Linkname: hdr.Linkname}
	}
}

// CheckOwners returns one sentence per path in want that is missing from fs
// or owned by someone else, in path order; none when every one matches.
func CheckOwners(fs map[string]Node, want map[string]Owner) []string {
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var problems []string
	for _, p := range paths {
		node, ok := fs[p]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s is not in the image", p))
			continue
		}
		if node.UID != want[p].UID || node.GID != want[p].GID {
			problems = append(problems, fmt.Sprintf("%s is owned %d:%d, want %d:%d", p, node.UID, node.GID, want[p].UID, want[p].GID))
		}
	}
	return problems
}
