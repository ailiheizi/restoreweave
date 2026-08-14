//go:build unix

package rpc

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"

	"google.golang.org/grpc/credentials"
)

// fdAuth carries pre-opened source and staging descriptors received over
// SCM_RIGHTS. It is connection-scoped AuthInfo, not a payload.
type fdAuth struct {
	credentials.CommonAuthInfo
	source  int
	staging int
}

func (fdAuth) AuthType() string { return "unix-fd" }

// fdCreds is private Unix transport credentials: the client sends two FDs
// before HTTP/2, the server receives them during ServerHandshake, and gRPC
// messages stay control-only.
type fdCreds struct {
	source  *os.File
	staging *os.File
}

func (c *fdCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "unix-fd", SecurityVersion: "1.0"}
}

func (c *fdCreds) Clone() credentials.TransportCredentials {
	cloned := *c
	return &cloned
}

func (c *fdCreds) OverrideServerName(string) error { return nil }

func (c *fdCreds) ClientHandshake(_ context.Context, _ string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.source == nil || c.staging == nil {
		return nil, nil, fmt.Errorf("client unix-fd credentials require source and staging files")
	}
	unixConn, err := unixConnOf(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := sendFDs(unixConn, int(c.source.Fd()), int(c.staging.Fd())); err != nil {
		return nil, nil, err
	}
	runtime.KeepAlive(c.source)
	runtime.KeepAlive(c.staging)
	return raw, fdAuth{
		CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity},
		source:         int(c.source.Fd()),
		staging:        int(c.staging.Fd()),
	}, nil
}

func (c *fdCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	unixConn, err := unixConnOf(raw)
	if err != nil {
		return nil, nil, err
	}
	fds, err := recvFDs(unixConn, 2)
	if err != nil {
		return nil, nil, err
	}
	return raw, fdAuth{
		CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity},
		source:         fds[0],
		staging:        fds[1],
	}, nil
}

func unixConnOf(conn net.Conn) (*net.UnixConn, error) {
	if unixConn, ok := conn.(*net.UnixConn); ok {
		return unixConn, nil
	}
	return nil, fmt.Errorf("connection is %T, want *net.UnixConn", conn)
}
