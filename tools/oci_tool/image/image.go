// Package image assembles OCI image configs and manifests from a base and
// deterministic layers, without a daemon and without the network.
//
// A base is an image's metadata: an imported layout's (a pulled image, or
// apko's), or another image built in the repository.
//
//   - Build appends layers to a base and writes the config and manifest.
//     Every time is the Unix epoch and every JSON document has one encoding,
//     so the same inputs give the same manifest digest. It also returns the
//     image as a base, so an image built in the repository can be another's.
package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// LayerMeta is an oci_layer's |meta output.
type LayerMeta struct {
	SchemaVersion int    `json:"schema_version"`
	MediaType     string `json:"media_type"`
	Digest        string `json:"digest"`
	Size          int64  `json:"size"`
	DiffID        string `json:"diff_id"`
	// Binaries maps each executable in the layer to the platform its header
	// names. Every one must be the image's platform.
	Binaries map[string]string `json:"binaries,omitempty"`
}

// BaseMeta is a base, ready to build on: its config with the lock's layers
// only, and those layers' descriptors.
type BaseMeta struct {
	SchemaVersion int             `json:"schema_version"`
	Platform      string          `json:"platform"`
	Config        v1.ConfigFile   `json:"config"`
	Layers        []v1.Descriptor `json:"layers"`
}

// Spec is what an oci_image declares on top of its base.
type Spec struct {
	Platform string `json:"platform"`
	// Entrypoint and Cmd: nil inherits the base's; an empty list clears it.
	// Setting Entrypoint without Cmd clears an inherited Cmd, as a
	// Dockerfile's ENTRYPOINT does.
	Entrypoint *[]string `json:"entrypoint,omitempty"`
	Cmd        *[]string `json:"cmd,omitempty"`
	// Env values are set over the base's, key by key.
	Env        map[string]string `json:"env,omitempty"`
	User       *string           `json:"user,omitempty"`
	WorkingDir *string           `json:"workdir,omitempty"`
	// StopSignal, when set, is the signal a runtime stops the container
	// with ("SIGQUIT"); nil inherits the base's.
	StopSignal *string `json:"stop_signal,omitempty"`
	// Repository is where the image is published; it names the image's
	// reference by digest and never enters the image itself.
	Repository string `json:"repository,omitempty"`
}

// Meta describes a built image: what a reference, a push and a layout need.
type Meta struct {
	SchemaVersion int             `json:"schema_version"`
	Platform      string          `json:"platform"`
	Manifest      v1.Descriptor   `json:"manifest"`
	Config        v1.Descriptor   `json:"config"`
	Layers        []v1.Descriptor `json:"layers"`
}

// Built is an image's three documents, and the image as a base for another.
type Built struct {
	Config   []byte
	Manifest []byte
	Meta     Meta
	Base     BaseMeta
}

var epoch = v1.Time{Time: time.Unix(0, 0).UTC()}

// Build appends layers to a base (or to nothing, for an image from scratch)
// and writes the image's config and manifest.
func Build(base *BaseMeta, layers []LayerMeta, spec Spec) (Built, error) {
	if err := checkPlatform(spec.Platform); err != nil {
		return Built{}, err
	}
	if len(layers) == 0 && base == nil {
		return Built{}, fmt.Errorf("an image needs a base or at least one layer")
	}

	var cfg v1.ConfigFile
	var descriptors []v1.Descriptor
	if base != nil {
		if base.Platform != spec.Platform {
			return Built{}, fmt.Errorf("the image is %s but its base is %s; a platform is checked, never converted", spec.Platform, base.Platform)
		}
		cfg = *base.Config.DeepCopy()
		descriptors = append(descriptors, base.Layers...)
	} else {
		cfg.RootFS.Type = "layers"
	}
	os, arch, _ := strings.Cut(spec.Platform, "/")
	cfg.OS, cfg.Architecture = os, arch
	cfg.Created = epoch

	for i, layer := range layers {
		if layer.SchemaVersion != 1 || layer.MediaType != string(types.OCILayer) {
			return Built{}, fmt.Errorf("layer %d is not an oci_layer's metadata (schema %d, media type %q)", i, layer.SchemaVersion, layer.MediaType)
		}
		paths := make([]string, 0, len(layer.Binaries))
		for path := range layer.Binaries {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			if layer.Binaries[path] != spec.Platform {
				return Built{}, fmt.Errorf("layer %d: %s is built for %s, but the image is %s; build it for %s (oci_layer's platform)", i, path, layer.Binaries[path], spec.Platform, spec.Platform)
			}
		}
		digest, err := v1.NewHash(layer.Digest)
		if err != nil {
			return Built{}, fmt.Errorf("layer %d: %w", i, err)
		}
		diffID, err := v1.NewHash(layer.DiffID)
		if err != nil {
			return Built{}, fmt.Errorf("layer %d: %w", i, err)
		}
		descriptors = append(descriptors, v1.Descriptor{MediaType: types.OCILayer, Digest: digest, Size: layer.Size})
		cfg.RootFS.DiffIDs = append(cfg.RootFS.DiffIDs, diffID)
		cfg.History = append(cfg.History, v1.History{Created: epoch, CreatedBy: "oci_layer"})
	}

	if spec.Entrypoint != nil {
		cfg.Config.Entrypoint = append([]string{}, (*spec.Entrypoint)...)
		if spec.Cmd == nil {
			cfg.Config.Cmd = nil
		}
	}
	if spec.Cmd != nil {
		cfg.Config.Cmd = append([]string{}, (*spec.Cmd)...)
	}
	if spec.User != nil {
		cfg.Config.User = *spec.User
	}
	if spec.WorkingDir != nil {
		if !strings.HasPrefix(*spec.WorkingDir, "/") {
			return Built{}, fmt.Errorf("workdir %q must be absolute", *spec.WorkingDir)
		}
		cfg.Config.WorkingDir = *spec.WorkingDir
	}
	if spec.StopSignal != nil {
		if !strings.HasPrefix(*spec.StopSignal, "SIG") {
			return Built{}, fmt.Errorf("stop signal %q is not a signal name such as SIGQUIT", *spec.StopSignal)
		}
		cfg.Config.StopSignal = *spec.StopSignal
	}
	env, err := mergeEnv(cfg.Config.Env, spec.Env)
	if err != nil {
		return Built{}, err
	}
	cfg.Config.Env = env

	config, err := json.Marshal(cfg)
	if err != nil {
		return Built{}, err
	}
	configDesc := v1.Descriptor{MediaType: types.OCIConfigJSON, Digest: mustHash(digestOf(config)), Size: int64(len(config))}
	manifest, err := json.Marshal(v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config:        configDesc,
		Layers:        descriptors,
	})
	if err != nil {
		return Built{}, err
	}
	return Built{
		Config:   config,
		Manifest: manifest,
		Meta: Meta{
			SchemaVersion: 1,
			Platform:      spec.Platform,
			Manifest:      v1.Descriptor{MediaType: types.OCIManifestSchema1, Digest: mustHash(digestOf(manifest)), Size: int64(len(manifest))},
			Config:        configDesc,
			Layers:        descriptors,
		},
		Base: BaseMeta{SchemaVersion: 1, Platform: spec.Platform, Config: cfg, Layers: descriptors},
	}, nil
}

// mergeEnv sets each value over the base's: an existing key keeps its place,
// a new key is appended, new keys in sorted order.
func mergeEnv(base []string, set map[string]string) ([]string, error) {
	out := append([]string{}, base...)
	keys := make([]string, 0, len(set))
	for k := range set {
		if k == "" || strings.ContainsAny(k, "=\x00") {
			return nil, fmt.Errorf("env key %q is not a valid name", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := k + "=" + set[k]
		replaced := false
		for i, existing := range out {
			if name, _, _ := strings.Cut(existing, "="); name == k {
				out[i] = entry
				replaced = true
			}
		}
		if !replaced {
			out = append(out, entry)
		}
	}
	return out, nil
}

// checkPlatform accepts the platforms this plugin builds: linux on amd64 or
// arm64. A platform is a check on the inputs, never a conversion of them.
func checkPlatform(p string) error {
	switch p {
	case "linux/amd64", "linux/arm64":
		return nil
	}
	return fmt.Errorf("platform %q is not linux/amd64 or linux/arm64", p)
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustHash(s string) v1.Hash {
	h, err := v1.NewHash(s)
	if err != nil {
		panic(err)
	}
	return h
}
