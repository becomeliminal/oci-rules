go_library(
    name = "publish",
    srcs = [
        "index.go",
        "publish.go",
        "retain.go",
    ],
    visibility = ["//tools/oci_tool/..."],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/partial",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/remote",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/remote/transport",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/types",
    ],
)

go_test(
    name = "publish_test",
    srcs = [
        "index_test.go",
        "publish_test.go",
    ],
    deps = [
        ":publish",
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/registry",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/partial",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/random",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/remote",
        "//tools/oci_tool/image",
        "//tools/oci_tool/layer",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
