go_library(
    name = "layer",
    srcs = [
        "binary.go",
        "layer.go",
    ],
    visibility = ["//tools/oci_tool/..."],
)

go_test(
    name = "layer_test",
    srcs = [
        "binary_test.go",
        "edge_test.go",
        "layer_test.go",
    ],
    deps = [
        ":layer",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
