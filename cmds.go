package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

// ErrUsage is a sentinel error for when commands detect an for invalid
// combinations of flags or args. Usually kong handles all this, but sometimes
// you cannot express an invalid combination of args/flags in the kong tags.
// It will typically be wrapped so should be checked with `errors.Is()`.
var ErrUsage = errors.New("usage error")

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
	Input SonyCmdInput `cmd:""`

	braviaAPI
}

// SonyCmdPower is the kong CLI struct for the `sony power` command.
type SonyCmdPower struct {
	State string `arg:"" optional:"" default:"" enum:",on,off" help:"Get/set power state"`
}

// SonyCmdInput is the kong CLI struct for the `sony input` command.
type SonyCmdInput struct {
	List  bool
	Label string `arg:"" optional:"" default:"" help:"Get/set input"`
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

// Run (sony input) gets or sets the currently displayed input of a Sony Bravia
// TV set. If no argument is provided and the flag --list is not specified, the
// currently selected input is printed with the label of the input as
// configured on the TV, or with an input URI if no label is set. If --list is
// specified, all the available input URIs with their labels (if any) are
// listed. If an argument is provided and matches the label of one of the
// inputs, the TV is set to that input. Otherwise the argument is assumed to be
// a URI and sets the input to that URI.
func (sc *SonyCmdInput) Run(cli *CLI) error {
	if sc.Label != "" && sc.List {
		return fmt.Errorf("%w: cannot use --list with a label", ErrUsage)
	}

	c := NewRESTClient(cli.TV.Hostname, cli.TV.PSK)
	labels, err := c.Inputs()
	if err != nil {
		return fmt.Errorf("getting labels: %w", err)
	}

	switch {
	// List all inputs
	case sc.Label == "" && sc.List:
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "URI\tLABEL")

		// Get the URIs from the map and sort them
		uris := make([]string, 0, len(labels))
		for k := range labels {
			if strings.HasPrefix(k, "extInput:") {
				uris = append(uris, k)
			}
		}
		sort.Strings(uris)

		for _, uri := range uris {
			fmt.Fprintf(tw, "%s\t%s\n", uri, labels[uri])
		}
		tw.Flush() //nolint:errcheck,gosec

	// Show selected input
	case sc.Label == "" && !sc.List:
		uri, err := c.SelectedInput()
		if err != nil {
			return fmt.Errorf("selected input: %w", err)
		}
		label := labels[uri]
		if label == "" {
			label = "unlabelled: " + uri
		}
		fmt.Println(label)

	// Select input by label
	case sc.Label != "":
		uri := labels[sc.Label]
		if uri == "" {
			uri = sc.Label
		}
		if err := c.SetInput(uri); err != nil {
			return fmt.Errorf("set input: %w", err)
		}
	}
	return nil
}
