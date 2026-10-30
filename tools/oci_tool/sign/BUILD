go_library(
    name = "sign",
    srcs = ["sign.go"],
    visibility = ["//tools/oci_tool/..."],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "//tools/oci_tool/image",
    ],
)

go_test(
    name = "sign_test",
    srcs = ["sign_test.go"],
    deps = [
        ":sign",
        "///third_party/go/github.com_google_go-containerregistry//pkg/name",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1",
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/types",
        "//tools/oci_tool/image",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
