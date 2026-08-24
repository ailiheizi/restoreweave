//go:build linux

package processor

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func onnxWorkerPeerIdentityPlatform(conn net.Conn) (onnxWorkerPeer, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return onnxWorkerPeer{}, fmt.Errorf("%w: connection is %T", errONNXWorkerPeerUnavailable, conn)
	}
	var out onnxWorkerPeer
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return onnxWorkerPeer{}, fmt.Errorf("%w: obtain raw Unix connection", errONNXWorkerPeerUnavailable)
	}
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			out = onnxWorkerPeer{}
			return
		}
		out = onnxWorkerPeer{PID: int(cred.Pid), UID: int(cred.Uid), GID: int(cred.Gid)}
	})
	if err != nil || out.PID <= 0 || out.UID < 0 {
		return onnxWorkerPeer{}, fmt.Errorf("%w: query SO_PEERCRED", errONNXWorkerPeerUnavailable)
	}
	return out, nil
}
