go_library(
    name = "inspect",
    srcs = ["inspect.go"],
    visibility = ["//tools/oci_tool/..."],
    deps = [
        "///third_party/go/github.com_google_go-containerregistry//pkg/v1/mutate",
        "//tools/oci_tool/publish",
    ],
)

go_test(
    name = "inspect_test",
    srcs = ["inspect_test.go"],
    deps = [
        ":inspect",
        "//tools/oci_tool/image",
        "//tools/oci_tool/layer",
        "//tools/oci_tool/publish",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
