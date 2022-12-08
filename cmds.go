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
	Power  SonyCmdPower  `cmd:""`
	Input  SonyCmdInput  `cmd:""`
	Toggle SonyCmdToggle `cmd:""`

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

// SonyCmdToggle is the kong CLI struct for the `sony toggle` command.
type SonyCmdToggle struct {
	Input string `short:"i" help:"Specify host input, do not autodetect"`
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

// Run (sony toggle) toggles the state of the TV based on a set of rules. If
// the TV is off, it will be turned on and the input labelled with the hostname
// will be selected. If the TV is on and the label of the currently selected
// input matches the hostname, the screen will be blanked. If the TV is on but
// the label of the currently selected input does not match the hostname, input
// labelled with the hostname will be selected.
//
// If the hostname is longer than 7 characters, it is truncated to 7 characters
// by taking the first 6 characters and the last character of the hostname.
// This is due to Sony Bravia labels being limited to 7 characters. The
// hostname can be overridden with the `--input <input>` flag. That value will
// not be truncated.
//
// The purpose of the (sony toggle) command is to be bound to a hot key so that
// when pressed, it causes the screen to be set to the host on which the key
// was pressed if the screen is not active for that machine. Otherwise it turns
// off the screen as an alternative to locking it when locking is not desired
// but there is no need to leave the screen on.
func (sc *SonyCmdToggle) Run(cli *CLI) error {
	c := NewRESTClient(cli.TV.Hostname, cli.TV.PSK)
	labels, err := c.Inputs()
	if err != nil {
		return fmt.Errorf("getting labels: %w", err)
	}

	// If input is not specified, determine it from our hostname. The
	// inputs on the TV set need to be labelled with the hostname, with
	// a max of 7 letters. If the hostname is longer, the label must be
	// the first 6 letters plus the last letter. e.g. "palantir" -> "palantr"
	if sc.Input == "" {
		if sc.Input, err = hostnameLabel(); err != nil {
			return err
		}
	}

	// If the input does not look like a URI, map it from labels if
	// we can. Otherwise just use the label as the URI.
	if !strings.HasPrefix(sc.Input, "extInput:") {
		input := labels[sc.Input]
		if input != "" {
			sc.Input = input
		}
	}

	status, err := c.PowerStatus()
	if err != nil {
		return fmt.Errorf("could not get power status: %w", err)
	}
	if status == "active" { //nolint:nestif // come on, it's not that "complex"!
		// turn off the screen if we are the current input, otherwise
		// switch to us.
		input, err := c.SelectedInput()
		if err != nil {
			return fmt.Errorf("could not get selected input: %w", err)
		}
		if input == sc.Input {
			// TODO(camh): Make this just enable the screen saver
			// when offscreen is complete and let it take care of
			// turning off the TV with the standad logic. That way
			// other screens attached to the host will also be blanked.
			if err := c.SetPowerStatus(false); err != nil {
				return fmt.Errorf("could not turn off screen: %w", err)
			}
			return nil
		}
		if err := c.SetInput(sc.Input); err != nil {
			return fmt.Errorf("could not select input %s: %w", sc.Input, err)
		}
		return nil
	}

	// Screen is off. turn it on and select our input
	if err := c.SetPowerStatus(true); err != nil {
		return fmt.Errorf("could not turn on screen: %w", err)
	}
	if err := c.SetInput(sc.Input); err != nil {
		return fmt.Errorf("could not select input %s: %w", sc.Input, err)
	}
	return nil
}

// hostnameLabel converts the machines hostname into a label for TV inputs.
// Labels are limited to seven characters. If the hostname is longer than that,
// the first six characters and the last character are used.
func hostnameLabel() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("could not get hostname: %w", err)
	}
	if len(hostname) > 7 {
		hostname = hostname[0:6] + hostname[len(hostname)-1:]
	}
	return hostname, nil
}
