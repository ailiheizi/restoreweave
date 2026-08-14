// Package rpc is private Processor control plumbing: protobuf RUN_STAGE
// frames over a Unix socket, with large bytes on pre-opened file
// descriptors via SCM_RIGHTS. grpc-go wrapping uses the same messages and
// still passes FDs during the Unix handshake. It is not a public ABI.
// bubblewrap execution remains a Linux qualification. The host still
// digests and admits artifacts.
package rpc

type Request struct {
	AttemptID      string
	FenceToken     int64
	CapabilityID   string
	Stage          string
	MaxOutputBytes int64
	SourceFDIndex  int
	StagingFDIndex int
}

type Response struct {
	Status           string
	DeterminismClass string
	SchemaRef        string
	MediaType        string
	Sealed           bool
	Reason           string
}
