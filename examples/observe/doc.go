// Command observe connects to a server, maintains an observed world, and
// prints every state event with the revision that produced it.
//
// It is the smallest program that exercises all of M7: the client applies each
// batch to a [world.World] before publishing that batch's events, so an event
// and the snapshot at its revision describe one instant, and this prints both
// sides of that.
//
// # Running it
//
//	go run ./observe -address localhost:25565
//	go run ./observe -address localhost:25565 -legacy   # protocol 47
//
// It takes the observe scope and sends nothing but the keepalive answers the
// client owes, so it is safe to point at a server a caller is allowed to watch
// and not act on.
//
// # What it prints
//
// One line per event: the revision, the domain, the event name, and a short
// description drawn from the event's own fields. Every line from one batch
// carries the same revision, which is what makes a protocol 775 bundle visible
// — several lines, one number. On exit it prints the final snapshot's counts.
//
// It renders no chat components. Both protocols send most text as a structured
// value and the library does not turn one into a line of text, so a message
// prints as the fact that it arrived rather than as its content.
package main
