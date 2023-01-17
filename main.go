package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/alecthomas/kong"
)

var version string = "v0.0.0"

const description = `
offscreen turns off/on your Sony Bravia when the screen saver turns on/off
`

type CLI struct {
	Version kong.VersionFlag `short:"V" help:"Print program version"`

	Run  RunCmd  `cmd:"" default:"1" help:"Run offscreen"`
	List ListCmd `cmd:"" help:"List connected monitor IDs"`
	TV   SonyCmd `cmd:"" help:"query/control TV set"`
}

func main() {
	// Set maxprocs to 1 - this is a simple program and we don't want
	// more than one kernel thread for it, even on large boxes.
	runtime.GOMAXPROCS(1)

	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Description(description),
		kong.Vars{"version": version},
		kong.PostBuild(func(k *kong.Kong) error {
			return kong.Visit(k.Model, setInputDefault)
		}),
	)
	err := kctx.Run(&cli)
	kctx.FatalIfErrorf(err)
}

// setInputDefault is a kong.Visitor that sets the default of any flag named
// "input" to the (possibly modified) hostname as a label. If the hostname is
// longer than 7 characters, it is truncated to 7 characters by taking the
// first six characters and appending the last character (e.g. palantir ->
// palantr). This is because the TV labels are limited to 7 characters and this
// transformation gives a reasonable looking name. It is called by [kong.Visit]
// in a [kong.PostBuild] function.
func setInputDefault(node kong.Visitable, next kong.Next) error {
	if f, ok := node.(*kong.Flag); ok && f.Name == "input" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("could not get hostname to set default input: %w", err)
		}
		if len(hostname) > 7 {
			hostname = hostname[0:6] + hostname[len(hostname)-1:]
		}
		f.Default = hostname
		f.HasDefault = true
	}
	return next(nil)
}
