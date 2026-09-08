// Package control holds the wire contract for the channel qubesome uses to
// tell the session gateway which address belongs to which workload, and to
// wait for the gateway to come up.
//
// The contract lives here rather than in the gateway because qubesome is the
// public repository of the two. A private repository can depend on a public
// one, and the reverse leaves everyone outside the organisation unable to
// resolve the module. The gateway imports this package and serves it.
package control

// ServerName is the name the gateway's control certificate is issued for.
// The channel runs over a unix socket, so there is no hostname to derive it
// from and both ends have to agree on a constant.
const ServerName = "qubesome-gateway"
