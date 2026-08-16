package world

import (
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// Func adapts a function to Reducer, for a domain small enough not to need a
// type of its own and for tests.
type Func func(ctx *Context, batch version.Batch, collector *event.Collector) error

// Reduce implements Reducer.
func (f Func) Reduce(ctx *Context, batch version.Batch, collector *event.Collector) error {
	return f(ctx, batch, collector)
}
