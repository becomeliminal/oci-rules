subinclude("//build_defs:oci")

# A layer named like the one in the other package: the two meet in one
# image's _blobs, which must hold both.
oci_layer(
    name = "app_layer",
    files = {"/two.txt": "file.txt"},
    test_only = True,
    visibility = ["//test/collide/..."],
)
