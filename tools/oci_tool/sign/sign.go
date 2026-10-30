// Package sign signs a published image's digest with cosign and attaches its
// SBOM and SLSA provenance as attestations. It runs after the image is
// published and never changes the image: a signature and an attestation are
// manifests of their own, pointing at the image's digest.
package sign

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
)

// The predicate types attached to each image, as cosign names them.
const (
	PredicateSBOM       = "spdxjson"
	PredicateProvenance = "slsaprovenance1"
)

// Source is where a build came from and what ran it, for its provenance.
// Every field is required: provenance that leaves one out says less than it
// seems to, and is refused rather than written.
type Source struct {
	// BuildType names the build: what turns these inputs into an image.
	BuildType string
	// SourceURI and Commit are the source the image was built from.
	SourceURI string
	Commit    string
	// BuilderID names what ran the build, and InvocationID this run of it.
	BuilderID    string
	InvocationID string
}

func (s Source) check() error {
	var missing []string
	for name, value := range map[string]string{
		"build type":    s.BuildType,
		"source URI":    s.SourceURI,
		"commit":        s.Commit,
		"builder ID":    s.BuilderID,
		"invocation ID": s.InvocationID,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("provenance needs a %s", strings.Join(missing, ", a "))
	}
	return nil
}

// provenance is a SLSA v1 provenance predicate, the fields this writes.
type provenance struct {
	BuildDefinition struct {
		BuildType            string               `json:"buildType"`
		ExternalParameters   map[string]string    `json:"externalParameters"`
		ResolvedDependencies []resourceDescriptor `json:"resolvedDependencies"`
	} `json:"buildDefinition"`
	RunDetails struct {
		Builder struct {
			ID string `json:"id"`
		} `json:"builder"`
		Metadata struct {
			InvocationID string `json:"invocationId"`
		} `json:"metadata"`
	} `json:"runDetails"`
}

type resourceDescriptor struct {
	URI    string            `json:"uri,omitempty"`
	Name   string            `json:"name,omitempty"`
	Digest map[string]string `json:"digest"`
}

// Provenance is the SLSA v1 provenance predicate for the image target built
// from src: the commit it was built at, and as resolved dependencies its
// config and every layer, base layers included, by digest.
func Provenance(target string, meta image.Meta, src Source) ([]byte, error) {
	if err := src.check(); err != nil {
		return nil, err
	}
	if target == "" {
		return nil, fmt.Errorf("provenance needs the target that built the image")
	}
	var p provenance
	p.BuildDefinition.BuildType = src.BuildType
	p.BuildDefinition.ExternalParameters = map[string]string{"target": target}
	p.BuildDefinition.ResolvedDependencies = append(p.BuildDefinition.ResolvedDependencies,
		resourceDescriptor{URI: src.SourceURI, Digest: map[string]string{"gitCommit": src.Commit}},
		descriptorOf("config", meta.Config),
	)
	for i, layer := range meta.Layers {
		p.BuildDefinition.ResolvedDependencies = append(p.BuildDefinition.ResolvedDependencies, descriptorOf(fmt.Sprintf("layer %d", i), layer))
	}
	p.RunDetails.Builder.ID = src.BuilderID
	p.RunDetails.Metadata.InvocationID = src.InvocationID
	return json.Marshal(p)
}

func descriptorOf(name string, d v1.Descriptor) resourceDescriptor {
	return resourceDescriptor{Name: name, Digest: map[string]string{d.Digest.Algorithm: d.Digest.Hex}}
}

// Verifier is who a signature must be from: a public key, or for keyless
// signing a certificate identity and its OIDC issuer. Exactly one.
type Verifier struct {
	PublicKey string
	Identity  string
	Issuer    string
}

func (v Verifier) args() ([]string, error) {
	keyless := v.Identity != "" || v.Issuer != ""
	switch {
	case v.PublicKey != "" && keyless:
		return nil, fmt.Errorf("verify by a public key or by a keyless identity, not both")
	case v.PublicKey != "":
		return []string{"--key", v.PublicKey}, nil
	case v.Identity != "" && v.Issuer != "":
		return []string{"--certificate-identity-regexp", v.Identity, "--certificate-oidc-issuer", v.Issuer}, nil
	case keyless:
		return nil, fmt.Errorf("keyless verification needs both a certificate identity and its OIDC issuer")
	}
	return nil, fmt.Errorf("verification needs a public key, or a keyless certificate identity and issuer")
}

// Cosign runs cosign. Key, when set, signs with that key (a file, or a KMS
// URI such as gcpkms://...); unset, signing is keyless, with the OIDC token
// the environment provides. Flags go to every invocation, after the
// command's own.
type Cosign struct {
	Path  string
	Key   string
	Flags []string
}

func (c Cosign) run(args ...string) error {
	command := exec.Command(c.Path, append(args, c.Flags...)...)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("cosign %s: %w", args[0], err)
	}
	return nil
}

func (c Cosign) keyArgs() []string {
	if c.Key == "" {
		return nil
	}
	return []string{"--key", c.Key}
}

// Sign signs the image at ref, a reference by digest.
func (c Cosign) Sign(ref string) error {
	return c.run(append(append([]string{"sign", "--yes"}, c.keyArgs()...), ref)...)
}

// Attest attaches predicate, of predicateType, to the image at ref.
func (c Cosign) Attest(ref, predicateType, predicate string) error {
	return c.run(append(append([]string{"attest", "--yes", "--type", predicateType, "--predicate", predicate}, c.keyArgs()...), ref)...)
}

// Verify checks the image at ref is signed by v.
func (c Cosign) Verify(ref string, v Verifier) error {
	args, err := v.args()
	if err != nil {
		return err
	}
	return c.run(append(append([]string{"verify"}, args...), ref)...)
}

// VerifyAttestation checks the image at ref has an attestation of
// predicateType signed by v.
func (c Cosign) VerifyAttestation(ref, predicateType string, v Verifier) error {
	args, err := v.args()
	if err != nil {
		return err
	}
	return c.run(append(append([]string{"verify-attestation", "--type", predicateType}, args...), ref)...)
}

// SBOM writes the SPDX SBOM of the image in the OCI layout at layout to out,
// with syft. syft's check for a newer release of itself is turned off: a
// deploy has no reason to call out for it, and where the network drops the
// connection rather than refusing it, each check waits half a minute.
func SBOM(syft, layout, out string) error {
	command := exec.Command(syft, "scan", "oci-dir:"+layout, "-o", "spdx-json="+out, "-q")
	command.Env = append(os.Environ(), "SYFT_CHECK_FOR_APP_UPDATE=false")
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("syft: %w", err)
	}
	return nil
}

// ChildRefs is the reference by digest of each image an index names, in the
// index's repository, taken from the published index document itself. raw
// must be the document ref names, and children the metadata of exactly the
// images it lists, each matching its entry's digest, size and media type:
// otherwise the references would be of something other than what was
// published, and nothing is returned.
func ChildRefs(raw []byte, ref name.Digest, children []image.Meta) ([]name.Digest, error) {
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != ref.DigestStr() {
		return nil, fmt.Errorf("the index document is %s, but the index to sign is %s", got, ref.DigestStr())
	}
	index, err := v1.ParseIndexManifest(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing the index: %w", err)
	}
	byDigest := map[v1.Hash]v1.Descriptor{}
	for _, descriptor := range index.Manifests {
		if _, dup := byDigest[descriptor.Digest]; dup {
			return nil, fmt.Errorf("the index names %s twice", descriptor.Digest)
		}
		byDigest[descriptor.Digest] = descriptor
	}
	given := map[v1.Hash]bool{}
	var refs []name.Digest
	for i, child := range children {
		descriptor, ok := byDigest[child.Manifest.Digest]
		if !ok {
			return nil, fmt.Errorf("child %d is %s, which the index does not name", i, child.Manifest.Digest)
		}
		if given[child.Manifest.Digest] {
			return nil, fmt.Errorf("child %s is given twice", child.Manifest.Digest)
		}
		given[child.Manifest.Digest] = true
		if descriptor.Size != child.Manifest.Size || descriptor.MediaType != child.Manifest.MediaType {
			return nil, fmt.Errorf("the index names %s as %d bytes of %s, and the child given is %d bytes of %s",
				descriptor.Digest, descriptor.Size, descriptor.MediaType, child.Manifest.Size, child.Manifest.MediaType)
		}
		childRef, err := name.NewDigest(ref.Context().Name()+"@"+descriptor.Digest.String(), name.StrictValidation)
		if err != nil {
			return nil, err
		}
		refs = append(refs, childRef)
	}
	for _, descriptor := range index.Manifests {
		if !given[descriptor.Digest] {
			return nil, fmt.Errorf("the index names %s, and no child given is it", descriptor.Digest)
		}
	}
	return refs, nil
}
