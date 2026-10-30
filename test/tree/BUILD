subinclude("//build_defs:oci")

# A tree: every file of the docs filegroup, under /data at its path in the
# docs package, however many there are.
oci_layer(
    name = "docs_layer",
    test_only = True,
    trees = {"/data": "//test/tree/docs:docs"},
)

oci_image(
    name = "image",
    layers = [":docs_layer"],
    test_only = True,
)

oci_layout(
    name = "image_layout",
    image = ":image",
    ref_name = "tree",
    test_only = True,
)

# regctl, which shares no code with what wrote the layout, reads both files
# from where the tree put them, and the layer is the same bytes on any
# executor: its digest is pinned.
#
# Regenerate with:
#   plz build //test/tree:docs_layer && \
#     cp plz-out/gen/test/tree/docs_layer.layer.json \
#        test/tree/golden.layer.json
gentest(
    name = "tree_test",
    data = {
        "golden": ["golden.layer.json"],
        "layout": [":image_layout"],
        "meta": [":docs_layer|meta"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "diff $DATA_GOLDEN $DATA_META",
        "T=$(mktemp -d) && cp -r $DATA_LAYOUT $T/layout && chmod -R u+w $T/layout",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:tree /data/a.md)\" = alpha",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:tree /data/sub/b.md)\" = beta",
    ]),
)
