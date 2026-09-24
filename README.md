# rode-dsp

[![CI](https://github.com/andymason/rode-dsp/actions/workflows/ci.yml/badge.svg)](https://github.com/andymason/rode-dsp/actions/workflows/ci.yml)

Controls the RØDE NT-USB Mini's built-in sound processing, on Linux.

## Description

The NT-USB Mini processes sound inside the microphone, before it reaches the
computer. Four processors are available: a compressor, a noise gate, an aural
exciter and a big bottom.

The microphone holds these settings only while it has power. They are lost
whenever it is unplugged or the computer is shut down. rode-dsp saves the chosen
settings and sends them back whenever the microphone is detected: at startup, on
connection, and after resume.

RØDE Connect controls the same processors, but runs only on Windows and macOS.

## Requirements

- A RØDE NT-USB Mini.
- Linux, with systemd for the startup service.

On Windows and macOS, use [RØDE
Connect](https://rode.com/en/software/rode-connect) instead.

## Installation

Download `rode-dsp-linux-amd64` from the [latest
release](https://github.com/andymason/rode-dsp/releases/latest), or
`rode-dsp-linux-arm64` for an ARM machine such as a Raspberry Pi. Then, in the
download folder:

```
chmod +x rode-dsp-linux-amd64
sudo ./rode-dsp-linux-amd64 setup
rode-dsp gui
```

Setup asks for a password. The last command opens the settings page in a
browser; settings apply as they are changed, and are saved automatically.

To remove: `sudo rode-dsp setup --remove`. Saved settings are kept; delete
`~/.config/rode-dsp` to remove those too.

## Processors

| | |
|---|---|
| **Compressor** | Evens out loud and quiet passages. |
| **Noise gate** | Attenuates background sound between words. |
| **Aural exciter** | Adds upper harmonics for clarity. |
| **Big bottom** | Adds low-end weight. |

All are off by default and can be used alone or together. The settings page
shows a live level meter and a picture of what each processor is doing. One
preset, **Radio Voice**, is built in; others can be saved and shared.

The microphone has no volume control, equaliser, high-pass filter or de-esser.
The four processors above are everything it exposes.

## Usage

```
rode-dsp setup                          install, and reapply at startup
rode-dsp gui                            open the settings page
rode-dsp status                         report the microphone's current values
rode-dsp comp --enable --threshold -20  enable the compressor
rode-dsp gate --enable --attack 0.8     enable the noise gate
rode-dsp load                           send the saved settings again
rode-dsp defaults                       reset all values to their defaults
```

`rode-dsp <command> -help` lists that command's options.

`status` reads the microphone rather than the saved file, so it stays correct
after another program has changed something. `--config-only` reports the saved
values instead.

## Files

Nothing is written until a value is changed. Until then the microphone runs on
its own defaults.

| | |
|---|---|
| Linux/BSD | `$XDG_CONFIG_HOME/rode-dsp/` (usually `~/.config/rode-dsp/`) |
| Windows | `%AppData%\rode-dsp\` |
| macOS | `~/Library/Application Support/rode-dsp/` |

`config.json` holds the current settings, `presets.json` the saved presets.
`--config <path>` or `$RODE_DSP_CONFIG` selects a different file; `--config`
wins. `rode-dsp load --config <file>` applies a file from elsewhere.

The settings page binds to loopback only. It has no password, so access is
limited to this machine by design.

## What setup installs

`rode-dsp setup --dry-run` prints every file it would write, with contents, and
writes nothing.

| | |
|---|---|
| `/usr/local/bin` | the program |
| `/etc/udev/rules.d` | a rule granting access without `sudo`, and a trigger for the service |
| `/etc/systemd/system` | a service that sends the settings whenever the microphone appears |

The service reads the settings file in the installing user's home directory, so
a changed value takes effect at the next trigger with nothing to reinstall.
`--no-boot` skips the service; `--bin-dir`, `--udev-dir` and `--unit-dir` change
the locations above.

Distributions without `uaccess` (Alpine, Void, Gentoo/OpenRC) need the access
rule edited: replace `TAG+="uaccess"` with `GROUP="plugdev"` and join that
group. The service requires systemd, so use `--no-boot` there.

## Exit status

A missing microphone is reported separately from a failure, which is how the
service treats a switched-off microphone as normal.

| Code | Meaning |
|---|---|
| 0 | Applied |
| 1 | Failure — invalid settings, permission denied, protocol error |
| 2 | Microphone not connected |

`load --wait N` retries for up to N seconds, for a microphone that is slow to
appear.

## Troubleshooting

**Microphone not found.** Check that it is connected. If it is, unplug and
reconnect it: the access rule reaches only microphones connected after setup ran.

**Settings not restored after a restart.** Check the service with `systemctl
status rode-dsp`. It reports as skipped until a value has been set, because
there is nothing to send.

**No volume control.** The microphone has none. Use the system sound settings.

**Firmware 2.1.2 or older.** These need a different scale factor, which is not
implemented. Update with RØDE Central, or see Q9 in
`docs/re/04-open-questions.md`.

## Building from source

```
go build
go test ./...
```

cgo is required, because the USB library is C: a C toolchain must be present and
`CGO_ENABLED` must not be `0`. Cross-compiling therefore needs a cross
toolchain. Linux also needs the libudev headers:

| Distribution | Package |
|---|---|
| Debian/Ubuntu | `apt install libudev-dev` |
| Fedora | `dnf install systemd-devel` |
| Arch | `pacman -S systemd` |
| openSUSE | `zypper install libudev-devel` |

`internal/protocol` and `internal/dsp` build without libudev, so the
correctness-critical tests run anywhere.

## Protocol notes

RØDE publish no specification, so the protocol was recovered by reading their
application. The encodings were taken from RØDE Connect's instruction stream
rather than inferred from recordings. The microphone's seven lookup tables were
extracted from the binary. What is sent here is therefore what the official
application sends, and `internal/protocol/capture_oracle_test.go` pins that
against real USB captures.

Two limits apply. Values quantise to the device's 256 steps, so a value read
back can differ from the one written by up to one step. Some arithmetic runs at
a different precision here than in the original, differing by about one part in
2.4 million.

`docs/re/` documents the tooling, the wire protocol, the encoder and decoder
formulas, the lookup-table extraction and the open questions.
`tools/frida/` holds the instrumentation scripts. Three commands exist for that
work rather than daily use:

```
rode-dsp probe-effects    which effect IDs hold state
rode-dsp read-raw         probe HID report 0x03
rode-dsp send-raw --i-know-what-this-does <hex>
```

There is no equaliser, high-pass filter or de-esser on this microphone. RØDE
Connect contains panels for all three and hides them; the firmware has no
matching DSP blocks. Q5 in `docs/re/04-open-questions.md` has the evidence.
