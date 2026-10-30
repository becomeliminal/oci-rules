package main

import (
	"bytes"
	"flag"
	"fmt"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/becomeliminal/oci-rules/tools/oci_tool/inspect"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/publish"
	"github.com/becomeliminal/oci-rules/tools/oci_tool/spec"
)

// checkSpec is what oci_test writes for the checks container-structure-test
// cannot make.
type checkSpec struct {
	// Owners maps an absolute image path to the numeric owner it must have.
	Owners map[string]inspect.Owner `json:"owners,omitempty"`
	// User, when set, is exactly the config's user; "" means none is set.
	// container-structure-test reads an empty expected user as "don't
	// check", so it cannot assert that.
	User *string `json:"user,omitempty"`
}

// runCheck checks a built image's flattened filesystem against a checkSpec. It
// runs as a test: it reads only its inputs, and fails naming every path
// that is not as the spec says.
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the check spec, as JSON")
	manifestPath := fs.String("manifest", "", "the image's manifest")
	configPath := fs.String("config", "", "the image's config")
	var blobs stringList
	fs.Var(&blobs, "blob", "a layer blob, once per blob")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *manifestPath == "" || *configPath == "" {
		return fmt.Errorf("--spec, --manifest and --config are all required")
	}
	var checkSpec checkSpec
	if err := spec.Decode(*specPath, &checkSpec); err != nil {
		return err
	}
	if len(checkSpec.Owners) == 0 && checkSpec.User == nil {
		return fmt.Errorf("%s checks nothing", *specPath)
	}
	img, err := publish.Load(*manifestPath, *configPath, blobs)
	if err != nil {
		return err
	}
	var problems []string
	if checkSpec.User != nil {
		cfg, err := v1.ParseConfigFile(bytes.NewReader(img.Config))
		if err != nil {
			return fmt.Errorf("parsing the image's config: %w", err)
		}
		if cfg.Config.User != *checkSpec.User {
			problems = append(problems, fmt.Sprintf("the image runs as user %q, want %q", cfg.Config.User, *checkSpec.User))
		}
	}
	if len(checkSpec.Owners) > 0 {
		flat, err := inspect.Flatten(img)
		if err != nil {
			return err
		}
		problems = append(problems, inspect.CheckOwners(flat, checkSpec.Owners)...)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d things are not as the test says:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	fmt.Printf("%d owners and the user as declared\n", len(checkSpec.Owners))
	return nil
}
