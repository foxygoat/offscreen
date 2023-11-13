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

## Building

You can build offscreen with:

    make build

and the binary will be written to `out/offscreen` in the root of the repository.

⚠️  Caution ⚠️

If the environment variables `OFFSCREEN_HOSTNAME` or `OFFSCREEN_PSK` are set in
the environment when you build the binary with `make build` or `make install`,
the values from those environment variables will be built into the binary as the
default for the `--hostname` and `--psk` flags. The pre-shared key (PSK) is
sensitive so be aware if you are going to distribute the binary.

This is done to make it easier to use the binary across multiple machines
without requiring the PSK or hostname environment variables set up in your shell
rc file, or requiring the argument be supplied when running. But this could leak
your PSK if you share such a binary.

If you take the binary from the [offscreen GitHub releases page] or build it
with `go install foxygo.at/offscreen@latest`, there will be no default hostname
or PSK embedded in the binary.

[offscreen GitHub releases page]: https://github.com/foxygoat/offscreen/releases
