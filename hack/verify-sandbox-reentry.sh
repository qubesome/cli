#!/bin/sh
# Verify how an unprivileged process can re-enter a running bwrap sandbox.
#
# qubesome needs this for singleInstance workloads, which "docker exec"
# used to handle. Run this on the qubesome host, as your normal user.
# It starts a throwaway sandbox, tries each mechanism against it, then
# cleans up.
#
# Result on the target host, 2026-09-08, bubblewrap 0.11.2 and util-linux
# 2.42.2, running as an ordinary user with CapEff 0000000000000000:
#
#   1. bwrap --userns FD                PASS
#   2. bwrap --userns FD --pidns FD     FAIL, Operation not permitted
#   3. nsenter --user --mount --pid     FAIL, stops at ns/pid
#   4. nsenter --mount alone            FAIL, as expected
#   5. veth in an owned netns           PASS
#   6. veth across two netns, one userns PASS
#   7. veth into a descendant's netns    PASS
#   8. nsenter --net into a descendant    PASS
#
# Checks 9, 10 and 11 are the microVM's. Result on the target host,
# 2026-09-12, bubblewrap 0.12.0 and the same util-linux:
#
#   9. tap created in a descendant netns PASS
#  10. that tap opened with CapEff 0     PASS
#  11. nft bridge table in a descendant  SKIP, no nft on the host
#
# Check 10 is the one the microVM design rests on, and it is the reason
# the sandbox a VMM runs in is given no capabilities at all. A guest is
# root on a kernel of its own and can renumber its interface, so what
# pins its address is an nft table in the namespace its VMM runs in, and
# that only holds if the VMM itself cannot remove it. It cannot, because
# it needs no capability to open a tap that was made for its uid, which
# is what this measured.
#
# Check 11 skips because nft is not a host tool here. qubesome runs it out
# of the gateway image, so the answer comes from a running session, and
# a guard that will not load fails the launch rather than booting a
# machine nothing is policing.
#
# Check 3 stopping at ns/pid while check 4 stops at ns/mnt is the useful
# part. nsenter joins the user namespace first, so that join succeeded and
# carried its capabilities forward. The pid namespace is the wall, not the
# route to it, and re-entry was designed around it rather than through it.
#
# Checks 7 and 8 are the pair the gateway is built on. A capability held in an
# ancestor user namespace does carry into the namespaces its descendants
# own, so a session namespace can wire a veth into a sandbox that nested
# its own inside it, and can enter that namespace to address it. That is
# what lets each sandbox keep --disable-userns and its own uid mapping
# instead of sharing one flat namespace. Check 4 fails the same class of
# call from outside the session, which is the contrast that makes the
# session namespace the thing doing the work.
#
# Check 6 was run separately, after the first version of it was rewritten:
# the original passed a pid where iproute2 wanted a namespace and so tested
# nothing. It is what the gateway's per-workload link was waiting on, and
# it is the reason a user namespace shared between namespaces is the
# mechanism that stage builds on.
set -u

MARK=qubesome-reentry-probe

res() {
    if [ "$1" -eq 0 ]; then
        printf '   PASS  %s\n\n' "$2"
    else
        printf '   FAIL  %s\n\n' "$2"
    fi
}

printf '\n== environment\n'
printf '   bwrap:   %s\n' "$(bwrap --version 2>&1)"
printf '   nsenter: %s\n' "$(nsenter --version 2>&1)"
printf '   %s\n' "$(grep CapEff /proc/self/status)"

printf '\n== starting a holder sandbox\n'
setsid bwrap --unshare-user --unshare-pid --bind / / --dev /dev --tmpfs /tmp \
    sh -c "exec -a $MARK sleep 120" >/dev/null 2>&1 &
sleep 1

INIT=$(readlink /proc/self/ns/user)
HOLDER=""
for p in $(pgrep -f "$MARK"); do
    [ -r "/proc/$p/ns/user" ] || continue
    u=$(readlink "/proc/$p/ns/user" 2>/dev/null) || continue
    if [ "$u" != "$INIT" ]; then
        HOLDER=$p
        break
    fi
done

if [ -z "$HOLDER" ]; then
    printf '   could not find the holder, aborting\n'
    pkill -f "$MARK" 2>/dev/null
    exit 1
fi

printf '   holder pid %s\n' "$HOLDER"
printf '   userns %s\n' "$(readlink "/proc/$HOLDER/ns/user")"
printf '   pidns  %s\n' "$(readlink "/proc/$HOLDER/ns/pid")"
printf '   mntns  %s\n' "$(readlink "/proc/$HOLDER/ns/mnt")"

printf '\n== 1. bwrap --userns FD, join the sandbox user namespace\n'
sh -c "exec 9</proc/$HOLDER/ns/user
       bwrap --userns 9 --bind / / --dev /dev --tmpfs /tmp \
         /bin/sh -c 'echo \"   userns \$(readlink /proc/self/ns/user)\"'" 2>&1
res $? "bwrap --userns"

printf '== 2. bwrap --userns FD --pidns FD, also join the pid namespace\n'
sh -c "exec 9</proc/$HOLDER/ns/user
       exec 8</proc/$HOLDER/ns/pid
       bwrap --userns 9 --pidns 8 --unshare-pid --proc /proc \
         --bind / / --dev /dev --tmpfs /tmp \
         /bin/sh -c 'echo \"   pid \$\$ in \$(readlink /proc/self/ns/pid)\"'" 2>&1
res $? "bwrap --userns --pidns"

printf '== 3. nsenter --user --mount --pid, the setns route\n'
nsenter --user="/proc/$HOLDER/ns/user" \
        --mount="/proc/$HOLDER/ns/mnt" \
        --pid="/proc/$HOLDER/ns/pid" \
        --preserve-credentials -F /bin/sh -c 'echo "   pid $$ uid $(id -u)"' 2>&1
res $? "nsenter user+mount+pid"

printf '== 4. nsenter --mount alone, expected to fail, shown for contrast\n'
nsenter --mount="/proc/$HOLDER/ns/mnt" true 2>&1
res $? "nsenter mount alone"

printf '== 5. veth inside a user namespace we own\n'
if command -v ip >/dev/null 2>&1; then
    unshare --user --map-root-user --net \
        sh -c 'ip link add v0 type veth peer name v1 && ip link show v0 >/dev/null && echo "   veth pair created"' 2>&1
    res $? "unprivileged veth in an owned netns"
else
    printf '   SKIP  ip is not installed\n\n'
fi

printf '== 6. moving a veth end into a sibling namespace\n'
printf '   What a per-workload link to the gateway needs. Both namespaces\n'
printf '   are created inside one user namespace, which is the arrangement\n'
printf '   that is supposed to make it permitted.\n'
if command -v ip >/dev/null 2>&1; then
    unshare --user --map-root-user --net sh -c '
        unshare --net sleep 5 &
        peer=$!
        sleep 1
        ip link add v0 type veth peer name v1 || exit 1
        if ip link set v1 netns "$peer" 2>&1; then
            echo "   moved v1 into the sibling namespace"
            rc=0
        else
            rc=1
        fi
        kill "$peer" 2>/dev/null
        exit $rc
    ' 2>&1
    res $? "veth across two netns in one userns"
else
    printf '   SKIP  ip is not installed\n\n'
fi

printf '== 7. a veth into a namespace owned by a DESCENDANT user namespace\n'
printf '   Check 6 put both network namespaces in one user namespace. The\n'
printf '   gateway design cannot: joining one user namespace would forbid\n'
printf '   --disable-userns and pin every sandbox to a single uid, so each\n'
printf '   sandbox nests its own inside a session one instead.\n'
printf '   So the question is whether CAP_NET_ADMIN in the session reaches a\n'
printf '   network namespace owned by a child of it. The documented rule says\n'
printf '   an ancestor carries, and this is what asks the kernel.\n'
if command -v ip >/dev/null 2>&1; then
    unshare --user --map-root-user --net sh -c '
        # This shell is root in the session user namespace S, in a network
        # namespace owned by S. The child makes its own user namespace C,
        # a child of S, and a network namespace owned by C.
        unshare --user --map-root-user --net sleep 5 &
        child=$!
        sleep 1

        ip link add v0 type veth peer name v1 || exit 1
        if ip link set v1 netns "$child" 2>&1; then
            echo "   moved v1 into the descendant namespace"
            rc=0
        else
            rc=1
        fi

        kill "$child" 2>/dev/null
        exit $rc
    ' 2>&1
    res $? "veth into a descendant userns netns"
else
    printf '   SKIP  ip is not installed\n\n'
fi

printf '== 8. entering a descendant network namespace\n'
printf '   Moving a link in is not enough. RTM_NEWADDR carries no target\n'
printf '   namespace, so giving the veth an address means being in the\n'
printf '   namespace. setns of a network namespace wants CAP_SYS_ADMIN in\n'
printf '   the caller own user namespace as well as in the owning one, and\n'
printf '   inside the session both are satisfied. Check 4 shows the same\n'
printf '   call failing from outside, so this is the difference the session\n'
printf '   namespace makes.\n'
if command -v ip >/dev/null 2>&1; then
    unshare --user --map-root-user --net sh -c '
        unshare --user --map-root-user --net sleep 5 &
        child=$!
        sleep 1

        if nsenter --net=/proc/"$child"/ns/net ip link show lo >/dev/null 2>&1; then
            echo "   entered and listed the descendant namespace"
            rc=0
        else
            nsenter --net=/proc/"$child"/ns/net ip link show lo 2>&1 | sed "s/^/   /"
            rc=1
        fi

        kill "$child" 2>/dev/null
        exit $rc
    ' 2>&1
    res $? "nsenter --net into a descendant userns netns"
else
    printf '   SKIP  ip is not installed\n\n'
fi

printf '== 9. creating a tap inside a DESCENDANT network namespace\n'
printf '   Firecracker opens a tap, and it opens it in whatever network\n'
printf '   namespace it stands in. The sandbox it runs in holds no\n'
printf '   CAP_NET_ADMIN, on purpose: a process that escaped the machine\n'
printf '   could otherwise dissolve the bridge and unload the guard that\n'
printf '   pins the guest address. So the tap has to be made from outside\n'
printf '   and handed over by uid, which is what this asks.\n'
if ! command -v ip >/dev/null 2>&1; then
    printf '   SKIP  ip is not installed\n\n'
elif [ ! -e /dev/net/tun ]; then
    printf '   SKIP  /dev/net/tun is not present\n\n'
else
    unshare --user --map-root-user --net sh -c '
        unshare --user --map-root-user --net sleep 5 &
        child=$!
        sleep 1

        if nsenter --net=/proc/"$child"/ns/net \
               ip tuntap add qtap0 mode tap user 0 group 0 2>&1; then
            echo "   created qtap0 in the descendant namespace"
            rc=0
        else
            rc=1
        fi

        kill "$child" 2>/dev/null
        exit $rc
    ' 2>&1
    res $? "ip tuntap add in a descendant userns netns"
fi

printf '== 10. opening that tap from inside, holding NO capability\n'
printf '   This is the one the design rests on. If a process with an empty\n'
printf '   capability set cannot attach to a tap that was created for its\n'
printf '   uid, the sandbox has to be given CAP_NET_ADMIN over its own\n'
printf '   network namespace, and the guard becomes decoration: whatever\n'
printf '   escaped the machine could remove it. Do not work around a\n'
printf '   failure here. Go back to the design.\n'
if ! command -v ip >/dev/null 2>&1; then
    printf '   SKIP  ip is not installed\n\n'
elif [ ! -e /dev/net/tun ]; then
    printf '   SKIP  /dev/net/tun is not present\n\n'
elif ! command -v python3 >/dev/null 2>&1; then
    printf '   SKIP  python3 is not installed, and the probe needs an ioctl\n\n'
else
    PROBE=$(mktemp)
    cat >"$PROBE" <<'PROBE_EOF'
import fcntl, struct, sys

# TUNSETIFF is what firecracker calls. Attaching to a tap that already
# exists and is owned by this uid is supposed to need no capability at
# all. Creating one does.
TUNSETIFF = 0x400454ca
IFF_TAP, IFF_NO_PI = 0x0002, 0x1000

try:
    fd = open("/dev/net/tun", "r+b", buffering=0)
except OSError as e:
    print("   cannot open /dev/net/tun:", e)
    sys.exit(1)

try:
    fcntl.ioctl(fd, TUNSETIFF, struct.pack("16sH", b"qtap0", IFF_TAP | IFF_NO_PI))
except OSError as e:
    print("   TUNSETIFF failed:", e)
    sys.exit(1)

print("   attached to qtap0 with an empty capability set")
PROBE_EOF
    unshare --user --map-root-user --net sh -c '
        probe=$1
        ready=$(mktemp -d)

        # The child holds the namespace the tap goes in. It waits for the
        # tap to exist before trying to open it, because firecracker is
        # started after the wiring for exactly the same reason.
        unshare --user --map-root-user --net sh -c "
            while [ ! -f $ready/go ]; do sleep 0.1; done
            # --tmpfs /tmp comes before the probe is bound, not after.
            # bwrap applies these in order and a mount hides whatever was
            # put under it earlier, which is what made the first version of
            # this check report a failure that was its own.
            bwrap --unshare-user --uid 0 --gid 0 --cap-drop ALL \
                  --bind / / --dev-bind /dev/net/tun /dev/net/tun \
                  --tmpfs /tmp --ro-bind $probe $probe \
                  -- python3 $probe
            echo \$? > $ready/rc
        " &
        child=$!
        sleep 1

        nsenter --net=/proc/"$child"/ns/net \
            ip tuntap add qtap0 mode tap user 0 group 0 2>&1 || {
            kill "$child" 2>/dev/null
            rm -rf "$ready"
            exit 1
        }

        touch "$ready/go"

        i=0
        while [ ! -f "$ready/rc" ] && [ "$i" -lt 50 ]; do
            sleep 0.1
            i=$((i + 1))
        done

        rc=$(cat "$ready/rc" 2>/dev/null || echo 1)
        kill "$child" 2>/dev/null
        rm -rf "$ready"
        exit "$rc"
    ' sh "$PROBE" 2>&1
    res $? "TUNSETIFF on a pre-made tap with CapEff 0"
    rm -f "$PROBE"
fi

printf '== 11. loading an nft bridge table into a descendant netns\n'
printf '   The guard is an nft table in the bridge family, loaded into the\n'
printf '   machine own network namespace from outside it. nft is run out of\n'
printf '   the gateway image rather than from the host, so a host without it\n'
printf '   skips this and the answer has to come from a running session.\n'
if ! command -v nft >/dev/null 2>&1; then
    printf '   SKIP  nft is not installed on the host\n\n'
else
    unshare --user --map-root-user --net sh -c '
        unshare --user --map-root-user --net sleep 5 &
        child=$!
        sleep 1

        if nsenter --net=/proc/"$child"/ns/net nft -f - <<NFT_EOF 2>&1
table bridge qubesome {
  chain ingress {
    type filter hook prerouting priority filter; policy drop;
    iifname != "tap0" accept
  }
}
NFT_EOF
        then
            echo "   loaded a bridge table in the descendant namespace"
            rc=0
        else
            rc=1
        fi

        kill "$child" 2>/dev/null
        exit $rc
    ' 2>&1
    res $? "nft -f a bridge table in a descendant userns netns"
fi

printf '== cleaning up\n'
pkill -f "$MARK" 2>/dev/null
printf '   done\n'
