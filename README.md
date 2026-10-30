# oci rules for Please

Container images as ordinary build outputs. Each rule is a build action that
reads only what it declares and runs with no network and no credentials, so an
image is built, cached and tested like any other target, locally or on remote
execution, and publishing it is a separate step that uploads bytes already
built.

It is a [Please](https://please.build) plugin, used like the please-build
ones: one `plugin_repo` and a `[Plugin "oci"]` section. Please 17.31.2 or
later.

## Installing

```python
# plugins/BUILD
plugin_repo(
    name = "oci",
    owner = "becomeliminal",
    plugin = "oci-rules",
    revision = "v0.1.0",
)
```

```ini
# .plzconfig
[Plugin "oci"]
Target = //plugins:oci
DefaultPlatform = linux/amd64
Registry = registry.example.com/team
```

```python
# any BUILD file
subinclude("///oci//build_defs:oci")
```

The rules run `oci_tool`, a Go program in this repository (`tools/oci_tool`).
By default `OciTool` is its release for your host, downloaded once and checked
against a sha256 pinned in `tools/release.build_defs`; nothing else needs
installing. Darwin and Linux, amd64 and arm64, are released. A host whose
actions run somewhere else, such as a Mac using Linux remote execution, points
`OciTool` at that platform's build: `///oci//tools:oci_tool_linux_amd64`.

`oci_test`, signing and SBOMs run tools you pin yourself, by sha256, wherever
you keep third-party binaries:

```ini
[Plugin "oci"]
StructureTestTool = //third_party/binary:container-structure-test
CosignTool = //third_party/binary:cosign
SyftTool = //third_party/binary:syft
ProvenanceBuildType = https://example.com/your-build/image@v1
```

`third_party/binary/BUILD` here pins the versions this repository's tests run
(container-structure-test 1.22.1, cosign 3.1.3, syft 1.52.0); copy those pins
or choose your own. `StructureTestTool` is needed only by `oci_test` with
`configs`. `CosignTool`, `SyftTool` and `ProvenanceBuildType` turn on signing
(below). `.plzconfig` lists every setting with its help text.

`DefaultPlatform` is the platform of an `oci_image` or `oci_layer` that names
none. Without it an `oci_image` must name its own, and the build fails if it
doesn't; nothing guesses a platform.

`Registry` lets a `repository` be a short name: `repository = "app"` is
`registry.example.com/team/app`. A repository whose first component is a
registry host (it holds a `.` or a `:`, or is `localhost`, Docker's own rule)
is used as given, so a full one always works. A short name with no `Registry`
fails the build. `repository` itself has no default: where an image is
published is written down, and renaming a target never moves its images.

## `oci_layer`

```python
subinclude("//build_defs:oci")

oci_layer(
    name = "app_layer",
    files = {"/app": "//app:app"},
    modes = {"/app": "0755"},
    platform = "linux/amd64",
)
```

A filesystem layer. Two named outputs:

| output | |
| --- | --- |
| `:app_layer\|blob` | the compressed layer, `application/vnd.oci.image.layer.v1.tar+gzip`: what a registry stores |
| `:app_layer\|meta` | JSON: `digest` and `size` of the blob, `diff_id` of the uncompressed tar, `media_type`, `schema_version`, and `binaries`: each executable's image path and the platform its header names |

An image's config and manifest need only `|meta`. Only publishing and complete
layouts read `|blob`.

| argument | |
| --- | --- |
| `files` | absolute image path to the label or file placed there; each must produce exactly one file (select one with `\|output`) |
| `trees` | absolute image directory to a rule whose every output is placed under it at its path relative to that rule's package: `{"/data": "//docs:docs"}` puts `docs/faq/a.md` at `/data/faq/a.md`. For a set of data files a `files` map cannot name (a glob). Each file is `0644`; an executable belongs in `files`. A file outside the rule's package fails the build |
| `tars` | rules whose output is a tarball, plain or gzipped (a package's data, another tool's filesystem), every entry held as the tarball has it: its whole mode (setuid, setgid and sticky bits included, as dpkg and apk install a package's; an entry the rule declares itself may set permission bits only), owner and link target, times at the epoch, as rules_oci's `tars`. A directory several tarballs hold alike is written once; any other path declared twice, a device, a name leaving the image root, or a relative symlink target climbing above it fails the build; an absolute target is kept, since it resolves against the image's own root. A hard link becomes a copy of its file |
| `modes` | image path to permission bits as an **octal string**, `"0755"` |
| `owners` | image path to `{"uid": int, "gid": int}`; everything else is `0:0` |
| `symlinks` | image path to link target, written as given |
| `empty_dirs` | image paths of empty directories |
| `whiteouts` | image paths to delete, with everything under them, from the layers below |
| `opaque_dirs` | directories whose contents from the layers below are hidden; what this layer puts there stays |
| `platform` | the platform the built files are for, `"linux/amd64"` or `"linux/arm64"`: see below |

### Platform

An image runs on its platform, not on the machine that built it.
`platform` builds each build label in `files` in that platform's
architecture subrepo (`///linux_amd64//app:app`), whatever the host. On a
host of that platform this is the same target, built once; on any other
host it is a build for that platform. Plain files and labels already in a
subrepo are left as they are.

Whether or not `platform` is set, each regular file's header is read. An
ELF or Mach-O file is recorded in `|meta`'s `binaries`, and `oci_image`
refuses any that isn't the image's platform, naming it. With `platform`,
the layer refuses it first. So a Mac's darwin/arm64 binary can't become a
linux/amd64 image.

Building for another platform needs a toolchain that can. For Go, that's
the standard library for the target platform, and, with CGO on, a C
toolchain that links for it. Where that's missing, the build fails; it
never falls back to the host's binary.

### What makes it deterministic

The same declarations over the same input bytes give the same layer on any
host, and the test suite pins golden digests to hold it there.

* Entries are sorted by path, and every parent directory is written before
  its children. Parents nobody declared are `0755`, owned `0:0`.
* Every timestamp is the Unix epoch. Owners are numeric; no user or group
  names are written, and the host's user database is never read.
* Regular files are `0644` and directories `0755` unless `modes` says
  otherwise. An input file with an executable bit **must** have its mode
  declared: remote executors leave build outputs `0555` and a local build
  leaves them `0755`, so the bit is never inherited.
* Compression is Go's `compress/gzip` at level 6, with no name or time in the
  header. The compressed bytes are therefore fixed by the Go toolchain that
  builds `oci-tool`: a Go upgrade can change every `digest`, while `diff_id`
  depends only on the declarations and the inputs.

### What it refuses

Each of these fails the build, naming the path: a relative path; the image
root; an empty, `.` or `..` component; a NUL; a path declared twice; a file or
symlink with something inside it; any bit above `0777` (setuid, setgid,
sticky); a negative owner; a `modes` or `owners` entry for a path the layer
does not declare; an executable input without a declared mode; a declared name
starting with `.wh.`, which every reader takes for a whiteout; and a whiteout
the same layer also writes into.

Whiteouts and opaque directories are the OCI image spec's empty marker files,
`.wh.<name>` beside the path and `.wh..wh..opq` inside the directory. The test
suite pins a golden layer of every unusual entry: both markers, absolute and
relative symlinks, a path too long for a plain tar header, and an empty file.

Modes are strings because Please's BUILD language reads `0o755` as the decimal
`755`, which is octal `1363`.

## `oci_pull`

```python
oci_pull(
    name = "postgres_17",
    image = "docker.io/library/postgres",
    digest = "sha256:d74eeac9a635390a49bc21bd49fccd973de707e2a53a76ac49b552b8712ec46f",
)
```

A public base image, pulled by digest as rules_oci's `oci.pull` pulls one.
The digest is all that is committed: of an image's manifest, or of an index,
from which the rule's platform (or the plugin's DefaultPlatform) is taken.

One `oci-tool pull` action fetches the document at the digest, then the
image's config and layers, through the registry's token handshake,
anonymously, and checks every byte against the digest that names it. It has
the network, as a `remote_file` does, so it is not sandboxed. No credential is
in it, so only a public image can be pulled: Docker Hub, ghcr.io, gcr.io,
quay.io. Its output is an OCI image layout in plz-out, which `oci_import`
reads, so a pulled base is checked as an imported one is.

| output | |
| --- | --- |
| `:base\|base` | the base's metadata: its config and its layers' descriptors |
| `:base_blobs` | every layer blob |

Taking a newer image is changing its digest in a reviewed PR. Pull each
third-party base once, in one package (say `//third_party/oci`), and have
images name it by label.

## `oci_import`

```python
oci_import(
    name = "base",
    layout = ":apko_layout",  # any rule whose output is an OCI image layout
)
```

A base from an OCI image layout another rule builds: apko's, or any tool's
that writes one. It runs as a build action like every other rule, reading
only the layout, so what reaches the layout is the producing rule's
business; apko built offline from a lock file is one way to keep that
hermetic. The layout must hold exactly one image for `platform` (unset, the
plugin's `DefaultPlatform`), in its index or one index below it; none, two,
a config of another platform, a layer that isn't a gzipped tar, or a blob
that isn't its digest fails the build.

| output | |
| --- | --- |
| `:base\|base` | the base's metadata: the image's config and its layers' descriptors |
| `:base_blobs` | its layers' blobs: a directory of files named by digest |

## `oci_image`

```python
oci_image(
    name = "image",
    base = ":base",
    layers = [":app_layer"],
    platform = "linux/amd64",
    entrypoint = ["/app"],
    repository = "registry.example.com/team/app",
)
```

Layers over a base, or over nothing. The image is identified by its manifest
digest, and nothing else stands in for it: no tag, version or label, so
anything that changes the image changes its identity.

| output | |
| --- | --- |
| `:image\|manifest`, `:image\|config` | the image's two documents |
| `:image\|meta` | its manifest, config and layer descriptors, as JSON |
| `:image\|digest` | the manifest's `sha256:` digest |
| `:image\|base` | the image as a base, which another `oci_image` builds on |
| `:image_fqn` | with `repository`: `<repository>@sha256:<digest>`, what a deploy substitutes into manifests |
| `:image_retain` | with `repository`: `<repository>:keep-sha256-<hex>`, the tag that keeps the image in the registry. Not the bare `sha256-<hex>`: the OCI distribution spec reserves that name for an image's referrers (signatures, SBOMs, attestations) on registries without the referrers API |
| `:image_push` | with `repository`: the publisher, run by `plz run` (below) |
| `:image_blobs` | every layer blob, base's and own |

`entrypoint` and `cmd` inherit when None and clear when `[]`; setting the
entrypoint alone clears an inherited cmd, as a Dockerfile's `ENTRYPOINT`
does. `env` sets values over the base's, key by key. `user`, `workdir` and
`stop_signal` (the signal a runtime stops the container with, `"SIGQUIT"`)
inherit when None. Every timestamp is the epoch and every document has one
encoding, so the same inputs give the same digest; the plugin's tests pin it.

`base` is an `oci_pull`, an `oci_import` or another `oci_image`. On an
image, the new one has the base image's layers, in order, then its own, and
its settings apply over the base image's config, as they would over a pulled
one. So a shared image
built in the repository is built, tested and published once, and the images
over it share its layers. The platforms must match: an image whose platform
is not its base's is refused.

## Multi-platform: `oci_image_index`

```python
oci_image_index(
    name = "image",
    images = [":image_amd64", ":image_arm64"],
    repository = "registry.example.com/team/app",
)
```

An OCI image index over one `oci_image` per platform, identified by the
index's digest. The children are sorted by platform, so their order here
doesn't change the index. Two images of one platform, an index as a child,
and fewer than two images are refused, and each child's layers are already
checked against its platform. `:image_fqn` and `:image_retain` work as an
image's do, so a manifest names the index and a runtime picks its platform.

`plz run :image_push` publishes every child into `repository` by digest,
each under its own retention tag so a registry's cleanup of untagged
manifests can't take it from under the index, then the index and its tag.
Every retention tag is looked at before anything is written, and one that
names another digest stops the whole publish.

The test suite builds a real two-platform image from regctl's own static
linux/amd64 and linux/arm64 release binaries, pinned by sha256, and pins
the index's digest.

## Publishing: `<image>_push`

`plz run :image_push` uploads the bytes the build made and nothing else: it
builds nothing, and runs with the caller's registry credentials (Docker's
credential helpers), never inside a build action. In order:

1. Each reference is looked up first. Only a 404 counts as absent; any other
   answer, an authorisation failure included, is an error.
2. If the retention tag already names another image, it stops before writing
   anything. Tags in the registry never move, so publishing cannot fix that,
   and a person has to look.
3. If the image is absent, its missing blobs are uploaded, then the manifest
   by digest.
4. If the retention tag is absent, it is written, naming the digest.
5. Both references are read back, and must resolve to the built digest.

So a digest that is present without its tag gets only the tag, and an image
already published and tagged makes no write at all. The script's files are
data as well as deps, so a remote build leaves them where it runs.

## Signing: `<image>_sign` and `<image>_verify`

With the plugin's `CosignTool`, `SyftTool` and `ProvenanceBuildType` set,
every `oci_image` and `oci_image_index` with a `repository` gets two more
runnable targets. Both run after the image is published and never change it:
a signature and an attestation are manifests of their own, pointing at the
image's digest.

```sh
plz run :image_sign -- --source-uri git+https://github.com/org/repo \
  --commit "$GITHUB_SHA" --builder-id https://github.com/org/repo/.github/workflows/cd.yaml \
  --invocation-id "$RUN_URL"
plz run :image_verify
```

`_sign` signs the published digest with cosign and attaches two
attestations: the image's SPDX SBOM, from syft over its OCI layout, and a
SLSA v1 provenance naming the build type, the target, the commit, and the
config and every layer by digest. For an index it does this for each child,
then signs the index. The source flags come from whatever deploys and are
all required; nothing is invented. Every predicate is written before
anything is signed, so a provenance that can't be written stops the run
before the registry changes. syft's check for a newer syft is turned off: a
deploy has no reason to call out for it.

`_verify` checks every signature and both attestations.

| setting | |
| --- | --- |
| `SigningKey` | a cosign key file or KMS URI (`gcpkms://…`) to sign with; unset, signing is keyless, with the environment's OIDC token |
| `VerifyPublicKey` | for key signing: what `_verify` checks against |
| `VerifyIdentity`, `VerifyIssuer` | for keyless signing: the certificate identity (a regular expression) and its OIDC issuer |

Configured settings reach oci-tool through a JSON spec file, never as shell
text. The test suite signs and verifies end to end with a throwaway key and
no network: an in-process registry, a signing config with no Sigstore
services, and an empty trusted root, so cosign never reaches for Sigstore's
TUF mirror. It also checks that another key and an unsigned image are
refused.

## Running it locally: `oci_load`

```python
oci_load(
    name = "image_minikube",
    image = ":image",
    loader = "minikube",
)
```

`plz run :image_minikube` loads the image into a local runtime: `docker`
(`docker load`, the default) or `minikube` (`minikube image load`, whatever
runtime the cluster uses). Any other loader is refused. The archive it loads is a build output, written by
the tool without a daemon, and the same image gives the same bytes.

A local runtime holds images by tag, not by digest alone, so the image loads
as its retention tag, `<repository>:keep-sha256-<hex>`, which names exactly one
image. `:image_minikube_fqn` (and `_retain`) is that tag, so a manifest that
runs `:image_minikube`, for instance through a kustomize image override, runs what
was loaded, with `imagePullPolicy: Never`. `:image_minikube_push` is the
image's own publisher, so a deploy that publishes every image it runs writes
the same tag to the registry.

## Layouts and archives: `oci_layout`, `oci_archive`

```python
oci_layout(name = "image_layout", image = ":image", ref_name = "app")
oci_archive(name = "image_oci", image = ":image", format = "oci", ref_name = "app")
oci_archive(name = "image_docker", image = ":image", format = "docker", tags = ["registry.example.com/team/app:v1"])
```

`oci_layout` is an OCI image layout directory: `oci-layout`, `index.json`
naming the image (by `ref_name`, when given), and every blob under
`blobs/sha256`. go-containerregistry writes it, and it is read back and
validated before it is an output: every descriptor's digest and size, and
every layer's diff ID.

`oci_archive` is the image as one file. `format = "oci"` is that layout as a
tar, its entries in path order with fixed times, owners and modes.
`format = "docker"` is what `docker load` reads, and loads as each of `tags`,
listed sorted, so the archive depends on which tags there are and not on
their order in the BUILD file. It is go-containerregistry's format, byte for
byte, but not its writer: that collects an image's tags from a map, so two
tags would come out in a different order from one build to the next.

Both are build outputs, the same bytes for the same image on any host, and
the test suite pins both. regctl, an OCI client that shares no code with
these rules, reads the layout and imports both archives.

## Testing an image: `oci_test`

```python
oci_test(
    name = "image_test",
    image = ":image",
    configs = ["image_test.yaml"],
    owners = {"/app": {"uid": 0, "gid": 0}},
    user = "",
)
```

Tests an image without running it and with no Docker daemon. `configs` are
[container-structure-test](https://github.com/GoogleContainerTools/container-structure-test)
files, run with its tar driver against the image as a Docker archive: file
existence, content, permissions and metadata. Two things it can't check are
done by `oci-tool` instead:

* **Owners.** The tar driver extracts files as whoever runs the test, so
  `owners` is checked against the image's flattened filesystem, with every
  layer applied and whiteouts honoured.
* **No user.** container-structure-test reads an empty expected user as
  "don't check", so `user` is matched exactly, `""` meaning none is set.

A binary's platform needs no test: `oci_image` refuses one that isn't its
own. container-structure-test is in maintenance mode, so it is reached only
through `oci_test`, set by the `StructureTestTool` setting and pinned by
sha256 wherever it's configured, and can be replaced behind it.

## Hermeticity

What these rules guarantee: deterministic assembly over the declared inputs,
with no network or credentials in the action. What they do not yet: that the
inputs themselves are reproducible. A Go binary placed in a layer is only as
reproducible as the toolchain that compiled it.

## Developing

```sh
./pleasew test //...
```

runs every test: the rules' own (`test/`) and oci_tool's unit tests. Here
`OciTool` is built from this checkout (`.plzconfig`), so the tests run the
code they ship with. CI runs them on Ubuntu and macOS for every pull request.

## Releasing

Two things are released, each when its version changes on `main`:

- **oci_tool**, tagged `oci-tool-v<version>`: `tools/oci_tool/VERSION` and
  `tools/oci_tool/ChangeLog`. `//package:oci_tool_release_files` cross-compiles
  it for every released platform. It is pure Go and linked without the
  toolchain's path, so every Linux host builds the same bytes, and releases are
  built on Linux. That is why `tools/release.build_defs` can pin the release
  in the same change that makes it, and why `//package:release_hashes_test`
  fails as soon as oci_tool changes without being re-pinned. A Mac builds
  different bytes for now, only in the Go version string go-rules embeds
  (`package/BUILD` has the detail), so re-pin on Linux. To change the tool: bump
  its version, run `./pleasew build //package:oci_tool_release_files`, and write
  the four new hashes (`shasum -a 256 plz-out/gen/package/*`) into
  `tools/release.build_defs`.
- **The plugin**, tagged `v<version>`: `VERSION` and `ChangeLog`.

`release.yaml` builds the tool on CI, refuses to publish a binary whose hash
isn't the pinned one, and publishes the tool before the plugin that pins it.

## Licence

Apache 2.0; see `LICENSE`.
