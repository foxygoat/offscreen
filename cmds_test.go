package main

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/matryer/is"
)

var buildtimeVarTests = []struct {
	name string

	buildHost, buildPSK string
	envHost, envPSK     string
	cliHost, cliPSK     string

	wantHost, wantPSK string
}{
	{"none", "", "", "", "", "", "", "", ""},
	{"build only", "example.com", "1234", "", "", "", "", "example.com", "1234"},
	{"env only", "", "", "example.com", "1234", "", "", "example.com", "1234"},
	{"cli only", "", "", "", "", "example.com", "1234", "example.com", "1234"},
	{"build+env", "example.com", "1234", "example2.com", "9876", "", "", "example2.com", "9876"},
	{"build+cli", "example.com", "1234", "", "", "example2.com", "9876", "example2.com", "9876"},
}

func TestBuildtimeVars(t *testing.T) {
	for _, tt := range buildtimeVarTests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			var cli CLI
			parser, err := kong.New(&cli)
			is.NoErr(err) // failed to create kong parser

			buildtimeHost = tt.buildHost
			buildtimePSK = tt.buildPSK
			t.Setenv("OFFSCREEN_HOSTNAME", tt.envHost)
			t.Setenv("OFFSCREEN_PSK", tt.envPSK)
			args := []string{"tv", "power"}
			if tt.cliHost != "" {
				args = append(args, "--hostname", tt.cliHost)
			}
			if tt.cliPSK != "" {
				args = append(args, "--psk", tt.cliPSK)
			}

			_, err = parser.Parse(args)
			is.NoErr(err)                          // failed to parse command line
			is.Equal(tt.wantHost, cli.TV.Hostname) // hostname incorrect
			is.Equal(tt.wantPSK, cli.TV.PSK)       // PSK incorrect
		})
	}
}
