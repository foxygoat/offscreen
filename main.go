package main

import (
	"runtime"

	"github.com/alecthomas/kong"
)

var version string = "v0.0.0"

const description = `
offscreen turns off/on your Sony Bravia when the screen saver turns on/off
`

type CLI struct {
	Version kong.VersionFlag `short:"V" help:"Print program version"`

	TV SonyCmd `cmd:"" help:"query/control TV set"`
}

func main() {
	// Set maxprocs to 1 - this is a simple program and we don't want
	// more than one kernel thread for it, even on large boxes.
	runtime.GOMAXPROCS(1)

	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Description(description),
		kong.Vars{"version": version},
	)
	err := kctx.Run(&cli)
	kctx.FatalIfErrorf(err)
}
