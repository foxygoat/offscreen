//nolint:goerr113 // Dynamic errors in main is OK
package main

import (
	"fmt"
	"sync/atomic"

	"github.com/anoopengineer/edidparser/edid"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

type Screen struct {
	xconn   *xgb.Conn
	rootWin xproto.Window

	manufacturerID string
	productCode    uint16

	ssOn    atomic.Bool
	present atomic.Bool
}

func NewScreen(display, manufacturerID string, productCode uint16) (*Screen, error) {
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("could not open display %s: %w", display, err)
	}

	// Intitialise the RANDR and SCREENSAVER extensions. These will fail if the
	// X11 server does not support these extensions.
	if err := randr.Init(c); err != nil {
		return nil, fmt.Errorf("could not initialise RANDR extension: %w", err)
	}
	if err := screensaver.Init(c); err != nil {
		return nil, fmt.Errorf("could not initialise SCREENSAVER extension: %w", err)
	}

	s := &Screen{
		xconn:          c,
		rootWin:        xproto.Setup(c).DefaultScreen(c).Root,
		manufacturerID: manufacturerID,
		productCode:    productCode,
	}

	// Set the initial state of the screen saver and monitor presence.
	ssOn, err := s.queryScreenSaver()
	if err != nil {
		return nil, fmt.Errorf("could not query screen saver state: %w", err)
	}
	s.ssOn.Store(ssOn)

	present, err := s.queryPresence()
	if err != nil {
		return nil, fmt.Errorf("could not query TV presence: %w", err)
	}
	s.present.Store(present)

	return s, nil
}

func (s *Screen) Close() {
	s.xconn.Close()
}

func (s *Screen) IsScreenSaverOn() bool {
	return s.ssOn.Load()
}

func (s *Screen) IsPresent() bool {
	return s.present.Load()
}

func (s *Screen) Blank() error {
	return xproto.ForceScreenSaverChecked(s.xconn, xproto.ScreenSaverActive).Check()
}

func (s *Screen) Watch(ch chan<- bool) error {
	defer close(ch)

	// Listen for randr events (monitor plug/unplug)
	err := randr.SelectInputChecked(s.xconn, s.rootWin, randr.NotifyMaskOutputChange).Check()
	if err != nil {
		return fmt.Errorf("could not watch RANDR events: %w", err)
	}

	// Listen for screensaver events (screensaver on/off)
	// For some reason, screensaver wants the root window as a "Drawable"
	drawableRoot := xproto.Drawable(s.rootWin)
	err = screensaver.SelectInputChecked(s.xconn, drawableRoot, screensaver.EventNotifyMask).Check()
	if err != nil {
		return fmt.Errorf("could not watch SCREENSAVER events: %w", err)
	}

	for {
		ev, err := s.xconn.WaitForEvent()
		if err != nil {
			return fmt.Errorf("could not wait for events: %w", err)
		}
		if ev == nil { // X11 connection closed
			return nil
		}
		switch event := ev.(type) {
		case screensaver.NotifyEvent:
			isOn := event.State == screensaver.StateOn || event.State == screensaver.StateCycle
			wasOn := s.ssOn.Swap(isOn)
			// Send the screensaver state if it changes and the monitor is present
			if isOn != wasOn && s.IsPresent() {
				ch <- isOn
			}
		case randr.NotifyEvent:
			// It is too hard to determine from the randr event whether it is for
			// the display being connected/disconnected, so for every randr event,
			// just check the presence by checking the randr properties.
			present, err := s.queryPresence()
			if err != nil {
				return fmt.Errorf("could not query TV presence: %w", err)
			}
			wasPresent := s.present.Swap(present)
			// If the monitor has just appeared, send the screensaver state
			if present && !wasPresent {
				ch <- s.IsScreenSaverOn()
			}
		}
	}
}

func (s *Screen) queryScreenSaver() (bool, error) {
	info, err := screensaver.QueryInfo(s.xconn, xproto.Drawable(s.rootWin)).Reply()
	if err != nil {
		return false, fmt.Errorf("QueryInfo failed: %w", err)
	}
	return info.State == screensaver.StateOn, nil
}

func (s *Screen) queryPresence() (bool, error) {
	var present bool
	err := RangeEDID(s.xconn, s.rootWin, func(_ randr.Output, e *edid.Edid) (bool, error) {
		if e.ManufacturerId == s.manufacturerID && e.ProductCode == s.productCode {
			present = true
			return false /* stop ranging */, nil
		}
		return true /* keep ranging */, nil
	})
	return present, err
}

// RangeEDIDFunc is called by [RangeEDID] for each X11 xrandr output that has
// EDID data. The function returns a bool that tells [RangeEDID] whether to
// continue ranging over subsequent outputs or not, and an error that if not
// nil will be returned to the caller of [RangeEDID]. If the RangeEDIDFunc
// returns false or an error, [RangeEDID] terminates and returns to the caller.
type RangeEDIDFunc func(output randr.Output, edidData *edid.Edid) (cont bool, err error)

// RangeEDID calls fn for each X11 xrandr output that has an EDID property.
// If fn returns false or an error, iteration will terminate. The error is
// returned.
//
// If root is zero (not a valid window ID) then RangeEDID will get it from
// the provided xgb.Conn. This needs to unpack a bunch of serialised data,
// so it can be more efficient to provide the root window ID if you have it.
func RangeEDID(c *xgb.Conn, root xproto.Window, fn RangeEDIDFunc) error {
	if root == xproto.Window(0) {
		root = xproto.Setup(c).DefaultScreen(c).Root
	}

	r, err := randr.GetScreenResourcesCurrent(c, root).Reply()
	if err != nil {
		return fmt.Errorf("could not get screens: %w", err)
	}

	edidAtom, err := xproto.InternAtom(c, false /* OnlyIfExists */, 4, "EDID").Reply()
	if err != nil {
		return fmt.Errorf("could not intern X11 atom: %w", err)
	}

	for _, output := range r.Outputs {
		// the length of 64 gives a maximum EDID data size of 256 bytes (4 * 64).
		// EDID maxes out at 256 bytes long, so should be fine.
		const offset, length, del, pending = 0, 64, false, false
		// https://cgit.freedesktop.org/xorg/proto/randrproto/tree/randrproto.txt#n872
		opr, err := randr.GetOutputProperty(c, output, edidAtom.Atom, xproto.AtomAny, offset, length, del, pending).Reply()
		if err != nil {
			return fmt.Errorf("could not get output properties: %w", err)
		}
		if opr.BytesAfter != 0 {
			return fmt.Errorf("EDID data too large. Max is 256 bytes, got %d bytes", 256+opr.BytesAfter)
		}
		if len(opr.Data) == 0 {
			continue
		}
		ed, err := edid.NewEdid(opr.Data)
		if err != nil {
			return fmt.Errorf("could not parse EDID data: %w", err)
		}
		if cont, err := fn(output, ed); !cont || err != nil {
			return err
		}
	}
	return nil
}
