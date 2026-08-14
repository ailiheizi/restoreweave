//go:build unix

package rpc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func sendFDs(conn *net.UnixConn, fds ...int) error {
	if len(fds) == 0 {
		return fmt.Errorf("at least one file descriptor is required")
	}
	oob := unix.UnixRights(fds...)
	_, _, err := conn.WriteMsgUnix([]byte{byte(len(fds))}, oob, nil)
	return err
}

func recvFDs(conn *net.UnixConn, want int) ([]int, error) {
	if want < 1 {
		return nil, fmt.Errorf("fd count must be positive")
	}
	data := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(4*want))
	_, oobn, _, _, err := conn.ReadMsgUnix(data, oob)
	if err != nil {
		return nil, err
	}
	if int(data[0]) != want {
		return nil, fmt.Errorf("received %d fds, want %d", data[0], want)
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no SCM_RIGHTS control message")
	}
	fds, err := unix.ParseUnixRights(&msgs[0])
	if err != nil {
		return nil, err
	}
	if len(fds) != want {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
		return nil, fmt.Errorf("parsed %d fds, want %d", len(fds), want)
	}
	return fds, nil
}
