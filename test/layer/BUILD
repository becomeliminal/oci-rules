subinclude("//build_defs:oci")

# One script, as a remote executor leaves a build output (0555) and as a local
# build does (0755). The layer declares its mode, so both give one layer.
# Flat output names: a remote executor does not create an output's parent
# directory before the command runs.
genrule(
    name = "hi_0555",
    outs = ["hi_0555"],
    cmd = "printf '#!/bin/sh\\necho hi\\n' > $OUT && chmod 0555 $OUT",
)

genrule(
    name = "hi_0755",
    outs = ["hi_0755"],
    cmd = "printf '#!/bin/sh\\necho hi\\n' > $OUT && chmod 0755 $OUT",
)

oci_layer(
    name = "from_0555",
    files = {
        "/bin/hi": ":hi_0555",
        "/etc/greeting": "greeting",
    },
    modes = {"/bin/hi": "0755"},
)

oci_layer(
    name = "from_0755",
    files = {
        "/bin/hi": ":hi_0755",
        "/etc/greeting": "greeting",
    },
    modes = {"/bin/hi": "0755"},
)

# The rule gives the same identity as the library's golden test
# (tools/oci_tool/layer/layer_test.go), whichever way its input arrived, locally and on
# remote execution alike.
gentest(
    name = "layer_test",
    data = {
        "golden": ["golden.layer.json"],
        "local": [":from_0755|meta"],
        "remote": [":from_0555|meta"],
    },
    no_test_output = True,
    test_cmd = "diff $DATA_GOLDEN $DATA_LOCAL && diff $DATA_GOLDEN $DATA_REMOTE",
)
