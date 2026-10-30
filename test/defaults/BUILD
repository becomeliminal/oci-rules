subinclude("//build_defs:oci")

# The image of //test/image:image with no
# platform, which comes from the plugin's DefaultPlatform, and a short
# repository, which is in its Registry.
oci_image(
    name = "image",
    cmd = ["/etc/greeting"],
    entrypoint = ["/bin/cat"],
    env = {"LANG": "C.UTF-8"},
    layers = ["//test/image:greeting_layer"],
    repository = "team/greeting",
    test_only = True,
)

# A repository whose first component is a registry host is used as given,
# whatever the Registry.
oci_image(
    name = "image_full",
    cmd = ["/etc/greeting"],
    entrypoint = ["/bin/cat"],
    env = {"LANG": "C.UTF-8"},
    layers = ["//test/image:greeting_layer"],
    repository = "localhost:5000/team/greeting",
    test_only = True,
)

# The defaults change nothing but how the image is written: it is the same
# image, and it is published to <Registry>/team/greeting.
gentest(
    name = "defaults_test",
    data = {
        "explicit": ["//test/image:image|meta"],
        "fqn": [":image_fqn"],
        "full": [":image_full_fqn"],
        "meta": [":image|meta"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "diff $DATA_EXPLICIT $DATA_META",
        "digest=$(sed -n 's/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $DATA_META | head -1)",
        f"test \"$(cat $DATA_FQN)\" = \"{CONFIG.OCI.REGISTRY}/team/greeting@$digest\"",
        "test \"$(cat $DATA_FULL)\" = \"localhost:5000/team/greeting@$digest\"",
    ]),
)
