#!/bin/sh
# Verify what a microVM root filesystem build needs from the host.
#
# The firecracker runner is being rebuilt to boot any OCI image, with the
# ext4 written straight out of the bundle internal/images already unpacks,
# by an unprivileged mkfs.ext4 running inside a user namespace over an
# overlay that composes the workload's read-only paths into the tree. None
# of that could be measured in the development container: it has bubblewrap
# but no e2fsprogs, no umoci, no firecracker, and it is a container, so no
# VM boots in it.
#
# Run this on the qubesome host, as your normal user. It builds a throwaway
# fixture, tries each mechanism against it, then cleans up. Checks whose
# tool is missing skip rather than fail, so a partial host still answers
# what it can.
#
# Result on the target host, 2026-09-08, running as an ordinary user:
#
#   1. mkfs.ext4 -d reads through a bind mount   PASS
#   2. ownership, setuid, symlink, hardlink      PASS
#      xattrs                                    PASS
#   3. build time and sparse size                PASS, see below
#   4. umoci bundle uniform in uid               PASS, every file uid 1000
#   5. guest kernel has vsock and devtmpfs       SKIP, no kernel downloaded
#   6. host and guest speak over vsock           SKIP, no kernel downloaded
#
# Check 3 in full: a 926M bundle, mkfs.ext4 at 4096M, 2.5 s elapsed, 4.0G
# apparent and 943M on disk. That is what makes rebuilding the image on
# every boot reasonable rather than something to cache: the build costs a
# couple of seconds and the sparse file costs its contents.
#
# Checks 5 and 6 skipped only because no kernel has been downloaded on
# that host yet. They are guest questions and no host run can answer them.
#
# Check 1 is the one the design rests on. mkfs.ext4 -d walks a directory
# tree that is an overlay with bind mounts inside it, so the walk crosses
# filesystem boundaries. A mkfs that filters by st_dev would leave every
# bound path silently out of the image, and the rootfs would have to be
# built from a copy of the bundle instead, which changes the shape of the
# whole runner.
#
# Check 4 is the one the ownership design rests on. Every file in the guest
# is root-owned because the bundle is uniform in uid to start with and the
# single uid mapped into the namespace cannot make it otherwise. Any output
# at all from that check falsifies it.
#
# Checks 5 and 6 need a booted VM, which does not exist until the runner
# does. They are a second half that prints what to run by hand.
set -u

MKFS=""
for c in /usr/sbin/mkfs.ext4 /sbin/mkfs.ext4; do
    if [ -x "$c" ]; then
        MKFS=$c
        break
    fi
done
if [ -z "$MKFS" ]; then
    MKFS=$(command -v mkfs.ext4 2>/dev/null) || MKFS=""
fi

DEBUGFS=""
for c in /usr/sbin/debugfs /sbin/debugfs; do
    if [ -x "$c" ]; then
        DEBUGFS=$c
        break
    fi
done
if [ -z "$DEBUGFS" ]; then
    DEBUGFS=$(command -v debugfs 2>/dev/null) || DEBUGFS=""
fi

WORK=$(mktemp -d /tmp/qubesome-microvm-probe.XXXXXX) || exit 1
TREE="$WORK/tree"
IMG="$WORK/fixture.ext4"
KERNEL="$HOME/.qubesome/vmlinux"

# BUILT records whether check 1 got an image out of mkfs, which is what
# check 2 needs. Check 2 still runs when the bound file was missing, since
# everything else it asks about came out of the tree itself.
BUILT=no

res() {
    if [ "$1" -eq 0 ]; then
        printf '   PASS  %s\n\n' "$2"
    else
        printf '   FAIL  %s\n\n' "$2"
    fi
}

skip() {
    printf '   SKIP  %s\n\n' "$1"
}

# version prints the first line a tool answers --version with, or says it
# is absent. Asking a missing tool directly would leak the shell's own
# "command not found" past the redirection.
version() {
    if command -v "$1" >/dev/null 2>&1; then
        "$@" 2>&1 | head -n 1
    else
        printf 'not installed'
    fi
}

# stat_field reads one field out of a debugfs stat of path in the image.
# debugfs writes its own chatter to stderr, so only stdout is matched.
stat_field() {
    "$DEBUGFS" -R "stat $1" "$IMG" 2>/dev/null | grep -o "$2"
}

printf '\n== environment\n'
if [ -n "$MKFS" ]; then
    printf '   mkfs.ext4:   %s (%s)\n' "$("$MKFS" -V 2>&1 | head -n 1)" "$MKFS"
else
    printf '   mkfs.ext4:   not installed\n'
fi
printf '   bwrap:       %s\n' "$(version bwrap --version)"
printf '   umoci:       %s\n' "$(version umoci --version)"
printf '   firecracker: %s\n' "$(version firecracker --version)"
printf '   %s\n' "$(grep CapEff /proc/self/status)"

printf '\n== building the fixture tree\n'
mkdir -p "$TREE/bin" "$TREE/etc" "$WORK/host"
printf 'root:x:0:0:root:/root:/bin/sh\n' >"$TREE/etc/passwd"
printf '#!/bin/sh\n' >"$TREE/bin/su"
chmod 4755 "$TREE/bin/su"
ln -s passwd "$TREE/etc/link"
printf 'linked\n' >"$TREE/bin/hard1"
ln "$TREE/bin/hard1" "$TREE/bin/hard2"
printf '[user]\n\tname = probe\n' >"$WORK/host/gitconfig"

# The tree deliberately has no /root, so the bind below lands on a
# directory only the overlay has. That is what a workload's read-only
# path into a home directory the image does not carry looks like.
XATTR=no
if command -v setfattr >/dev/null 2>&1 &&
    setfattr -n user.qubesome -v probe "$TREE/etc/passwd" 2>/dev/null; then
    XATTR=yes
fi
printf '   tree at %s\n' "$TREE"
printf '   setuid /bin/su, symlink /etc/link, hardlinked /bin/hard1 and hard2\n'
printf '   user.qubesome xattr on /etc/passwd: %s\n' "$XATTR"

printf '\n== 1. does mkfs.ext4 -d read through a bind mount in a composed view\n'
printf '   The tree is an overlay and /root/.gitconfig comes from a bind\n'
printf '   into it. If mkfs filters by st_dev the file is silently absent.\n'
if [ -z "$MKFS" ]; then
    skip "e2fsprogs is not installed"
elif ! command -v bwrap >/dev/null 2>&1; then
    skip "bwrap is not installed"
else
    bwrap --unshare-user --uid 0 --gid 0 --bind / / \
        --overlay-src "$TREE" --tmp-overlay /mnt \
        --ro-bind "$WORK/host/gitconfig" /mnt/root/.gitconfig \
        "$MKFS" -q -F -m 0 -O ^has_journal \
        -E lazy_itable_init=1,lazy_journal_init=1 \
        -d /mnt "$IMG" 64M 2>&1
    if [ $? -ne 0 ]; then
        res 1 "mkfs.ext4 over the composed tree"
    else
        BUILT=yes
        if [ -z "$DEBUGFS" ]; then
            skip "debugfs is not installed, cannot read the image back"
        else
            stat_field /root/.gitconfig 'Inode:' >/dev/null
            rc=$?
            res $rc "the bound file is in the image"
            if [ "$rc" -ne 0 ]; then
                printf '   ***  mkfs.ext4 -d did not cross into the bind mount.\n'
                printf '   ***  Composing read-only paths into an overlay is dead.\n'
                printf '   ***  The rootfs has to be built from a copy of the\n'
                printf '   ***  bundle instead, and the runner changes shape.\n\n'
            fi
        fi
    fi
fi

printf '== 2. what survives into the image\n'
if [ "$BUILT" != "yes" ] || [ -z "$DEBUGFS" ]; then
    skip "no image was built, or debugfs is not installed"
else
    "$DEBUGFS" -R 'stat /bin/su' "$IMG" 2>/dev/null | head -n 4

    [ "$(stat_field /bin/su 'User: *[0-9][0-9]*' | tr -dc '0-9')" = "0" ]
    res $? "/bin/su is owned by uid 0"

    # debugfs prints the mode in octal, with or without a leading zero
    # depending on its version, so it is compared as a number and not as
    # the string it was printed as.
    MODE=$(stat_field /bin/su 'Mode: *[0-7][0-7]*' | tr -dc '0-7')
    [ "$((0$MODE))" -eq "$((04755))" ]
    res $? "/bin/su keeps mode 4755"

    stat_field /etc/link 'Type: symlink' >/dev/null
    res $? "/etc/link is still a symlink"

    [ "$(stat_field /bin/hard1 'Links: *[0-9][0-9]*' | tr -dc '0-9')" = "2" ]
    res $? "/bin/hard1 and hard2 are still one inode"

    if [ "$XATTR" != "yes" ]; then
        skip "setfattr is not installed, xattrs untested"
    else
        stat_field /etc/passwd 'user\.qubesome' >/dev/null
        res $? "the user.qubesome xattr survived"
    fi
fi

printf '== 3. what it costs on a real bundle\n'
BUNDLE=$(find "$HOME/.qubesome/images/unpacked" -mindepth 2 -maxdepth 2 \
    -type d -name rootfs 2>/dev/null | head -n 1)
BIG="$WORK/bundle.ext4"
if [ -z "$MKFS" ]; then
    skip "e2fsprogs is not installed"
elif ! command -v bwrap >/dev/null 2>&1; then
    skip "bwrap is not installed"
elif [ -z "$BUNDLE" ]; then
    skip "no unpacked bundle under ~/.qubesome/images, run qubesome images first"
else
    printf '   bundle %s\n' "$BUNDLE"
    printf '   %s\n' "$(du -sh "$BUNDLE" 2>/dev/null)"
    start=$(date +%s%N)
    bwrap --unshare-user --uid 0 --gid 0 --bind / / \
        --overlay-src "$BUNDLE" --tmp-overlay /mnt \
        "$MKFS" -q -F -m 0 -O ^has_journal \
        -E lazy_itable_init=1,lazy_journal_init=1 \
        -d /mnt "$BIG" 4096M 2>&1
    rc=$?
    end=$(date +%s%N)
    if [ "$rc" -eq 0 ]; then
        printf '   elapsed:  %s s\n' \
            "$(awk -v a="$start" -v b="$end" 'BEGIN { printf "%.1f", (b - a) / 1000000000 }')"
        printf '   apparent: %s\n' "$(du -h --apparent-size "$BIG" | cut -f1)"
        printf '   on disk:  %s\n' "$(du -h "$BIG" | cut -f1)"
    fi
    res $rc "mkfs.ext4 of a real bundle at 4096M"
fi

printf '== 4. is the bundle uniform in ownership\n'
printf '   umoci unpack --rootless cannot chown, so every file should be\n'
printf '   the invoking uid. Any line below falsifies the ownership design.\n'
if [ -z "$BUNDLE" ]; then
    skip "no unpacked bundle under ~/.qubesome/images"
else
    STRAY=$(find "$BUNDLE" ! -uid "$(id -u)" -printf '%u %p\n' 2>/dev/null | head)
    if [ -n "$STRAY" ]; then
        printf '%s\n' "$STRAY"
    fi
    [ -z "$STRAY" ]
    res $? "every file in the bundle is uid $(id -u)"
fi

printf '== 5 and 6. the guest, by hand\n'
printf '   These need a booted VM, which does not exist until the runner\n'
printf '   builds one. Nothing here can answer them.\n'
if [ ! -f "$KERNEL" ]; then
    skip "no kernel at $KERNEL, run a firecracker workload once to fetch it"
else
    printf '   kernel: %s\n' "$(du -h "$KERNEL" | cut -f1) $KERNEL"
    printf '\n'
    printf '   5. Does the pinned kernel carry virtio-vsock and devtmpfs?\n'
    printf '      The vmlinux has no embedded config to grep, so ask from\n'
    printf '      inside the guest:\n'
    printf '\n'
    printf '          ls -l /dev/vsock\n'
    printf '          mount -t devtmpfs devtmpfs /dev\n'
    printf '\n'
    printf '      No /dev/vsock means no console transport. A devtmpfs that\n'
    printf '      will not mount means the init has to populate /dev itself.\n'
    printf '\n'
    printf '   6. Does the guest reach the host over vsock, and does the\n'
    printf '      host CONNECT preamble work? In the guest:\n'
    printf '\n'
    printf '          socat VSOCK-LISTEN:1234,fork EXEC:/bin/cat\n'
    printf '\n'
    printf '      and on the host, against the uds firecracker was given\n'
    printf '      as vsock.uds_path:\n'
    printf '\n'
    printf '          socat - UNIX-CONNECT:/path/to/vsock.uds\n'
    printf '          CONNECT 1234\n'
    printf '\n'
    printf '      A guest port is reached by writing that preamble first and\n'
    printf '      reading back OK <port>. Anything else and the console has\n'
    printf '      to be built on something other than vsock.\n\n'
fi

printf '== cleaning up\n'
rm -rf "$WORK"
printf '   removed %s\n' "$WORK"
printf '   done\n'
