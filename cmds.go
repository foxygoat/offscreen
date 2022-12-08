package main

import (
	"fmt"
)

// braviaAPI is a kong CLI struct to be embedded in command structs that
// talk to a Sony Bravia TV set. It contains the parameters to communicate
// with a TV using the Bravia REST IP control protocol.
type braviaAPI struct {
	Hostname string `env:"OFFSCREEN_HOSTNAME" help:"Hostname of Sony Bravia TV"`
	PSK      string `env:"OFFSCREEN_PSK" help:"Pre-shared key"`
}

// SonyCmd is the kong CLI struct for the `sony` command.
type SonyCmd struct {
	Power SonyCmdPower `cmd:""`

	braviaAPI
}

// SonyCmdPower is the kong CLI struct for the `sony power` command.
type SonyCmdPower struct {
	State string `arg:"" optional:"" default:"" enum:",on,off" help:"Get/set power state"`
}

// Run (sony power) gets or sets the power state of a Sony Bravia TV. If no
// argument is provided, the current power state is printed. If the argument is
// present and is "on", the TV is turned on. If it is "off" the TV is turned
// off.
func (sc *SonyCmdPower) Run(cli *CLI) error {
	c := NewRESTClient(cli.TV.Hostname, cli.TV.PSK)
	if sc.State == "" {
		state, err := c.PowerStatus()
		if err != nil {
			return fmt.Errorf("power status: %w", err)
		}
		fmt.Println(state)
		return nil
	}
	status := false
	if sc.State == "on" {
		status = true
	}
	return c.SetPowerStatus(status)
}
