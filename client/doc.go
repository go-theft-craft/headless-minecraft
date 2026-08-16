// Package client connects to a Java Edition server and publishes what it
// observes.
//
// It owns the connection lifecycle and one read loop. Observed world state
// is M7 and plugs into the batch boundary this package defines; gameplay
// actions are M9.
//
// The library never reconnects. Reconnecting can repeat actions, so retry
// policy belongs to the application.
package client
