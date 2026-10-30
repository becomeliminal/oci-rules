# The oci plugin's build tool. This repository's own OciTool points here
# (.plzconfig), so its tests run this source; a user's defaults to the released
# build of it, //tools:oci_tool, pinned by sha256.
go_binary(
    name = "oci_tool",
    # The linker otherwise embeds the Go toolchain's absolute path (where this
    # checkout happens to be) as runtime.GOROOT, so the same source built in
    # two directories would differ. Cleared, the binary depends only on its
    # source and the Go version, and //tools:oci_tool can pin a release by the
    # hash of a build made anywhere.
    definitions = {"runtime.defaultGOROOT": ""},
    srcs = [
        "archive.go",
        "build.go",
        "check.go",
        "index.go",
        "main.go",
        "pull.go",
        "push.go",
        "signcmd.go",
        "tar.go",
    ],
    visibility = ["PUBLIC"],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/authn",
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/empty",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/layout",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/remote",
        "//tools/oci_tool/archive",
        "//tools/oci_tool/image",
        "//tools/oci_tool/inspect",
        "//tools/oci_tool/layer",
        "//tools/oci_tool/publish",
        "//tools/oci_tool/sign",
        "//tools/oci_tool/spec",
    ],
)

go_test(
    name = "oci_tool_test",
    srcs = [
        "archive.go",
        "build.go",
        "check.go",
        "index.go",
        "main.go",
        "main_test.go",
        "pull.go",
        "pull_test.go",
        "push.go",
        "signcmd.go",
        "tar.go",
        "tar_test.go",
    ],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/authn",
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/registry",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/empty",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/layout",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/mutate",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/random",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/remote",
        "//tools/oci_tool/archive",
        "//tools/oci_tool/image",
        "//tools/oci_tool/inspect",
        "//tools/oci_tool/layer",
        "//tools/oci_tool/publish",
        "//tools/oci_tool/sign",
        "//tools/oci_tool/spec",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
