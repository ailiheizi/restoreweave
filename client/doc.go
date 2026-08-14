// Package client contains the public, thin-client harness shared by the
// RestoreWeave CLI and other local frontends. It carries no server logic.
//
// The command package defines the operation vocabulary spoken by the client,
// and the local package resolves the daemon's local socket and directory
// layout on this machine.
package client
