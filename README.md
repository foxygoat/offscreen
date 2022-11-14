# Offscreen

Offscreen is an X Windows program to watch for the screen saver being activated
and deactivated and turning an attached Sony Bravia TV set off and on.

The Sony Bravia 43X8000D is a 43-inch 4k TV screen with 4:4:4 chroma, making it
suitable to use as a computer monitor. However as it is a TV, it does not
support DPMI which puts a computer monitor into standby, low power mode. When a
PC turns on DPMI, the Sony TV displays a message saying nothing is connected -
hardly power saving.

Many Sony TVs have a control interface. In the olden days, this was a serial
interface with a custom protocol. Modern smart TVs from Sony have a HTTP/REST
API. With this control interface, we can turn the TV on and off.

By hooking into the XSCREENSAVER extension, we can watch when the screen saver
is enabled and turn off the TV. When the screen saver is disabled, we can turn
the TV on. To be even smarter, we can use the RANDR extension to check if the TV
is actually connected to the computer so that we do not try to turn it on and
off if it is not actually plugged into the machine. This is handy for laptops
which may get unplugged but remain on wifi, so are still able to control the TV
when we may not want it to.
