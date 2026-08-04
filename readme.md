# RØDE NT-USB Mini DSP tool

CLI and web GUI for the RØDE NT-USB Mini's onboard Aphex DSP — compressor, noise
gate, aural exciter and big bottom — over USB HID.

RØDE Connect is Windows and macOS only. This works anywhere Go and hidapi do.

## Build

```
go build
```

cgo is required (hidapi is C), so a C toolchain must be present and
`CGO_ENABLED` must not be 0. That also means no cross-compiling without a
cross toolchain — build each platform on that platform.

Linux additionally needs `libudev-dev` (Debian/Ubuntu:
`apt install libudev-dev`), and non-root access to the microphone needs a udev
rule. Write `/etc/udev/rules.d/70-rode-nt-usb-mini.rules`:

```
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="19f7", ATTRS{idProduct}=="0015", MODE="0660", TAG+="uaccess"
```

then `sudo udevadm control --reload-rules && sudo udevadm trigger`, and
replug the microphone. Without it `rode-dsp` fails to open the device unless
run as root.

## Use

```
rode-dsp status                         # connection and current values
rode-dsp comp --enable --threshold -20
rode-dsp gate --enable --attack 0.8 --hold 80
rode-dsp load                           # apply rode_dsp_config.json
rode-dsp defaults                       # reset everything
rode-dsp gui                            # web UI on localhost
```

Settings live in `rode_dsp_config.json` (or `~/.rode-dsp/config.json`). Exit
code 2 means the microphone is not connected, so boot scripts can tell that
apart from a real failure:

```sh
if rode-dsp load --quiet; then :
elif [ $? -eq 2 ]; then echo "mic not connected, skipping"
fi
```

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
