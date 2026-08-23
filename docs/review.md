# Review and cleanup, August 2026

A review of the tool against one question: is this simple enough to set up once
and then forget about? Most of what follows is deletion.

The reverse-engineering work was not touched. The protocol, the encoders and the
lookup tables are the part of this repository that took real effort to get
right, they are tested against captures, and they were already correct.

## What was wrong

**The web GUI was reachable by the whole network, and by any website.** Two
separate mistakes with the same consequence. The listener bound `:8080`, which
is every interface, not loopback — so any machine on the same wifi could open
the GUI and change your microphone. And the WebSocket origin check read the
`Host` header, which is the *server's* address and is identical for every
request that arrives, instead of `Origin`, which is the page doing the calling.
It therefore returned true for everything. Deleting the check entirely would
have been safer than keeping it, because gorilla/websocket's default does the
comparison correctly.

Now: loopback only, deliberately not configurable, and the origin check compares
`Origin` against `Host` with the port attached. `internal/server/origin_test.go`
pins it.

**`defaults` could not turn an effect off.** `SendAllParams` sent an effect's
on/off packet only when the effect was *on*, so an effect that had been switched
off was never told. `rode-dsp defaults` would print "Compressor: DISABLED" while
the compressor carried on running. `load` had the same hole. The GUI had already
worked around it with its own private copy of the apply logic; the CLI never got
the fix.

Now there is one apply path, `Device.Apply`, used by the CLI and the GUI alike:
every parameter, then the on/off switch, for every effect.

**A backgrounded browser tab could crash the tool.** The WebSocket hub closed a
slow client's channel while the connection's own goroutine was still writing to
it — a panic that would take down the whole process. See below; the fix was to
delete the hub.

**A stale binary was committed, and the installer preferred it.** `rode-dsp`
(10 MB) was tracked in git, and `install.sh` checked "is there a binary here?"
before installing, so `git clone && sudo ./install.sh` would install whatever
binary happened to be committed — no build, no error, wrong architecture on
anything but x86-64 Linux. It is now untracked and gitignored, and the installer
builds if there is nothing to install.

## What got simpler

**The device layer lost its concurrency.** `internal/hid` had a send queue, a
worker goroutine, a polling read goroutine, an ACK channel and three lifecycle
channels — about 570 lines. Applying a config is twenty packets to a device that
answers in under a millisecond, so none of it bought anything, and it cost:
errors from a queued write were discarded (`Send` always returned nil, so the
CLI printed "OK" no matter what happened), `Disconnect` replaced the channels
its own goroutines were still using, `Flush` guessed with a sleep, and the read
loop spun without backoff if the microphone was unplugged mid-run.

It is now one synchronous `exchange`: write a packet, wait for the reply, return
it. Every one of those bugs is gone because the code they lived in is gone, and
a failed write is now something the caller actually hears about.

**The GUI lost its hub.** A `Hub` goroutine, four channels, a per-client send
channel and a `writePump` with ping/pong keepalive, all to serve one browser tab
on localhost. It is now a map of connections and a mutex around each connection's
writes; a broadcast is a loop. The keepalive went too — there is no proxy between
the browser and a server on the same machine that needs convincing the connection
is alive.

**Commands.** `disconnect` did nothing at all — each CLI run is a fresh process,
so it always printed "Device is not connected." and exited. `connect` was `load`
with a different name. Both are gone; `load` is the command that applies your
settings. The three reverse-engineering commands moved to an "Advanced" section
of the help so the everyday list is eight entries. `AddCommand` and the per-command
`FlagSet` plumbing were unused and are gone.

**Config writing.** The config and the presets each had their own copy of the
temp-file-and-rename dance, and neither flushed before renaming — which is the
one thing that makes the rename actually protect anything. There is now one
`writeJSON` doing it properly, with a unique temp name so the CLI and a running
GUI cannot collide.

## Smaller things

- Help output listed commands in random order (Go map iteration). Sorted now,
  with `RODE_DSP_CONFIG` documented where people will see it.
- Debug output went to stdout, where it corrupted `status --json`. It goes to
  stderr.
- "Can't open the microphone" now says what to do about it: on Linux that is
  almost always the missing udev rule, so the error names the installer.
- The GUI opened a browser after a hopeful 500 ms sleep. It binds the port
  first, then opens the browser at the address it actually got.
- `--version` was a hardcoded string. It comes from the build now, so there is
  nothing to remember to bump.
- CI built only on Linux for a tool whose whole point is that it runs where RØDE
  Connect doesn't. It builds and tests on Linux, macOS and Windows.

## Deliberately not done

These came up and were rejected as more machinery than the problem deserves:

- **An interface around the device, so the server could be tested with a fake.**
  Correct in a larger codebase. Here it adds indirection to make room for tests
  nobody is going to write for a tool that talks to one USB device.
- **`log/slog`.** The structured-logging story is worth having when something
  ingests the logs. Nothing does. `log` to stderr is right for this.
- **A config library (viper, koanf).** One environment variable and a flag. The
  standard library is the correct answer and a dependency would be a step back.
- **A `--host` flag on the GUI.** Making it possible to expose a passwordless
  microphone-control panel to the network is not a feature, and the flag would
  invite exactly that.
- **Graceful HTTP shutdown.** Ctrl-C on a local GUI. The listener closes and the
  process exits.
- **Firmware 2.1.2 and older** still need the Q16 scale factor and still are not
  supported. Q9 in `04-open-questions.md` has the detail; it needs a device on
  that firmware to verify against, which nobody has.

## One change worth knowing about

The startup GET sweep — twenty reads that RØDE Connect performs and whose
results were thrown away — is gone, along with `connect --no-init`. The
reverse-engineering notes conclude the device does not require it and that it
reads rather than writes, so skipping it should be invisible. It could not be
tested against hardware here. If applying settings ever starts behaving oddly on
a fresh plug-in, this is the first thing to put back.
