// Package conformance holds offline audits of generated data against
// recordings taken from real servers.
//
// The recordings under testdata are captured by the vanilla-tagged tests in
// client — the lane that can start a real server — and committed, so the
// audits here run without a jar, a workspace, or a network. What an audit
// asserts is deliberately the drift itself: a test that pins "the dataset
// disagrees with the server in these ways" fails the day the dataset is
// corrected, which is the day the pin should come off.
package conformance
