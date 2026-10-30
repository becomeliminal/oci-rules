subinclude("//build_defs:oci")

# The greeting image's OCI layout, read back as a base: what apko writes is
# the same shape.
oci_import(
    name = "base",
    layout = "//test/image:image_layout",
    test_only = True,
)

# An image on the imported base that adds nothing.
oci_image(
    name = "image",
    base = ":base",
    test_only = True,
)

oci_layout(
    name = "image_layout",
    image = ":image",
    ref_name = "imported",
    test_only = True,
)

# Importing loses nothing: an image that adds nothing to the imported base
# is the greeting image exactly, digest and all, and its blobs come through
# the import's blob directory, so regctl reads the file out of its layout.
gentest(
    name = "import_test",
    data = {
        "greeting": ["//test/image:image|meta"],
        "layout": [":image_layout"],
        "meta": [":image|meta"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "diff $DATA_GREETING $DATA_META",
        "T=$(mktemp -d) && cp -r $DATA_LAYOUT $T/layout && chmod -R u+w $T/layout",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:imported /etc/greeting)\" = 'hello, image'",
    ]),
)
