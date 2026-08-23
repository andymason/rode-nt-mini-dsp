# RØDE NT-USB Mini DSP tool

CLI and web GUI for the RØDE NT-USB Mini's onboard Aphex DSP — compressor, noise
gate, aural exciter and big bottom — over USB HID.

RØDE Connect is Windows and macOS only. This works anywhere Go and hidapi do.

## Install

On Linux, one command does everything — it builds the tool if you have Go, puts
it on your `PATH`, and sets up permissions:

```
sudo ./packaging/linux/install.sh
```

Add `--boot` to also re-apply your settings automatically whenever the
microphone is plugged in. See [Linux setup](#linux-setup) for what that
installs and how to remove it.

## Build it yourself

```
go build
```

cgo is required (hidapi is C), so a C toolchain must be present and
`CGO_ENABLED` must not be 0. That also means no cross-compiling without a
cross toolchain — build each platform on that platform.

Linux additionally needs the libudev development headers:

| Distro | Package |
|---|---|
| Debian/Ubuntu | `apt install libudev-dev` |
| Fedora | `dnf install systemd-devel` |
| Arch | `pacman -S systemd` (libudev ships with systemd) |
| openSUSE | `zypper install libudev-devel` |

## Linux setup

### Run it yourself

```
sudo ./packaging/linux/install.sh
```

This builds the binary if there isn't one yet, installs it to
`/usr/local/bin/rode-dsp` along with one udev rule, and reloads udev so the
rule applies to the microphone already plugged in. Without the rule `rode-dsp`
can only open the device as root.

The rule matches this one device and uses systemd's `uaccess`, so access
follows whoever is logged in at the seat: no group to create or join, no
world-writable device node. Non-systemd distros (Alpine, Void, Gentoo/OpenRC)
have no `uaccess` — swap the tag for `GROUP="plugdev"` and add yourself to
that group.

### Apply settings automatically

The microphone has no memory: its DSP settings are gone every time it loses
power. To restore them without thinking about it:

```
sudo ./packaging/linux/install.sh --boot
```

That adds `rode-dsp.service` and a udev rule that starts it. The udev trigger
is the whole mechanism — it fires on cold boot, on hotplug, and on
re-enumeration after suspend — so there is no `systemctl enable` step and no
second boot-time unit.

No config is installed or copied. The unit reads your own
`~/.config/rode-dsp/config.json`, so changing a setting takes effect at the
next boot with nothing to re-publish. You can install this before setting
anything up: until that file exists the unit skips itself
(`ConditionPathExists=`), which systemd logs as skipped rather than failed,
and the microphone just uses its own defaults.

The service runs as root, which is what makes it independent of `uaccess`
(at boot nobody is logged in yet, so `uaccess` grants nothing) and of any
desktop session. A microphone that is switched off is a clean success rather
than a failed unit, via `SuccessExitStatus=2`.

```
systemctl status rode-dsp    # how the last run went
journalctl -u rode-dsp       # history
sudo ./packaging/linux/install.sh --uninstall
```

`packaging/linux/` holds the unit and rules as plain files if you would rather
place them yourself — replace `@CONFIG@` in the unit with your config path,
which cannot be written as `%h` because that expands to `/root` in a system
unit. The installer's locations follow the FHS split between local software
and packaged software (`/usr/local/bin` for a hand-built binary, `/etc` for
admin-installed rules and units), and both are overridable:

```
sudo BIN_DIR=/opt/bin ./packaging/linux/install.sh --boot
```

## Use

```
rode-dsp status                         # connection and current values
rode-dsp comp --enable --threshold -20
rode-dsp gate --enable --attack 0.8 --hold 80
rode-dsp load                           # apply the saved config
rode-dsp defaults                       # reset everything
rode-dsp gui                            # web UI, http://127.0.0.1:8080
```

The GUI listens on loopback only. It has no password, so anything that can
reach it can change your settings — which is why it is not reachable from the
rest of your network.

### Where settings live

There is no config file until you change something. A fresh install writes
nothing, and the microphone runs on its own defaults — `status` and opening the
GUI both leave the disk alone. The first time you set a value, from
the CLI or the GUI, `config.json` is created in the platform's per-user config
directory, with `presets.json` beside it:

| | |
|---|---|
| Linux/BSD | `$XDG_CONFIG_HOME/rode-dsp/` (default `~/.config/rode-dsp/`) |
| Windows | `%AppData%\rode-dsp\` |
| macOS | `~/Library/Application Support/rode-dsp/` |

To use a different file, pass `--config <path>` or set `$RODE_DSP_CONFIG`;
`--config` wins. `rode-dsp load --config <file>` applies a config someone sent
you, and the GUI's import does the same in the browser. Presets always sit
beside whichever config is in use.

Exit codes separate a missing microphone from a real failure, which is what
lets the service treat a switched-off microphone as success:

| Code | Meaning |
|---|---|
| 0 | applied |
| 1 | failure — bad config, permission denied, protocol error |
| 2 | microphone not connected |

```sh
if rode-dsp load --quiet; then :
elif [ $? -eq 2 ]; then echo "mic not connected, skipping"
fi
```

`load --wait N` retries for up to N seconds, covering a microphone that
enumerates a moment late.

## Accuracy

The encodings were read out of RØDE Connect's instruction stream rather than
fitted to captures, and the device's seven lookup tables were extracted from the
binary itself, so what this sends is what the official application sends.

Two limits are worth knowing:

- Values quantise to the device's 256 steps, so reading a parameter back can
  differ from what was written by up to one step. That is the hardware's
  resolution.
- `powf`, `logf` and `cos` run in float32 in the binary and float64 here, which
  can differ by a single LSB — around one part in 2.4 million on a 31-bit
  coefficient.

Microphones on firmware 2.1.2 or older need a different scale factor that is not
implemented; see Q9 in `docs/re/04-open-questions.md`.
`internal/protocol/capture_oracle_test.go` pins behaviour against the USB
captures so any change is deliberate.

## Reverse-engineering notes

`docs/re/` documents the protocol and the tooling used to work it out:

| | |
|---|---|
| `00-setup.md` | Ghidra + GhidrAssistMCP, Frida, USB capture |
| `01-addresses.md` | function and data addresses in `RODE Connect.exe` |
| `02-protocol.md` | the USB HID DSP wire protocol |
| `03-encoders.md` | exact encoder and decoder formulas |
| `04-open-questions.md` | what is still unresolved, and what is settled |
| `05-juce.md` | using the JUCE source to read the UI and stream code |
| `06-static-extraction.md` | extracting the lookup tables from the PE image |

`tools/frida/` holds instrumentation scripts for the RØDE Connect binary.

The microphone's DSP state is readable, so `rode-dsp status` reports what the
device actually holds rather than the last-saved config — it stays correct even
after RØDE Connect or another instance has changed something. Pass
`--config-only` for the config's view.

Three commands exist for protocol work rather than daily use:

```
rode-dsp probe-effects    # which effect IDs actually hold state?
rode-dsp read-raw         # probe HID report 0x03
rode-dsp send-raw --i-know-what-this-does <hex>
```

There is no Equalizer, High Pass Filter or De-Esser on this microphone. RØDE
Connect contains panels for all three and hides them; the firmware has no
matching DSP blocks. Q5 in `04-open-questions.md` has the evidence.

## Tests

```
go test ./...
```

`internal/protocol` and `internal/dsp` build without libudev, so the encoder and
registry tests run anywhere.
