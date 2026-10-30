subinclude("//build_defs:oci")

# A public image pulled from Docker Hub by digest, as rules_oci's oci.pull
# pulls one: its linux/amd64 image's manifest digest, every byte checked.
# hello-world, because it is two small blobs. //third_party/oci's postgres_17
# is pulled by an index's digest.
oci_pull(
    name = "hello_world",
    digest = "sha256:d1a8d0a4eeb63aff09f5f34d4d80505e0ba81905f36158cc3970d8e07179e59e",
    image = "docker.io/library/hello-world",
    test_only = True,
)

oci_image(
    name = "image",
    base = ":hello_world",
    test_only = True,
)

oci_layout(
    name = "image_layout",
    image = ":image",
    ref_name = "pulled",
    test_only = True,
)

# The pulled image's identity is pinned, its layer is the locked one, and
# regctl, which shares no code with what fetched it, reads hello-world's
# binary out of the layout.
#
# Regenerate with:
#   plz build //test/pull:image && \
#     cp plz-out/gen/test/pull/image.image.json \
#        test/pull/golden.image.json
gentest(
    name = "pull_test",
    data = {
        "golden": ["golden.image.json"],
        "layout": [":image_layout"],
        "meta": [":image|meta"],
        "regctl": ["//third_party/binary:regctl"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "diff $DATA_GOLDEN $DATA_META",
        "grep -q '\"digest\": \"sha256:4f55086f7dd096d48b0e49be066971a8ed996521c2e190aa21b2435a847198b4\"' $DATA_META",
        "T=$(mktemp -d) && cp -r $DATA_LAYOUT $T/layout && chmod -R u+w $T/layout",
        "HOME=$T $DATA_REGCTL image get-file ocidir://$T/layout:pulled /hello > $T/hello",
        "test \"$(head -c 4 $T/hello | od -An -c | tr -d ' ')\" = '177ELF'",
    ]),
)
