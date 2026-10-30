go_library(
    name = "spec",
    srcs = ["spec.go"],
    visibility = ["//tools/oci_tool/..."],
)

go_test(
    name = "spec_test",
    srcs = ["spec_test.go"],
    deps = [
        ":spec",
        "///third_party/go/github.com_stretchr_testify//require",
    ],
)
