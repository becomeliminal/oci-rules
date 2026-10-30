subinclude("//build_defs:oci")

# Two layers with one target name, from two packages, in one image: its
# _blobs, its archive and its publisher must hold both.
oci_image(
    name = "image",
    layers = [
        "//test/collide/one:app_layer",
        "//test/collide/two:app_layer",
    ],
    platform = "linux/amd64",
    repository = "registry.example.com/team/collide",
    test_only = True,
)

oci_archive(
    name = "image_oci",
    image = ":image",
    test_only = True,
)

# Both layers are in the image: regctl reads each one's file out of the
# archive.
gentest(
    name = "collide_test",
    data = {
        "archive": [":image_oci"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "T=$(mktemp -d)",
        "HOME=$T $DATA_REGCTL image import ocidir://$T/image:x $DATA_ARCHIVE",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/image:x /one.txt)\" = 'from package one'",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/image:x /two.txt)\" = 'from package two'",
    ]),
)
