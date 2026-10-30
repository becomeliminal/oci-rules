# The executables this repository's tests run, each pinned by sha256 for every
# platform CI and contributors build on. A user of the plugin pins their own
# and points the oci plugin's settings at them (README.md, "Installing").

_PLATFORM = f"{CONFIG.HOSTOS}_{CONFIG.HOSTARCH}"

def _pinned(name:str, version:str, url:str, hashes:dict, extract:str=None, licences:list=[]):
    if _PLATFORM not in hashes:
        fail(f"{name}: no pin for {_PLATFORM}; pinned for " + ", ".join(sorted(hashes.keys())))
    goos, _, goarch = _PLATFORM.partition("_")
    url = url.format(version = version, os = goos, arch = goarch)
    if not extract:
        return remote_file(
            name = name,
            binary = True,
            hashes = [hashes[_PLATFORM]],
            licences = licences,
            url = url,
            visibility = ["PUBLIC"],
        )
    archive = remote_file(
        name = f"_{name}#archive",
        hashes = [hashes[_PLATFORM]],
        licences = licences,
        url = url,
    )
    return genrule(
        name = name,
        srcs = [archive],
        outs = [name],
        binary = True,
        cmd = f"mkdir _x && tar -xzf $SRC -C _x {extract} && mv _x/{extract} $OUT",
        visibility = ["PUBLIC"],
    )

# container-structure-test, which oci_test runs with its tar driver: file and
# metadata checks against an image archive, with no Docker daemon.
_pinned(
    name = "container-structure-test",
    hashes = {
        "darwin_amd64": "1ca7265ed9061785a39cda83d5a8104c769bbf432dab40a269596ea6f1a13c45",
        "darwin_arm64": "ba00172029ecec3afda7fd93a5eee7cdaef710196df7103689da3386eba56b0a",
        "linux_amd64": "fa35e89512a8978585f76cf41397956d2e3a30c62c2ad3fb857b1597074d14ca",
        "linux_arm64": "801826ed107222120eb6df8c143b7de752cfebbe3aebfeea576e7098486917c2",
    },
    licences = ["Apache-2.0"],
    url = "https://github.com/GoogleContainerTools/container-structure-test/releases/download/v{version}/container-structure-test-{os}-{arch}",
    version = "1.22.1",
)

# cosign, which <image>_sign and <image>_verify run.
_pinned(
    name = "cosign",
    hashes = {
        "darwin_amd64": "2347488e5d5b25336644024dfeca5601b190e91197a71a917bda44744aff106c",
        "darwin_arm64": "5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76",
        "linux_amd64": "4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71",
        "linux_arm64": "c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a",
    },
    licences = ["Apache-2.0"],
    url = "https://github.com/sigstore/cosign/releases/download/v{version}/cosign-{os}-{arch}",
    version = "3.1.3",
)

# syft, which writes the SPDX SBOM <image>_sign attests.
_pinned(
    name = "syft",
    extract = "syft",
    hashes = {
        "darwin_amd64": "56975f5d7ffa9846a1eaf64330647841b878097bc7e3730cb9325f93add96917",
        "darwin_arm64": "014d561b6d13059124155f74a6c5a9a99501f5e209313638dd884f39eb418ee6",
        "linux_amd64": "caeedb81fb0491615f1ebd1761e4145d41ee86dd2cc7bf80669f9f5ad9d6133d",
        "linux_arm64": "c46d5e4c28e12aa4c5becfaa343ef1c7f89045b6b895f2c21d471c62db09c706",
    },
    licences = ["Apache-2.0"],
    url = "https://github.com/anchore/syft/releases/download/v{version}/syft_{version}_{os}_{arch}.tar.gz",
    version = "1.52.0",
)

# regctl, an OCI client that is not go-containerregistry: the tests read the
# layouts oci_tool writes with it, so a layout is checked by an implementation
# other than the one that wrote it.
_pinned(
    name = "regctl",
    hashes = {
        "darwin_amd64": "d651381c58357e9b9446cec172761c7418b7621da57960d438175d8f4c5b479d",
        "darwin_arm64": "5b52b71861abc300ab513e87a4c3794cff6508035ac7f6b6bdfa1fa5688c0040",
        "linux_amd64": "8e0e62a497fcdb8048d18aa927a139613176ba0531f412bc541044e28f9856bd",
        "linux_arm64": "a9b71a3ee79b2d1dbbd7d51fd5e8fa214722c192864235d3d8764463c751a1ff",
    },
    licences = ["Apache-2.0"],
    url = "https://github.com/regclient/regclient/releases/download/v{version}/regctl-{os}-{arch}",
    version = "0.11.6",
)
