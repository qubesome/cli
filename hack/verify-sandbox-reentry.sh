#!/bin/sh
# Verify how an unprivileged process can re-enter a running bwrap sandbox.
#
# qubesome needs this for singleInstance workloads, which "docker exec"
# used to handle. Run this on the qubesome host, as your normal user.
# It starts a throwaway sandbox, tries each mechanism against it, then
# cleans up.
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
printf '   This is what a per-workload link to the gateway needs. It wants\n'
printf '   CAP_NET_ADMIN in both namespaces, which only holds if they share\n'
printf '   a user namespace. Answering it properly needs the real sandbox\n'
printf '   layout, so this only reports whether the pieces are present.\n'
if command -v ip >/dev/null 2>&1; then
    unshare --user --map-root-user --net \
        sh -c 'ip link add v0 type veth peer name v1 && ip link set v1 netns 1 2>&1 | head -1; true' 2>&1 |
        sed 's/^/   /'
    printf '   (moving to netns 1 is expected to fail, it is the host namespace)\n\n'
else
    printf '   SKIP  ip is not installed\n\n'
fi

printf '== cleaning up\n'
pkill -f "$MARK" 2>/dev/null
printf '   done\n'
