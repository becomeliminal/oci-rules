subinclude("//build_defs:oci")

oci_layer(
    name = "greeting_layer",
    files = {"/etc/greeting": "greeting"},
    visibility = ["//test/..."],
)

# An image from scratch: one layer, an entrypoint, an env value, published to
# a repository so it has a reference by digest.
oci_image(
    name = "image",
    cmd = ["/etc/greeting"],
    entrypoint = ["/bin/cat"],
    env = {"LANG": "C.UTF-8"},
    layers = [":greeting_layer"],
    platform = "linux/amd64",
    repository = "registry.example.com/team/greeting",
    visibility = ["//test/..."],
)

# The image's identity is exactly what it has always been, on any executor;
# its reference is the repository at that digest, and its retention tag
# names that digest.
#
# Regenerate with:
#   plz build //test/image:image && \
#     cp plz-out/gen/test/image/image.image.json \
#        test/image/golden.image.json
gentest(
    name = "image_test",
    data = {
        "fqn": [":image_fqn"],
        "golden": ["golden.image.json"],
        "meta": [":image|meta"],
        "retain": [":image_retain"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "diff $DATA_GOLDEN $DATA_META",
        "test \"$(cat $DATA_FQN)\" = \"registry.example.com/team/greeting@$(sed -n 's/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $DATA_GOLDEN | head -1)\"",
        "test \"$(cat $DATA_RETAIN)\" = \"registry.example.com/team/greeting:keep-sha256-$(sed -n 's/.*\"digest\": \"sha256:\\([0-9a-f]*\\)\".*/\\1/p' $DATA_GOLDEN | head -1)\"",
    ]),
)

# The image as a Docker archive, loaded by `docker load`.
oci_load(
    name = "image_load",
    image = ":image",
)

# The archive is the same bytes on any executor, and it loads as the image's
# retention tag, which is also what a manifest running image_load names.
#
# The hash is the archive's sha256; after an intended change, rebuild
# :_image_load#archive and take `shasum -a 256` of image_load.tar.
#
# shasum, not coreutils' sha256sum: shasum comes with perl, so it is on every
# Mac and every Linux host with perl, GitHub's runners included.
gentest(
    name = "image_load_test",
    data = {
        "archive": [":_image_load#archive"],
        "fqn": [":image_load_fqn"],
        "retain": [":image_retain"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "test \"$(shasum -a 256 $DATA_ARCHIVE | cut -d' ' -f1)\" = 1768b7681efac5b264d77f5325999fc2723c2cb8c565ad9999cf8bc81466adde",
        "test \"$(cat $DATA_FQN)\" = \"$(cat $DATA_RETAIN)\"",
        "tar -xOf $DATA_ARCHIVE manifest.json | grep -F \"\\\"RepoTags\\\":[\\\"$(cat $DATA_RETAIN)\\\"]\" > /dev/null",
    ]),
)

# The image as an OCI image layout, named "greeting" in its index.
oci_layout(
    name = "image_layout",
    image = ":image",
    ref_name = "greeting",
    visibility = ["//test/..."],
)

# The image as an OCI archive and as a Docker archive.
oci_archive(
    name = "image_oci",
    format = "oci",
    image = ":image",
    ref_name = "greeting",
)

oci_archive(
    name = "image_docker",
    format = "docker",
    image = ":image",
    tags = [
        "registry.example.com/team/greeting:stable",
        "registry.example.com/team/greeting:v1",
    ],
)

# The image tested without running it, and with no Docker daemon: its
# metadata, file and content checks through container-structure-test, and
# its owner and (absent) user through oci-tool.
oci_test(
    name = "image_structure_test",
    configs = ["image_test.yaml"],
    image = ":image",
    owners = {"/etc/greeting": {
        "uid": 0,
        "gid": 0,
    }},
    user = "",
)

# The layout is read by regctl, an OCI client that shares no code with the
# one that wrote it: it finds the image by its ref.name at the golden digest,
# and reads the file out of its layer.
gentest(
    name = "image_layout_test",
    data = {
        "golden": ["golden.image.json"],
        "layout": [":image_layout"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "T=$(mktemp -d) && cp -r $DATA_LAYOUT $T/layout && chmod -R u+w $T/layout",
        "test \"$(HOME=$T $DATA_REGCTL image digest ocidir://$T/layout:greeting)\" = \"$(sed -n 's/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $DATA_GOLDEN | head -1)\"",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:greeting /etc/greeting)\" = 'hello, image'",
    ]),
)

# Both archives are the same bytes on any host, and regctl imports each: the
# OCI archive as exactly the built image, the Docker archive (which carries
# no manifest of its own) as the built image's config, sha256:d0c60143..., and
# its file.
#
# After an intended change, rebuild :image_oci and :image_docker and take
# `shasum -a 256` of image_oci.tar and image_docker.tar.
gentest(
    name = "image_archives_test",
    data = {
        "docker": [":image_docker"],
        "golden": ["golden.image.json"],
        "oci": [":image_oci"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "test \"$(shasum -a 256 $DATA_OCI | cut -d' ' -f1)\" = da1d4eed3c4350feece183c068e88b79faf055e8a2facce762cd60c74b552887",
        "test \"$(shasum -a 256 $DATA_DOCKER | cut -d' ' -f1)\" = dd974ea43f4e1f9e368cdffd46ffbead1516f7af5017180b2502eda7a428d703",
        "T=$(mktemp -d)",
        "HOME=$T $DATA_REGCTL image import ocidir://$T/oci:x $DATA_OCI",
        "test \"$(HOME=$T $DATA_REGCTL image digest ocidir://$T/oci:x)\" = \"$(sed -n 's/.*\"digest\": \"\\(sha256:[0-9a-f]*\\)\".*/\\1/p' $DATA_GOLDEN | head -1)\"",
        "HOME=$T $DATA_REGCTL image import ocidir://$T/docker:x $DATA_DOCKER",
        "test \"$(HOME=$T $DATA_REGCTL image get-file ocidir://$T/docker:x /etc/greeting)\" = 'hello, image'",
        "test \"$(HOME=$T $DATA_REGCTL manifest get --format '{{.Config.Digest}}' ocidir://$T/docker:x)\" = sha256:d0c60143770a3856d6ada8427f6530e5a8afb19bbd6dbdef798ddf6d6f190d02",
    ]),
)
