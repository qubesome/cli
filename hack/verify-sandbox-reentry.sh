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
#
# Check 3 stopping at ns/pid while check 4 stops at ns/mnt is the useful
# part. nsenter joins the user namespace first, so that join succeeded and
# carried its capabilities forward. The pid namespace is the wall, not the
# route to it, and re-entry was designed around it rather than through it.
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

printf '== cleaning up\n'
pkill -f "$MARK" 2>/dev/null
printf '   done\n'
