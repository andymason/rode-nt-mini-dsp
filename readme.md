# RØDE NT-USB Mini DSP tool

CLI and web GUI for the RØDE NT-USB Mini's onboard Aphex DSP — compressor, noise
gate, aural exciter and big bottom — over USB HID.

RØDE Connect is Windows and macOS only. This works anywhere Go and hidapi do.

## Build

```
go build
```

Linux needs `libudev-dev` (Debian/Ubuntu: `apt install libudev-dev`).

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

The parameter encodings were reverse engineered from RØDE Connect and verified
against USB captures. Most are byte-exact; some are not yet:

- **Noise gate threshold and range** are out by roughly 1.2 dB and 2.0 dB at the
  bottom of their ranges.
- **Compressor threshold, attack, release and gain** interpolate between
  observed values rather than indexing the device's real lookup table, so
  mid-range values are approximations.
- **Noise gate attack, hold and release** use a reciprocal-linear approximation
  of the device's bilinear transform, exact only at the endpoints.

Endpoints are correct throughout. `docs/re/04-open-questions.md` tracks each
item, and `internal/protocol/capture_oracle_test.go` pins the current behaviour
against the captures so any change is deliberate.

## Reverse-engineering notes

`docs/re/` documents the protocol and the tooling used to work it out:

| | |
|---|---|
| `00-setup.md` | Ghidra + GhidrAssistMCP, Frida, USB capture |
| `01-addresses.md` | function and data addresses in `RODE Connect.exe` |
| `02-protocol.md` | the USB HID DSP protocol |
| `03-luts.md` | lookup tables: provenance and what's wrong with them |
| `04-open-questions.md` | what is still unresolved |
| `05-juce.md` | using the JUCE source to read the UI code |
| `06-static-extraction.md` | what the PE image gave up without Ghidra |
| `07-workplan.md` | what is left, and the order to do it in |

`tools/frida/` holds instrumentation scripts for the RØDE Connect binary.

Three commands exist for protocol work rather than daily use:

```
rode-dsp probe-effects    # which effect IDs does the firmware acknowledge?
rode-dsp read-raw         # probe HID report 0x03
rode-dsp send-raw --i-know-what-this-does <hex>
```

## Tests

```
go test ./...
```

`internal/protocol` and `internal/dsp` build without libudev, so the encoder and
registry tests run anywhere.
