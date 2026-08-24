package processor

import (
	"errors"
	"net"
)

type onnxWorkerPeer struct {
	PID int
	UID int
	GID int
}

var errONNXWorkerPeerUnavailable = errors.New("Unix peer credentials are unavailable")

// onnxWorkerPeerIdentityOf is implemented only on platforms with a kernel
// credential query. A worker's self-reported PID is never trusted.
func onnxWorkerPeerIdentityOf(conn net.Conn) (onnxWorkerPeer, error) {
	return onnxWorkerPeerIdentityPlatform(conn)
}
