package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/image"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/sign"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/spec"
)

// signSpec is what the plugin's settings say about signing, as the rules
// write it: never shell text.
type signSpec struct {
	// Key signs (a file or a KMS URI); none signs keyless.
	Key string `json:"key,omitempty"`
	// BuildType is the provenance's build type.
	BuildType string `json:"build_type,omitempty"`
	// PublicKey, or Identity and Issuer, is who a signature must be from.
	PublicKey string `json:"public_key,omitempty"`
	Identity  string `json:"identity,omitempty"`
	Issuer    string `json:"issuer,omitempty"`
}

// readSignSpec reads path, when given, into a signSpec. A flag given on the
// command line overrides its field.
func readSignSpec(path string) (signSpec, error) {
	var signSpec signSpec
	if path == "" {
		return signSpec, nil
	}
	if err := spec.Decode(path, &signSpec); err != nil {
		return signSpec, err
	}
	return signSpec, nil
}

func override(flag *string, value string) {
	if *flag == "" {
		*flag = value
	}
}

// subject is one image to sign or verify: its reference by digest, and for
// signing, its layout (for the SBOM) and metadata (for the provenance).
type subject struct {
	ref    string
	layout string
	meta   image.Meta
}

// subjectFlags are the flags naming what is signed: an image, or an index
// and its children.
type subjectFlags struct {
	ref, indexRef          *string
	index                  *string
	layout, meta           *string
	childLayouts, children stringList
}

func addSubjectFlags(fs *flag.FlagSet) *subjectFlags {
	f := &subjectFlags{
		ref:      fs.String("ref", "", "the file holding an image's reference by digest"),
		indexRef: fs.String("index-ref", "", "the file holding an index's reference by digest"),
		index:    fs.String("index", "", "the index document itself, which names its children"),
		layout:   fs.String("layout", "", "the image's OCI layout, for its SBOM"),
		meta:     fs.String("meta", "", "the image's |meta, for its provenance"),
	}
	fs.Var(&f.children, "child-meta", "an index child's |meta, once per child")
	fs.Var(&f.childLayouts, "child-layout", "the same child's OCI layout, in the same order")
	return f
}

// resolve is the images to sign or verify, each child of an index in the
// index's repository, and the index's own reference when there is one.
func (f *subjectFlags) resolve(needLayouts bool) ([]subject, string, error) {
	switch {
	case (*f.ref == "") == (*f.indexRef == ""):
		return nil, "", fmt.Errorf("give --ref for an image or --index-ref for an index, one of them")
	case *f.ref != "":
		if len(f.children) > 0 || len(f.childLayouts) > 0 {
			return nil, "", fmt.Errorf("an image has no children; --child-meta and --child-layout are for an index")
		}
		ref, err := readDigest(*f.ref)
		if err != nil {
			return nil, "", err
		}
		s := subject{ref: ref.String(), layout: *f.layout}
		if *f.meta != "" {
			if err := spec.Decode(*f.meta, &s.meta); err != nil {
				return nil, "", err
			}
		}
		if needLayouts && (*f.layout == "" || *f.meta == "") {
			return nil, "", fmt.Errorf("signing an image needs its --layout and --meta")
		}
		return []subject{s}, "", nil
	}
	index, err := readDigest(*f.indexRef)
	if err != nil {
		return nil, "", err
	}
	if *f.index == "" {
		return nil, "", fmt.Errorf("an index is signed by what its own document names; give it with --index")
	}
	if len(f.children) < 2 {
		return nil, "", fmt.Errorf("an index has at least two children; give each with --child-meta")
	}
	if needLayouts && len(f.childLayouts) != len(f.children) {
		return nil, "", fmt.Errorf("%d children and %d layouts; each child gives its layout", len(f.children), len(f.childLayouts))
	}
	raw, err := os.ReadFile(*f.index)
	if err != nil {
		return nil, "", err
	}
	metas := make([]image.Meta, len(f.children))
	for i, p := range f.children {
		if err := spec.Decode(p, &metas[i]); err != nil {
			return nil, "", err
		}
	}
	// Each child's reference is the published index's own entry for it, in
	// the index's repository; a child the index doesn't name, or names
	// differently, is refused.
	refs, err := sign.ChildRefs(raw, index, metas)
	if err != nil {
		return nil, "", err
	}
	var subjects []subject
	for i, ref := range refs {
		s := subject{ref: ref.String(), meta: metas[i]}
		if needLayouts {
			s.layout = f.childLayouts[i]
		}
		subjects = append(subjects, s)
	}
	return subjects, index.String(), nil
}

// runSign signs a published image, or an index and every child, with
// cosign, and attaches each image's SPDX SBOM and SLSA provenance. A runtime
// operation, run after the push by `plz run <image>_sign -- <source flags>`;
// the image itself is never changed.
func runSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	subjects := addSubjectFlags(fs)
	specPath := fs.String("spec", "", "the plugin's signing settings, as JSON")
	target := fs.String("target", "", "the build target the image is, for its provenance")
	cosignPath := fs.String("cosign", "", "the cosign executable")
	syftPath := fs.String("syft", "", "the syft executable")
	key := fs.String("key", "", "the key to sign with (a file or a KMS URI); none signs keyless")
	var cosignFlags stringList
	fs.Var(&cosignFlags, "cosign-flag", "a flag for every cosign invocation, once per flag")
	var src sign.Source
	fs.StringVar(&src.BuildType, "build-type", "", "the provenance's build type")
	fs.StringVar(&src.SourceURI, "source-uri", "", "the source repository the image was built from")
	fs.StringVar(&src.Commit, "commit", "", "the commit it was built at")
	fs.StringVar(&src.BuilderID, "builder-id", "", "what ran the build")
	fs.StringVar(&src.InvocationID, "invocation-id", "", "this run of it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cosignPath == "" || *syftPath == "" {
		return fmt.Errorf("--cosign and --syft are both required")
	}
	signSpec, err := readSignSpec(*specPath)
	if err != nil {
		return err
	}
	override(key, signSpec.Key)
	override(&src.BuildType, signSpec.BuildType)
	images, index, err := subjects.resolve(true)
	if err != nil {
		return err
	}
	cosign := sign.Cosign{Path: *cosignPath, Key: *key, Flags: cosignFlags}
	work, err := os.MkdirTemp("", "oci-sign-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	// Every predicate is written before anything is signed, so a provenance
	// that can't be written stops the run before the registry changes.
	type predicates struct{ sbom, provenance string }
	written := make([]predicates, len(images))
	for i, img := range images {
		p, err := sign.Provenance(*target, img.meta, src)
		if err != nil {
			return err
		}
		written[i].provenance = filepath.Join(work, fmt.Sprintf("provenance%d.json", i))
		if err := os.WriteFile(written[i].provenance, p, 0o644); err != nil {
			return err
		}
		written[i].sbom = filepath.Join(work, fmt.Sprintf("sbom%d.json", i))
		if err := sign.SBOM(*syftPath, img.layout, written[i].sbom); err != nil {
			return err
		}
	}
	for i, img := range images {
		if err := cosign.Sign(img.ref); err != nil {
			return err
		}
		if err := cosign.Attest(img.ref, sign.PredicateSBOM, written[i].sbom); err != nil {
			return err
		}
		if err := cosign.Attest(img.ref, sign.PredicateProvenance, written[i].provenance); err != nil {
			return err
		}
		fmt.Printf("%s: signed, SBOM and provenance attached\n", img.ref)
	}
	if index != "" {
		if err := cosign.Sign(index); err != nil {
			return err
		}
		fmt.Printf("%s: index signed\n", index)
	}
	return nil
}

// runVerify checks that a published image, or an index and every child, is
// signed by the expected signer and carries its SBOM and provenance.
func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	subjects := addSubjectFlags(fs)
	cosignPath := fs.String("cosign", "", "the cosign executable")
	specPath := fs.String("spec", "", "the plugin's signing settings, as JSON")
	var verifier sign.Verifier
	fs.StringVar(&verifier.PublicKey, "public-key", "", "the public key signatures must verify against")
	fs.StringVar(&verifier.Identity, "identity", "", "for keyless signing: a regular expression the certificate identity must match")
	fs.StringVar(&verifier.Issuer, "issuer", "", "for keyless signing: the certificate's OIDC issuer")
	var cosignFlags stringList
	fs.Var(&cosignFlags, "cosign-flag", "a flag for every cosign invocation, once per flag")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cosignPath == "" {
		return fmt.Errorf("--cosign is required")
	}
	signSpec, err := readSignSpec(*specPath)
	if err != nil {
		return err
	}
	override(&verifier.PublicKey, signSpec.PublicKey)
	override(&verifier.Identity, signSpec.Identity)
	override(&verifier.Issuer, signSpec.Issuer)
	images, index, err := subjects.resolve(false)
	if err != nil {
		return err
	}
	cosign := sign.Cosign{Path: *cosignPath, Flags: cosignFlags}
	for _, img := range images {
		// cosign counts an attestation signed by the key as a signature over
		// the digest too, so for an image this adds nothing the attestation
		// checks don't; it is the only check an index gets.
		if err := cosign.Verify(img.ref, verifier); err != nil {
			return err
		}
		for _, predicateType := range []string{sign.PredicateSBOM, sign.PredicateProvenance} {
			if err := cosign.VerifyAttestation(img.ref, predicateType, verifier); err != nil {
				return err
			}
		}
		fmt.Printf("%s: signature, SBOM and provenance verified\n", img.ref)
	}
	if index != "" {
		if err := cosign.Verify(index, verifier); err != nil {
			return err
		}
		fmt.Printf("%s: index signature verified\n", index)
	}
	return nil
}
