subinclude("///go//build_defs:go")

go_toolchain(
    name = "toolchain",
    install_std = False,
    version = "1.27.1",
)

# The standard library, recompiled for each platform an image or a release is
# built for, so oci_tool and the tests' Go binaries cross-compile from any host.
go_stdlib(
    name = "std",
    tags = [
        "netgo",
        "osusergo",
    ],
)

# Versions are what `go mod tidy` selects for go.mod's two requirements.

go_repo(
    licences = ["Apache-2.0"],
    module = "github.com/google/go-containerregistry",
    version = "v0.22.1",
)

go_repo(
    licences = ["Apache-2.0"],
    module = "github.com/docker/cli",
    # +incompatible: no go.mod, so its requirements are named here.
    requirements = [
        "github.com/docker/docker-credential-helpers",
        "github.com/sirupsen/logrus",
    ],
    version = "v29.7.2+incompatible",
)

go_repo(
    licences = ["MIT"],
    module = "github.com/docker/docker-credential-helpers",
    version = "v0.9.3",
)

go_repo(
    licences = ["Apache-2.0"],
    module = "github.com/opencontainers/go-digest",
    version = "v1.0.0",
)

go_repo(
    licences = ["Apache-2.0"],
    module = "github.com/opencontainers/image-spec",
    version = "v1.1.1",
)

go_repo(
    licences = ["Apache-2.0", "BSD-3-Clause", "MIT"],
    module = "github.com/klauspost/compress",
    version = "v1.19.2",
)

go_repo(
    licences = ["MIT"],
    module = "github.com/sirupsen/logrus",
    version = "v1.9.4",
)

go_repo(
    licences = ["BSD-3-Clause"],
    module = "golang.org/x/sync",
    version = "v0.22.0",
)

go_repo(
    licences = ["BSD-3-Clause"],
    module = "golang.org/x/sys",
    version = "v0.47.0",
)

go_repo(
    licences = ["MIT"],
    module = "github.com/stretchr/testify",
    version = "v1.10.0",
)

go_repo(
    licences = ["ISC"],
    module = "github.com/davecgh/go-spew",
    version = "v1.1.1",
)

go_repo(
    licences = ["BSD-3-Clause"],
    module = "github.com/pmezard/go-difflib",
    version = "v1.0.0",
)

go_repo(
    licences = ["MIT", "Apache-2.0"],
    module = "gopkg.in/yaml.v3",
    version = "v3.0.1",
)

go_repo(
    licences = ["BSD-3-Clause"],
    module = "github.com/google/go-cmp",
    version = "v0.7.0",
)
