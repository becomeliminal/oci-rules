subinclude("//build_defs:oci")

oci_layer(
    name = "farewell_layer",
    files = {"/etc/farewell": "farewell"},
    test_only = True,
)

# An image built on an image built in the repository: the greeting image's
# layer, then its own, with the greeting image's entrypoint and a new cmd.
oci_image(
    name = "image",
    base = "//test/image:image",
    cmd = ["/etc/farewell"],
    layers = [":farewell_layer"],
    test_only = True,
)

oci_layout(
    name = "image_layout",
    image = ":image",
    ref_name = "stacked",
    test_only = True,
)

# The image's identity is pinned, its first layer is the greeting image's
# and its second its own, and regctl, which shares no code with what wrote
# the layout, finds it at that digest and reads both files out of it.
#
# Regenerate with:
#   plz build //test/stacked:image && \
#     cp plz-out/gen/test/stacked/image.image.json \
#        test/stacked/golden.image.json
gentest(
    name = "stacked_test",
    data = {
        "base": ["//test/image:image|meta"],
        "golden": ["golden.image.json"],
        "layout": [":image_layout"],
        "meta": [":image|meta"],
        "own": [":farewell_layer|meta"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "diff $DATA_GOLDEN $DATA_META",
        "layers() { sed -n '/\"layers\"/,$ s/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $1; }",
        "test \"$(layers $DATA_META)\" = \"$(layers $DATA_BASE; sed -n 's/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $DATA_OWN)\"",
        "T=$(mktemp -d) && cp -r $DATA_LAYOUT $T/layout && chmod -R u+w $T/layout",
        "test \"$(HOME=$T $DATA_REGCTL image digest ocidir://$T/layout:stacked)\" = \"$(sed -n 's/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $DATA_GOLDEN | head -1)\"",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:stacked /etc/greeting)\" = 'hello, image'",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:stacked /etc/farewell)\" = 'goodbye, image'",
    ]),
)
