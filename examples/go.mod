module github.com/go-theft-craft/headless-minecraft/examples

go 1.26.6

require github.com/go-theft-craft/headless-minecraft v0.0.0

require (
	github.com/go-theft-craft/minecraft-protocol v0.8.0
	github.com/go-theft-craft/minecraft-simulation v0.2.1-0.20260818172105-5d344056065b
)

// The examples track the working tree, not a release. They are the repository's
// integration surface, so they must fail when the library changes under them
// rather than keep compiling against the last tag.
replace github.com/go-theft-craft/headless-minecraft => ../
