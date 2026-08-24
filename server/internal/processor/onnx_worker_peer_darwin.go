//go:build darwin

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
		pid, pidErr := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		cred, credErr := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if pidErr != nil || credErr != nil || cred == nil {
			return
		}
		out = onnxWorkerPeer{PID: pid, UID: int(cred.Uid), GID: int(cred.Groups[0])}
	})
	if err != nil || out.PID <= 0 || out.UID < 0 {
		return onnxWorkerPeer{}, fmt.Errorf("%w: query LOCAL_PEERPID/LOCAL_PEERCRED", errONNXWorkerPeerUnavailable)
	}
	return out, nil
}
