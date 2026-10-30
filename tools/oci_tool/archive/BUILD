go_library(
    name = "archive",
    srcs = [
        "archive.go",
        "read.go",
    ],
    visibility = ["//tools/oci_tool/..."],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/empty",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/layout",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/tarball",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/types",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/validate",
        "//tools/oci_tool/image",
        "//tools/oci_tool/publish",
    ],
)

go_test(
    name = "archive_test",
    srcs = [
        "archive_test.go",
        "read_test.go",
    ],
    deps = [
        ":archive",
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/empty",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/layout",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/mutate",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/random",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/tarball",
        "//tools/oci_tool/image",
        "//tools/oci_tool/layer",
        "//tools/oci_tool/publish",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
