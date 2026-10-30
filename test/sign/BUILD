# The image signed end to end, with a throwaway key and no network: cosign
# signs the published digest and attaches the SBOM (syft) and SLSA
# provenance; verification passes, the image is unchanged, and another key
# and an unsigned image are refused.
gentest(
    name = "sign_test",
    data = {
        "blobs": ["//test/image:image_blobs"],
        "amd64_config": ["//test/index:image_amd64|config"],
        "amd64_layout": ["//test/index:image_amd64_layout"],
        "amd64_manifest": ["//test/index:image_amd64|manifest"],
        "amd64_meta": ["//test/index:image_amd64|meta"],
        "arm64_config": ["//test/index:image_arm64|config"],
        "arm64_layout": ["//test/index:image_arm64_layout"],
        "arm64_manifest": ["//test/index:image_arm64|manifest"],
        "arm64_meta": ["//test/index:image_arm64|meta"],
        "config": ["//test/image:image|config"],
        "index": ["//test/index:index|index"],
        "index_blobs": ["//test/index:index_blobs"],
        "index_digest": ["//test/index:index|digest"],
        "cosign": ["//third_party/binary:cosign"],
        "digest": ["//test/image:image|digest"],
        "layout": ["//test/image:image_layout"],
        "manifest": ["//test/image:image|manifest"],
        "meta": ["//test/image:image|meta"],
        "oci_tool": ["//tools/oci_tool:oci_tool"],
        "other_blobs": ["//test/index:image_amd64_blobs"],
        "other_config": ["//test/index:image_amd64|config"],
        "other_digest": ["//test/index:image_amd64|digest"],
        "other_manifest": ["//test/index:image_amd64|manifest"],
        "regctl": ["//third_party/binary:regctl"],
        "registry": ["//test/sign/registry"],
        "script": ["sign_test.sh"],
        "syft": ["//third_party/binary:syft"],
    },
    no_test_output = True,
    test_cmd = "bash $DATA_SCRIPT",
)

# A deploy gives _sign and _verify its source and run flags at run time, so
# the scripts must pass their arguments on: sh_cmd expands "$@" while
# building unless it is escaped, which once left "" here instead.
gentest(
    name = "sign_forwards_its_arguments_test",
    data = {
        "sign": ["//test/image:image_sign"],
        "verify": ["//test/image:image_verify"],
    },
    no_test_output = True,
    test_cmd = "tail -1 $DATA_SIGN | grep -q ' \"\\$@\"$' && tail -1 $DATA_VERIFY | grep -q ' \"\\$@\"$'",
)
