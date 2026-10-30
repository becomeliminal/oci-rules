subinclude("//tools:release")

# oci_tool for every platform a user's host can be, each cross-compiled from
# whatever host runs the build. The tool is pure Go (CgoEnabled = false) and
# links without the toolchain's path, so its bytes depend only on the source
# and the Go version: the same on a Mac as on CI. release.yaml publishes these.
_builds = [build_rule(
    name = f"oci_tool_{platform}",
    srcs = [f"///{platform}//tools/oci_tool"],
    outs = [f"oci_tool-{OCI_TOOL_VERSION}-{platform}"],
    cmd = "cp $SRC $OUT",
) for platform in sorted(OCI_TOOL_HASHES.keys())]

filegroup(
    name = "oci_tool_release_files",
    srcs = _builds,
)

# The pins in tools/release.build_defs are the hashes of what this checkout
# builds on a Linux host, which is what release.yaml builds on. A change to
# oci_tool fails here until it is re-pinned (and its version bumped), so a
# release can never ship bytes the default OciTool would refuse.
#
# Linux hosts only, for now: go-rules' please_go writes the whole output of
# `go version` into a binary's build info ("go version go1.27.1 darwin/arm64"),
# so a Mac's build differs from Linux's in that string and nothing else.
# `go build` writes "go1.27.1". When go-rules does too, this runs on every host.
if CONFIG.HOSTOS == "linux":
    gentest(
        name = "release_hashes_test",
        data = _builds,
        no_test_output = True,
        test_cmd = "; ".join(["status=0"] + [
            f"got=$(shasum -a 256 package/oci_tool-{OCI_TOOL_VERSION}-{platform} | cut -d' ' -f1); test $got = {hash} || {{ echo \"{platform}: built $got, pinned {hash}\"; status=1; }}"
            for platform, hash in sorted(OCI_TOOL_HASHES.items())
        ] + ["exit $status"]),
    )
