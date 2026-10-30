subinclude("//build_defs:oci")

# A tarball laid out by another tool, as a package's data is: a group-writable
# executable, a file only its owner's group may read, and a symlink. Written
# by Please's arcat, the same on any host (GNU tar's --sort and --owner flags
# are not in macOS's tar): it owns every entry 65534:65534, dates it
# 2000-01-01, and writes no directory entries. It also adds user and group
# write to every mode, so the modes here already have them and the tarball
# holds exactly what this rule sets.
#
# A directory a tarball owns by another account (a package's 999:999
# /var/lib/pgbackrest, 0750) needs a directory entry arcat can't write. The
# oci-tool unit tests hold it: tools/oci_tool/tar_test.go reads that directory's mode
# and owner out of a tarball (TestTarEntries_KeepsEachEntrysModeOwnerAndLink),
# and TestMergeDirs_WritesADirectoryTwoArchivesHoldAlikeOnce writes a
# directory two tarballs hold alike once.
genrule(
    name = "package_tar",
    outs = ["package.tar"],
    cmd = " && ".join([
        "mkdir -p root/usr/bin root/etc",
        "printf '#!/bin/sh\\n' > root/usr/bin/tool && chmod 0775 root/usr/bin/tool",
        "printf '[global]\\n' > root/etc/tool.conf && chmod 0660 root/etc/tool.conf",
        "ln -s tool root/usr/bin/tool-alias",
        "$TOOLS tar -o $OUT --srcs root --strip-prefix root",
    ]),
    test_only = True,
    tools = [CONFIG.ARCAT_TOOL],
)

# The tarball's entries as they are, beside a file the layer declares; the
# /etc both need is written once.
oci_layer(
    name = "layer",
    files = {"/etc/motd": "motd"},
    tars = [":package_tar"],
    test_only = True,
)

oci_image(
    name = "image",
    layers = [":layer"],
    platform = "linux/amd64",
    test_only = True,
)

# The layer holds each tarball entry with its own mode and owner, and the
# declared file and the /etc the layer writes for both with the layer's own.
# Every time in a layer is the epoch whatever its source says;
# tools/oci_tool/layer/layer_test.go holds that.
oci_test(
    name = "tars_structure_test",
    configs = ["tars_test.yaml"],
    image = ":image",
    owners = {
        "/etc": {
            "uid": 0,
            "gid": 0,
        },
        "/etc/motd": {
            "uid": 0,
            "gid": 0,
        },
        "/etc/tool.conf": {
            "uid": 65534,
            "gid": 65534,
        },
        "/usr/bin/tool": {
            "uid": 65534,
            "gid": 65534,
        },
        "/usr/bin/tool-alias": {
            "uid": 65534,
            "gid": 65534,
        },
    },
)

# The symlink keeps its target, and /etc, which the tarball's file and the
# declared one both need, is in the layer once. Only what macOS's tar and GNU
# tar do alike: names, not the long listing, and readlink without flags.
gentest(
    name = "tars_test",
    data = {"blob": [":layer|blob"]},
    no_test_output = True,
    test_cmd = " && ".join([
        "tar -tzf $DATA_BLOB > listing",
        "test \"$(grep -c '^etc/$' listing)\" = 1",
        "mkdir root && tar -xzf $DATA_BLOB -C root",
        # A symlink, to its target as the tarball wrote it. Its own mode isn't
        # asserted: macOS gives a symlink 0755 and Linux 0777, and no runtime
        # reads it.
        "test \"$(readlink root/usr/bin/tool-alias)\" = tool",
    ]),
)
