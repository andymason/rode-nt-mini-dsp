# RØDE NT-USB Mini sound settings

[![CI](https://github.com/andymason/rode-dsp/actions/workflows/ci.yml/badge.svg)](https://github.com/andymason/rode-dsp/actions/workflows/ci.yml)

Set up how your RØDE NT-USB Mini sounds, on Linux. Then forget about it.

## What it does

Your microphone can change how you sound before the sound ever reaches your
computer. It can even out your volume, quieten the room behind you, and add
clarity and warmth to your voice.

RØDE's own app controls this, but it only runs on Windows and macOS. This tool
does the same job on Linux.

There is one catch with this microphone: it forgets everything when it loses
power. Unplug it, or turn your computer off, and your sound is gone. This tool
fixes that. It saves what you chose and sends it back to the microphone every
time you start your computer or plug the microphone in.

So you set your sound once. After that it just happens.

## What you can change

Four things, which you can use on their own or together:

| | What it does |
|---|---|
| **Compressor** | Evens out your volume. Quiet words come up, loud words come down. |
| **Noise gate** | Turns down background sound while you are not speaking. |
| **Aural exciter** | Adds brightness, so your voice sounds clearer. |
| **Big bottom** | Adds depth, so your voice sounds fuller. |

You change them on a settings page in your browser. It shows a live meter and a
picture of what each control is doing to your voice, so you can hear and see the
difference as you move a slider.

There is a ready-made **Radio Voice** setting if you would rather not start from
scratch. You can save your own, and share them with other people.

This microphone has no volume control, equaliser or de-esser to change. The
four above are everything it can do.

## What you need

- A RØDE NT-USB Mini.
- A computer running Linux.

On Windows and macOS, use [RØDE
Connect](https://rode.com/en/software/rode-connect) instead. It does the same
job and RØDE support it.

## Set it up

**1. Download the program.** Go to the [latest
release](https://github.com/andymason/rode-dsp/releases/latest) and download
`rode-dsp-linux-amd64`. If your computer has an ARM processor, such as a
Raspberry Pi, download `rode-dsp-linux-arm64` instead.

**2. Set it up.** Open a terminal in the folder you downloaded it to, then run
these two lines. You will be asked for your password.

```
chmod +x rode-dsp-linux-amd64
sudo ./rode-dsp-linux-amd64 setup
```

**3. Choose how you want to sound.**

```
rode-dsp gui
```

Your browser opens the settings page. Turn on what you want, move the sliders
until you like what you hear, then close the tab.

That is the whole thing. Your sound comes back by itself from now on.

## Change your sound later

Run `rode-dsp gui` again whenever you want to. Anything you change is saved as
you go, and applies straight away.

## Check it is working

```
rode-dsp status
```

This asks the microphone what it is set to and prints the answer.

## Remove it

```
sudo rode-dsp setup --remove
```

This takes the program off your computer. Your saved sound is kept, in case you
come back. To delete that too, remove the folder `~/.config/rode-dsp`.

## If something is not working

**"Microphone: not found."** Check the microphone is plugged in. If it is,
unplug it and plug it back in — setup only reaches microphones connected after
it ran.

**Your sound is not coming back after a restart.** Check the setup ran
completely: `systemctl status rode-dsp`. If you have not chosen any settings
yet, there is nothing to send, and that is what it will say.

**You want to change the volume.** This microphone has no volume control of its
own. Use your computer's sound settings.

**Your microphone runs old firmware.** Version 2.1.2 and earlier need a
different calculation that this tool does not do. Update the microphone with
RØDE Central, or see Q9 in `docs/re/04-open-questions.md`.

## For advanced users

Everything below is optional.

### Build it yourself

```
go build
sudo ./rode-dsp setup
```

You need a C compiler, and `CGO_ENABLED` must not be `0`, because the USB
library is C. That also means you cannot cross-compile without a cross
toolchain — build for each system on that system. Linux also needs the libudev
headers:

| Distribution | Package |
|---|---|
| Debian/Ubuntu | `apt install libudev-dev` |
| Fedora | `dnf install systemd-devel` |
| Arch | `pacman -S systemd` |
| openSUSE | `zypper install libudev-devel` |

### Commands

```
rode-dsp setup                          set up this computer
rode-dsp gui                            open the settings page
rode-dsp status                         what the microphone is set to now
rode-dsp comp --enable --threshold -20  turn the compressor on
rode-dsp gate --enable --attack 0.8     turn the noise gate on
rode-dsp load                           send your saved settings again
rode-dsp defaults                       put everything back to standard
```

Run `rode-dsp <command> -help` for a command's own options.

### What setup installs

`rode-dsp setup --dry-run` prints every file it would write, with the contents,
and writes nothing. That is the list to work from if you would rather place them
yourself.

In short: the program in `/usr/local/bin`, a rule in `/etc/udev/rules.d` that
lets you reach the microphone without `sudo`, and a service in
`/etc/systemd/system` that sends your settings whenever the microphone appears.
That one trigger covers starting up, plugging in, and waking from sleep, so
there is nothing to enable separately.

The service reads your own settings file. Changing a setting takes effect next
time, with nothing to reinstall. Use `--no-boot` to skip the service, and
`--bin-dir`, `--udev-dir` and `--unit-dir` to install somewhere else.

Distributions without `uaccess` (Alpine, Void, Gentoo/OpenRC) need the access
rule changed: replace `TAG+="uaccess"` with `GROUP="plugdev"` and add yourself
to that group. The startup service needs systemd, so use `--no-boot` there.

### Where your settings live

Nothing is written until you change something. Until then the microphone uses
its own settings.

| | |
|---|---|
| Linux/BSD | `$XDG_CONFIG_HOME/rode-dsp/` (usually `~/.config/rode-dsp/`) |
| Windows | `%AppData%\rode-dsp\` |
| macOS | `~/Library/Application Support/rode-dsp/` |

`config.json` holds your sound and `presets.json` holds your saved ones. Use
`--config <path>` or `$RODE_DSP_CONFIG` to use a different file; `--config`
wins. `rode-dsp load --config <file>` applies a file someone sent you.

The settings page listens on this computer only, and has no password. Anything
that can reach it can change your microphone, which is why nothing else on your
network can.

### Exit codes

These tell a script the difference between a missing microphone and a real
problem, which is how the startup service treats a switched-off microphone as
normal.

| Code | Meaning |
|---|---|
| 0 | Applied |
| 1 | Something failed — bad settings, no permission, a protocol error |
| 2 | The microphone is not connected |

`load --wait N` waits up to N seconds for a microphone that is slow to appear.

### Tests

```
go test ./...
```

`internal/protocol` and `internal/dsp` build without libudev, so the tests that
matter most run anywhere.

## How this was made

RØDE publish no specification for this, so the protocol was worked out by
reading their own application.

The encodings were read out of RØDE Connect's instruction stream rather than
guessed from recordings, and the microphone's seven lookup tables were taken
from the application itself. What this sends is what the official application
sends.

Two limits are worth knowing. Values land on one of the microphone's 256 steps,
so reading a value back can differ from what you set by up to one step. And some
of the maths runs at a different precision here than in the original, which can
differ by about one part in 2.4 million.

`internal/protocol/capture_oracle_test.go` checks all of this against real USB
recordings, so nothing changes by accident.

The full write-up is in `docs/re/`:

| | |
|---|---|
| `00-setup.md` | Ghidra + GhidrAssistMCP, Frida, USB capture |
| `01-addresses.md` | Function and data addresses in `RODE Connect.exe` |
| `02-protocol.md` | The USB HID wire protocol |
| `03-encoders.md` | The exact encoder and decoder formulas |
| `04-open-questions.md` | What is unresolved, and what is settled |
| `05-juce.md` | Using the JUCE source to read the UI and stream code |
| `06-static-extraction.md` | Extracting the lookup tables from the program image |

`tools/frida/` holds the instrumentation scripts.

Three commands exist for that work rather than daily use:

```
rode-dsp probe-effects    which effect IDs actually hold state?
rode-dsp read-raw         probe HID report 0x03
rode-dsp send-raw --i-know-what-this-does <hex>
```

The microphone's state can be read back, so `rode-dsp status` reports what the
device actually holds rather than what was last saved. It stays right even after
another program has changed something. Pass `--config-only` for the saved view.

There is no equaliser, high-pass filter or de-esser on this microphone. RØDE
Connect has panels for all three and hides them; the firmware has no matching
blocks. Q5 in `04-open-questions.md` has the evidence.
