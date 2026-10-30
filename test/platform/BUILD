subinclude("//build_defs:oci")

go_binary(
    name = "hello",
    srcs = ["main.go"],
)

# A layer for linux/amd64: its binary is built in that platform's
# architecture subrepo, whatever the host.
oci_layer(
    name = "hello_layer",
    files = {"/hello": ":hello"},
    modes = {"/hello": "0755"},
    platform = "linux/amd64",
)

# The layer records the binary it holds and the platform it was built for:
# what oci_image checks against its own.
gentest(
    name = "hello_layer_test",
    data = {"meta": [":hello_layer|meta"]},
    no_test_output = True,
    test_cmd = "grep -qF '\"/hello\": \"linux/amd64\"' $DATA_META",
)
