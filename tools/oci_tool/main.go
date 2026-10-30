// oci-tool is the oci plugin's build tool. Each subcommand is one build
// action: it reads only the files it is given and writes only the outputs it
// is told to, with no network and no credentials.
//
//	oci-tool layer --spec ruleSpec.json --blob layer.tar.gz --meta layer.json
//	oci-tool image --base base.json --layer meta.json... --spec ruleSpec.json \
//	               --config c.json --manifest m.json --meta image.json --digest d \
//	               [--fqn f --retain r]
//	oci-tool archive --spec s.json --manifest m.json --config c.json --blob b... [--tag-file t...] --out image.tar
//	oci-tool layout  --spec s.json --manifest m.json --config c.json --blob b... --out dir
//	oci-tool check   --spec s.json --manifest m.json --config c.json --blob b...
//	oci-tool import  --layout dir --platform linux/amd64 --base base.json --blobs dir
//	oci-tool index --spec s.json --child meta.json... --index i.json --meta m.json --digest d [--fqn f --retain r]
//	oci-tool push  --manifest m.json --config c.json --blob b... --fqn f --retain r
//	oci-tool push-index --index i.json --child-manifest m --child-config c... --blob b... --fqn f --retain r
//	oci-tool sign  --ref f --layout dir --meta m.json --target t --cosign c --syft s [--key k] <source flags>
//	oci-tool verify --ref f --cosign c (--public-key k | --identity i --issuer s)
//	oci-tool pull  --image docker.io/library/postgres --digest sha256:<hex> --platform linux/amd64 --out dir
//
// push is the exception: it talks to a registry with the caller's
// credentials, and runs from a deploy, never as a build action. pull is the
// one build action with the network: it downloads a public image by digest,
// anonymously, and checks every byte against that digest.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/layer"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/spec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: oci-tool layer|import|image|index|archive|layout|check|pull|push|push-index|sign|verify [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "layer":
		err = runLayer(os.Args[2:])
	case "import":
		err = runImport(os.Args[2:])
	case "image":
		err = runImage(os.Args[2:])
	case "index":
		err = runIndex(os.Args[2:])
	case "push-index":
		err = runPushIndex(os.Args[2:])
	case "sign":
		err = runSign(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "archive":
		err = runArchive(os.Args[2:])
	case "layout":
		err = runLayout(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	case "pull":
		err = runPull(os.Args[2:])
	case "push":
		err = runPush(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q; want layer, import, image, index, archive, layout, check, pull, push, push-index, sign or verify", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "oci-tool %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

// ruleSpec is the spec as the build rule writes it.
type ruleSpec struct {
	Entries  []ruleEntry `json:"entries"`
	Platform string      `json:"platform,omitempty"`
}

// kindTree is a rule entry that is not a layer.Entry itself: every file of
// its source group, placed under its path at the file's path relative to
// Strip (the source rule's package).
const kindTree layer.Kind = "tree"

// ruleEntry is a layer.Entry as the build rule writes it. Source names one of
// the rule's source groups (f0, f1, t0, ...), which Please exposes as
// $SRCS_F0.
// Mode is an octal string ("0755"): Please's BUILD language reads 0o755 as
// the decimal 755, so a mode never crosses that boundary as a number.
type ruleEntry struct {
	Path   string     `json:"path"`
	Kind   layer.Kind `json:"kind"`
	Source string     `json:"source,omitempty"`
	Target string     `json:"target,omitempty"`
	Mode   string     `json:"mode,omitempty"`
	Strip  string     `json:"strip,omitempty"`
	UID    int        `json:"uid"`
	GID    int        `json:"gid"`
}

func runLayer(args []string) error {
	fs := flag.NewFlagSet("layer", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the layer spec, as JSON")
	blobPath := fs.String("blob", "", "where to write the compressed layer")
	metaPath := fs.String("meta", "", "where to write the layer's metadata, as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *blobPath == "" || *metaPath == "" {
		return fmt.Errorf("--spec, --blob and --meta are all required")
	}

	var ruleSpec ruleSpec
	if err := spec.Decode(*specPath, &ruleSpec); err != nil {
		return err
	}
	var entries []layer.Entry
	for _, e := range ruleSpec.Entries {
		if e.Kind == kindTar {
			source, err := resolveSource(e.Source)
			if err != nil {
				return fmt.Errorf("tar %s: %w", e.Source, err)
			}
			scratch, err := os.MkdirTemp(".", "tar-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(scratch)
			tarred, err := tarEntries(source, scratch)
			if err != nil {
				return err
			}
			entries = append(entries, tarred...)
			continue
		}
		if e.Kind == kindTree {
			tree, err := treeEntries(e)
			if err != nil {
				return fmt.Errorf("%s: %w", e.Path, err)
			}
			entries = append(entries, tree...)
			continue
		}
		entry := layer.Entry{Path: e.Path, Kind: e.Kind, Target: e.Target, UID: e.UID, GID: e.GID}
		if e.Mode != "" {
			mode, err := parseMode(e.Mode)
			if err != nil {
				return fmt.Errorf("%s: %w", e.Path, err)
			}
			entry.Mode = &mode
		}
		if e.Kind == layer.KindFile {
			resolved, err := resolveSource(e.Source)
			if err != nil {
				return fmt.Errorf("%s: %w", e.Path, err)
			}
			entry.Source = resolved
		}
		entries = append(entries, entry)
	}

	entries = mergeDirs(entries)
	blob, err := os.Create(*blobPath)
	if err != nil {
		return err
	}
	meta, err := layer.Write(layer.Spec{Entries: entries, Platform: ruleSpec.Platform}, blob)
	if closeErr := blob.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*metaPath, append(encoded, '\n'), 0o644)
}

// treeEntries is a tree's files: each of its source group's files, under its
// path at the file's path relative to its Strip, 0644 and owned as the tree
// is. A file outside Strip is refused rather than placed somewhere guessed.
func treeEntries(e ruleEntry) ([]layer.Entry, error) {
	if e.Strip == "" {
		return nil, fmt.Errorf("a tree names no package to place its files relative to")
	}
	group := "SRCS_" + strings.ToUpper(e.Source)
	value, ok := os.LookupEnv(group)
	if !ok {
		return nil, fmt.Errorf("source group %s is not set ($%s)", e.Source, group)
	}
	paths := strings.Fields(value)
	if len(paths) == 0 {
		return nil, fmt.Errorf("source group %s holds no files", e.Source)
	}
	mode := int64(0o644)
	prefix := e.Strip + "/"
	entries := make([]layer.Entry, 0, len(paths))
	for _, p := range paths {
		rel, ok := strings.CutPrefix(p, prefix)
		if !ok || rel == "" {
			return nil, fmt.Errorf("%s is not in %s, the package its place is relative to", p, e.Strip)
		}
		entries = append(entries, layer.Entry{Path: e.Path + "/" + rel, Kind: layer.KindFile, Source: p, Mode: &mode, UID: e.UID, GID: e.GID})
	}
	return entries, nil
}

// parseMode reads an octal permission string: "0755" or "755".
func parseMode(s string) (int64, error) {
	mode, err := strconv.ParseInt(s, 8, 64)
	if err != nil {
		return 0, fmt.Errorf("mode %q is not an octal number such as \"0755\"", s)
	}
	return mode, nil
}

// resolveSource turns a source group's name into the one file Please placed
// for it. A group holding several files is refused rather than guessed at.
func resolveSource(group string) (string, error) {
	if group == "" {
		return "", fmt.Errorf("no source group")
	}
	value, ok := os.LookupEnv("SRCS_" + strings.ToUpper(group))
	if !ok {
		return "", fmt.Errorf("source group %s is not set ($SRCS_%s)", group, strings.ToUpper(group))
	}
	paths := strings.Fields(value)
	if len(paths) != 1 {
		return "", fmt.Errorf("source group %s holds %d files (%s); a layer file maps to exactly one, so select it with |output", group, len(paths), value)
	}
	return paths[0], nil
}
