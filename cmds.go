//nolint:goerr113 // dynamic errors in main are OK
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

// screenFlags is a kong CLI struct to be embedded in command structs that
// use a [Screen] struct for communicating with an X11 server. It has an
// [AfterApply] method that creates the [Screen] struct from the flags.
//
// [AfterApply]: https://github.com/alecthomas/kong#hooks-beforereset-beforeresolve-beforeapply-afterapply-and-the-bind-option
type screenFlags struct {
	Display      string `env:"DISPLAY" help:"X11 display to connect to"`
	Manufacturer string `default:"SNY" help:"EDID manufacturer ID of screen to manage"`
	ProductCode  uint16 `default:"63747" help:"EDID product code of screen to manage"`

	screen *Screen
}

// braviaAPI is a kong CLI struct to be embedded in command structs that
// talk to a Sony Bravia TV set. It contains the parameters to communicate
// with a TV using the Bravia REST IP control protocol.
type braviaAPI struct {
	Hostname string `env:"OFFSCREEN_HOSTNAME" help:"Hostname of Sony Bravia TV"`
	PSK      string `env:"OFFSCREEN_PSK" help:"Pre-shared key"`
}

// RunCmd is the kong CLI struct for the `run` command.
type RunCmd struct {
	braviaAPI
	screenFlags

	Input string `short:"i" help:"The TV input (label or URI) we are connected to"`
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
	screenFlags
	Input string `short:"i" help:"Specify host input, do not autodetect"`
}

// AfterApply creates a new [Screen] from the flags in the [screenFlags] struct.
func (sf *screenFlags) AfterApply() error {
	s, err := NewScreen(sf.Display, sf.Manufacturer, sf.ProductCode)
	if err != nil {
		return err
	}
	sf.screen = s
	return nil
}

// Run (offscreen run) runs offscreen to turn the connected TV on and off
// in line with X screen saver events.
func (cmd *RunCmd) Run() (err error) {
	defer cmd.screen.Close()

	// Install a panic handler to catch the errors from the ScreenWatcher
	// function. It cannot return errors, so it panics with them instead.
	defer func() {
		v := recover()
		if e, ok := v.(error); ok {
			err = e
		} else if v != nil {
			panic(v)
		}
	}()

	c := NewRESTClient(cmd.Hostname, cmd.PSK)
	ourInput, err := getInputURI(c, cmd.Input)
	if err != nil {
		return fmt.Errorf("could not get input URI for %s: %w", cmd.Input, err)
	}

	watcher := ScreenWatcherFunc(func(ssOn bool) {
		if err := ssChange(c, ourInput, ssOn); err != nil {
			panic(err)
		}
	})
	return cmd.screen.Watch(watcher)
}

// ssChange handles a screen saver change event, turning the TV on or
// off and possibly selecting our input on the TV.
func ssChange(c *RESTClient, ourInput string, ssOn bool) error {
	status, err := c.PowerStatus()
	if err != nil {
		return fmt.Errorf("could not get power status: %w", err)
	}

	// If the TV is off and the screen saver turns on, nothing to do
	// because the TV is already off.
	if status == "standby" && ssOn {
		return nil
	}

	// If the TV is off and the screen saver turns off, turn on the TV.
	// We may later change the input, but we can't do that now because we
	// cannot get the current input until the TV is on.
	if status == "standby" && !ssOn {
		if err := c.SetPowerStatus(true); err != nil {
			return fmt.Errorf("could not set power status: %w", err)
		}
	}

	// Get the selected input. We cannot do this before turning on the
	// TV otherwise the Bravia REST API returns an error.
	input, err := c.SelectedInput()
	if err != nil {
		return fmt.Errorf("could not get selected input: %w", err)
	}

	// If we turned on the TV and the currently selected input is not us,
	// select our input.
	if status == "standby" && !ssOn && input != ourInput {
		if err := c.SetInput(ourInput); err != nil {
			return fmt.Errorf("could not set input: %w", err)
		}
		return nil
	}

	// If the TV is on and the screen saver turns on, we turn off
	// the TV but only if our input is the current input. Otherwise
	// we leave it alone - the TV is showing the screen of another
	// machine so we should not blank the screen.
	if status == "active" && ssOn && input == ourInput {
		if err := c.SetPowerStatus(false); err != nil {
			return fmt.Errorf("could not set power status: %w", err)
		}
	}

	return nil
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
	ourInput, err := getInputURI(c, sc.Input)
	if err != nil {
		return fmt.Errorf("getting labels: %w", err)
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
		if input == ourInput {
			if err := sc.screen.Blank(); err != nil {
				return fmt.Errorf("could not blank screen: %w", err)
			}
			return nil
		}
		if err := c.SetInput(ourInput); err != nil {
			return fmt.Errorf("could not select input %s: %w", ourInput, err)
		}
		return nil
	}

	// Screen is off. turn it on and select our input
	if err := c.SetPowerStatus(true); err != nil {
		return fmt.Errorf("could not turn on screen: %w", err)
	}
	if err := c.SetInput(ourInput); err != nil {
		return fmt.Errorf("could not select input %s: %w", ourInput, err)
	}
	return nil
}

func getInputURI(c *RESTClient, label string) (string, error) {
	// If the label is already a URI, just return that.
	if strings.HasPrefix(label, "extInput:") {
		return label, nil
	}

	labels, err := c.Inputs()
	if err != nil {
		return "", fmt.Errorf("could not get available inputs: %w", err)
	}
	uri, ok := labels[label]
	if !ok {
		return "", fmt.Errorf("tv set does not have labelled input: %s", label)
	}

	return uri, nil
}
