//go:build !darwin && !linux

package processor

import "net"

func onnxWorkerPeerIdentityPlatform(net.Conn) (onnxWorkerPeer, error) {
	return onnxWorkerPeer{}, errONNXWorkerPeerUnavailable
}
