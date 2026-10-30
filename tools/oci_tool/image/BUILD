go_library(
    name = "image",
    srcs = [
        "image.go",
        "index.go",
    ],
    visibility = ["//tools/oci_tool/..."],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/types",
    ],
)

go_test(
    name = "image_test",
    srcs = [
        "image_test.go",
        "index_test.go",
    ],
    deps = [
        ":image",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/types",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
