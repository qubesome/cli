TAP_DEV="tap0"
TAP_IP="172.16.0.1"
MASK_SHORT="/30"

# Setup network interface (as root)
echo ip link del "$TAP_DEV" 2> /dev/null || true
echo ip tuntap add dev "$TAP_DEV" mode tap
echo ip addr add "${TAP_IP}${MASK_SHORT}" dev "$TAP_DEV"
echo ip link set dev "$TAP_DEV" up

# Enable ip forwarding (as root)
sudo sh -c "echo 1 > /proc/sys/net/ipv4/ip_forward"

# Set up microVM internet access (as root)
iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE || true
iptables -D FORWARD -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT \
    || true
iptables -D FORWARD -i tap0 -o eth0 -j ACCEPT || true
iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
iptables -I FORWARD 1 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -I FORWARD 1 -i tap0 -o eth0 -j ACCEPT
