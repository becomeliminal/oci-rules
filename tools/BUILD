filegroup(
    name = "release",
    srcs = ["release.build_defs"],
    visibility = ["//package/..."],
)

subinclude(":release")

# The released oci_tool, the plugin's default OciTool. :oci_tool is the host's;
# :oci_tool_<os>_<arch> are the others, for a host whose actions run elsewhere
# (a Mac using Linux remote execution sets OciTool to :oci_tool_linux_amd64).
for platform, hash in OCI_TOOL_HASHES.items():
    remote_file(
        name = "oci_tool" if platform == f"{CONFIG.HOSTOS}_{CONFIG.HOSTARCH}" else f"oci_tool_{platform}",
        binary = True,
        hashes = [hash],
        licences = ["Apache-2.0"],
        url = f"https://github.com/becomeliminal/oci-rules/releases/download/oci-tool-v{OCI_TOOL_VERSION}/oci_tool-{OCI_TOOL_VERSION}-{platform}",
        visibility = ["PUBLIC"],
    )
