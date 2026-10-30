subinclude("//build_defs:oci")

# A real two-platform image: regctl's own static release binaries, one per
# platform, pinned by sha256. Each layer declares its platform, so the
# binary's header is checked against it, and the image refuses a binary of
# the other platform.
remote_file(
    name = "regctl_amd64",
    binary = True,
    hashes = ["8e0e62a497fcdb8048d18aa927a139613176ba0531f412bc541044e28f9856bd"],
    test_only = True,
    url = "https://github.com/regclient/regclient/releases/download/v0.11.6/regctl-linux-amd64",
)

remote_file(
    name = "regctl_arm64",
    binary = True,
    hashes = ["a9b71a3ee79b2d1dbbd7d51fd5e8fa214722c192864235d3d8764463c751a1ff"],
    test_only = True,
    url = "https://github.com/regclient/regclient/releases/download/v0.11.6/regctl-linux-arm64",
)

[oci_layer(
    name = f"{arch}_layer",
    files = {"/regctl": f":regctl_{arch}"},
    modes = {"/regctl": "0755"},
    platform = f"linux/{arch}",
    test_only = True,
) for arch in [
    "amd64",
    "arm64",
]]

[oci_image(
    name = f"image_{arch}",
    entrypoint = ["/regctl"],
    layers = [f":{arch}_layer"],
    platform = f"linux/{arch}",
    test_only = True,
    visibility = ["//test/..."],
) for arch in [
    "amd64",
    "arm64",
]]

oci_image_index(
    name = "index",
    images = [
        ":image_arm64",
        ":image_amd64",
    ],
    repository = "registry.example.com/team/regctl",
    test_only = True,
    visibility = ["//test/..."],
)

# The index is exactly what it has always been, on any executor, and lists
# each child under that child's own manifest digest, amd64 before arm64.
#
# After an intended change, rebuild :index and take index.digest.
#
# shasum, not coreutils' sha256sum: shasum comes with perl, so it is on every
# Mac and every Linux host with perl, GitHub's runners included.
gentest(
    name = "index_test",
    data = {
        "amd64": [":image_amd64|digest"],
        "arm64": [":image_arm64|digest"],
        "digest": [":index|digest"],
        "index": [":index|index"],
        "retain": [":index_retain"],
    },
    no_test_output = True,
    test_cmd = " && ".join([
        "test \"$(cat $DATA_DIGEST)\" = sha256:bf2e70f39d781a50a0d6051eae7cf3448d3faea933c99811d30e7d092b6b32e4",
        "test \"$(shasum -a 256 $DATA_INDEX | cut -d' ' -f1)\" = bf2e70f39d781a50a0d6051eae7cf3448d3faea933c99811d30e7d092b6b32e4",
        "grep -qF \"\\\"digest\\\":\\\"$(cat $DATA_AMD64)\\\",\\\"platform\\\":{\\\"architecture\\\":\\\"amd64\\\",\\\"os\\\":\\\"linux\\\"}\" $DATA_INDEX",
        "grep -qF \"\\\"digest\\\":\\\"$(cat $DATA_ARM64)\\\",\\\"platform\\\":{\\\"architecture\\\":\\\"arm64\\\",\\\"os\\\":\\\"linux\\\"}\" $DATA_INDEX",
        "test \"$(grep -o 'architecture\":\"[a-z0-9]*' $DATA_INDEX | tr '\\n' ' ')\" = 'architecture\":\"amd64 architecture\":\"arm64 '",
        "test \"$(cat $DATA_RETAIN)\" = registry.example.com/team/regctl:keep-sha256-bf2e70f39d781a50a0d6051eae7cf3448d3faea933c99811d30e7d092b6b32e4",
    ]),
)

# Each child as a layout, for the signing test's SBOMs.
[oci_layout(
    name = f"image_{arch}_layout",
    image = f":image_{arch}",
    test_only = True,
    visibility = ["//test/..."],
) for arch in [
    "amd64",
    "arm64",
]]
