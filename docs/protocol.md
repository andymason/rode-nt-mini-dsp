# NT-USB Mini DSP protocol

RØDE publish no specification for the NT-USB Mini's on-board processing, and
their control application, RØDE Connect, runs only on Windows and macOS. This
document records the protocol as it was recovered, for interoperability, so that
`rode-dsp` could drive the microphone from Linux.

Nothing from RØDE Connect is in this repository: no binary, and no decompiled or
disassembled code. What is here is the method, the protocol facts it produced,
and the scripts written along the way. The formulas below are written as
mathematics; none of it is RØDE's code.

- [Method](#method)
- [Wire format](#wire-format)
- [Encoders and decoders](#encoders-and-decoders)
- [No equaliser, high-pass filter or de-esser](#no-equaliser-high-pass-filter-or-de-esser)
- [Unresolved](#unresolved)

## Method

### Toolchain

| Job | Tool |
|---|---|
| Static analysis of the Windows application | [Ghidra](https://ghidra-sre.org/) 11.4 |
| Giving an LLM agent access to Ghidra | [GhidrAssistMCP](https://github.com/symgraph/GhidrAssistMCP), an MCP server running inside Ghidra |
| Agents | Claude Code and DeepSeek |
| Recovering class layouts and vtables | the public [JUCE](https://github.com/juce-framework/JUCE) source, version 7.0.9 |
| Capturing USB traffic | Wireshark with USBPcap on Windows; `tshark` for scripted analysis |
| Checking formulas against the running application | [Frida](https://frida.re/), scripts in [`tools/frida/`](../tools/frida/) |
| Reading and writing the device directly | `rode-dsp status`, `probe-effects`, `read-raw`, `send-raw` |

All capture, debugging and testing was done on Windows, where RØDE Connect runs.
Ghidra showed *why* the application sends what it sends; USB captures, Frida and
the microphone itself showed *what* is actually sent. No formula was accepted
until two of these agreed.

### USB captures

USBPcap recorded RØDE Connect while each slider was swept through its range,
with every effect enabled and then disabled. DSP commands are HID `SET_REPORT`
control transfers:

```
usb.bmRequestType == 0x21 && usb.setup.bRequest == 0x09
```

This pairs each outgoing packet with the device's reply:

```
tshark -r capture.pcapng -T fields \
  -e frame.number -e usb.endpoint_address.direction \
  -e usb.data_fragment -e usb.capdata \
  | awk -F'\t' '$3 != "" || $4 != ""'
```

The captures gave the packet layout, the effect and parameter IDs, and several
hundred `(UI value, payload)` pairs. Those pairs are
`internal/protocol/testdata/baseline_v1.csv`, and
`internal/protocol/capture_oracle_test.go` tests the encoders against them.

### Ghidra over MCP

Captures give sample points, not formulas. GhidrAssistMCP exposed Ghidra's
analysis to the agents as MCP tools (function search, cross-references,
decompilation, data reads):

```json
{
  "mcpServers": {
    "ghidra": { "type": "http", "url": "http://127.0.0.1:8080/mcp" }
  }
}
```

It runs inside Ghidra with no bridge process, exposes about 50 tools, and ships
with script execution and program export disabled. The server bound to
`127.0.0.1` only, behind an inbound firewall rule, since it has no
authentication; script execution, file import and program export stayed
disabled throughout.

To keep the context window manageable, function listings were filtered and
paginated, decompilation was requested only for small functions (large UI
constructors were reached through cross-references), and decompile requests
were batched through the server's asynchronous queue.

Searching for float constants known from the captures (`255.0`, `48000.0`,
`-60.0`) led to the four per-effect encoder functions. Cross-references led on
to the packet builder, the matching readers, and a firmware-version check that
chooses between two fixed-point scales. The executable had no symbols, but its
RTTI survived: Ghidra recovered 337 JUCE class names.

### JUCE source

RØDE Connect statically links JUCE 7.0.9 (the binary contains `"JUCE v7.0.9"`).
The public headers gave the virtual-method order, and from that the vtable
slots:

| Class | Slot | Method | Used for |
|---|---|---|---|
| `juce::Component` | `0x58` | `setVisible` | following panel visibility logic |
| `juce::InputStream` | 3 | `read` | parsing device replies |
| `juce::InputStream` | 8 | `readInt` | little-endian coefficients in replies |

### Lookup tables

Four of the fifteen parameters index a 256-entry table of 32-bit coefficients.
The captures covered only 40–91 entries of each; the complete tables were found
in the application's read-only data:

- Splitting the candidate region into monotonic runs gave exactly seven runs of
  256 words.
- Each capture set matched exactly one run, 100 % of its values, with no other
  run above 2 %.
- Reading the same tables from the live process with Frida agreed byte for byte.

The tables are in `internal/protocol/luts/`. They are the only data taken from
the application, and they are what the microphone needs to be sent.

### Frida

Frida hooked the four encoder functions and the packet builder, logging each UI
value next to the packet it produced. That gave exact `(input, output)` pairs
over the full range of every slider, and showed that indexes are truncated, not
rounded, and that nothing is clamped.

- Frida 17 removed the static `Module` lookups. `rode.js` tries the current API,
  then the old one, then a scan of the loaded modules.
- The Windows x64 ABI passes floats in XMM registers, which Frida's CPU context
  did not expose until 17.16. The hooks wrap each function in a typed
  `NativeFunction` and let Frida marshal the arguments.

```
frida -n "RODE Connect.exe" -l tools/frida/rode.js -l tools/frida/log-encoders.js
python tools/frida/host.py log-encoders
```

The addresses in `rode.js` are specific to one build of RØDE Connect, SHA-256
`c3015816eacc3360d5b935a08fd05f4e948105604a2f36b812b9d58262a1023c`.

## Wire format

### Transport

| Field | Value |
|---|---|
| VID / PID | `0x19F7` / `0x0015` |
| bcdDevice | `0x0236` |
| Interface | HID interface 3, vendor-defined (usage page `0xFF00`) |
| Transfer | SET_REPORT control — `bmRequestType=0x21`, `bRequest=0x09`, `wValue=0x0204` |
| Packet size | 29 bytes (1 report ID + 28 payload) |
| ACK | single byte `0x41` (`'A'`), 500 ms timeout in RØDE Connect |

The device enumerates four interfaces: audio control, audio streaming in, audio
streaming out, and HID. Audio is 48 kHz / 24-bit, mono in / stereo out (USB
Audio Class 1.0).

The 78-byte HID report descriptor declares eight vendor-defined reports in four
IN/OUT pairs:

| Report | Direction | Data | Purpose |
|---|---|---|---|
| `0x01` | IN | 9 | unknown |
| `0x02` | OUT | 9 | unknown |
| **`0x03`** | **IN** | **28** | **DSP replies — ACK and read data** |
| **`0x04`** | **OUT** | **28** | **DSP commands** |
| `0x05` | IN | 14 | unknown |
| `0x06` | OUT | 14 | unknown |
| `0x07` | IN | 27 | unknown |
| `0x08` | OUT | 27 | unknown |

### Requests — report `0x04`

```
byte 0    0x04         report ID
byte 1    effect ID
byte 2    command
byte 3    parameter ID
byte 4+   value        encoding varies by parameter
```

Effect IDs are `channel × 4 + index`, index `0x00`–`0x03`. The NT-USB Mini is
single-channel:

| ID | Effect |
|---|---|
| `0x00` | Compressor |
| `0x01` | Noise Gate |
| `0x02` | Aural Exciter |
| `0x03` | Big Bottom |

Commands come in two symmetric pairs:

| Value | Command | Notes |
|---|---|---|
| `0x00` | SET all | every parameter of an effect in one packet |
| `0x01` | GET all | every parameter of an effect in one reply |
| `0x02` | SET one | the form RØDE Connect uses for this microphone |
| `0x03` | GET one | the form RØDE Connect uses for this microphone |

A firmware version check decides which pair RØDE Connect uses; the device
answers both (`rode-dsp send-raw 04 00 01` returns the whole compressor in one
reply). A GET's value field is unused and sent as zeros, so a captured GET looks
like a "clear" packet, but the device answers it with the live coefficient.

| Value type | Width | Layout |
|---|---|---|
| flag | 1 byte @ 4 | `0x00` off, `0x01` on |
| index | 1 byte @ 4 | direct linear map to the UI range |
| LUT | 4 bytes LE @ 4:8 | 32-bit value from a lookup table |
| LUT + index | 5 bytes @ 4:9 | 4-byte LUT value then a 1-byte index |
| dual LUT + index | 9 bytes @ 4:13 | two 4-byte LUT values then a 1-byte index |
| Q31 | 4 bytes LE @ 4:8 | signed 32-bit, value × 2³¹, saturating |

### Replies — report `0x03`

```
byte 0    0x03         report ID
byte 1    effect ID    echoed from the request
byte 2    0x41 'A'     ACK
byte 3+   data         same field layout as the SET payload
```

The 26 bytes from offset 3 are read field by field, in order; multi-byte fields
are little-endian. The device ACKs every effect and parameter ID, including ones
no firmware block backs, so an ACK proves receipt only. What discriminates is
whether a read returns data. `internal/protocol/decode.go` implements this.

### Startup sequence

RØDE Connect reads, then writes back:

1. **20 GET packets** in effect order: Compressor 6 (`0x00`–`0x05`), Noise Gate
   7 (`0x00`–`0x06`), Aural Exciter 4 (`0x00`–`0x03`), Big Bottom 3
   (`0x00`–`0x02`).
2. **SET packets** only for parameters whose UI value differs from what was just
   read, so the SET burst varies in length.

The device does not require the read sweep, and `rode-dsp` skips it when
applying settings. If applying settings ever misbehaves on a fresh plug-in, that
is the first thing to restore.

### Parameters

| ID | Compressor | Noise Gate | Aural Exciter | Big Bottom |
|---|---|---|---|---|
| `0x00` | Enable | Enable | Enable | Enable |
| `0x01` | Threshold −60…0 dB | Threshold −60…0 dB | Harmonics 0…100 % | Drive 0…100 % |
| `0x02` | Ratio 1.5…4.5:1 | Attack 0.1…1000 ms | Tune 600…5000 Hz | Tune 60…312 Hz |
| `0x03` | Attack 0.1…10 ms | Hold 50…2000 ms | reads `0x1f`, never SET | reads `0x1f`, not read by RØDE Connect |
| `0x04` | Release 5…200 ms | Release 50…2000 ms | | |
| `0x05` | Gain 0…9 dB | Range −100…0 dB | | |
| `0x06` | | Hysteresis 0…100 % | | |

## Encoders and decoders

`internal/protocol/encode.go` and `decode.go` implement what follows.

### Scale factor and firmware

A check in the application decides how a normalised float becomes an integer.
The NT-USB Mini gets Q31 on firmware above 2.1.2 and Q16 on older firmware:

| Firmware | Scale | Conversion |
|---|---|---|
| above 2.1.2 | `2147483648.0` (Q31) | multiply, truncate, then saturate |
| 2.1.2 and older | `65536.0` (Q16) | multiply, truncate, no saturation |

Saturation: if the float was positive but the converted integer is not, the
result becomes `0x7FFFFFFF` (or `0x80000000` for a negative float). This is why
noise gate threshold at 0 dB sends `0x7FFFFFFF`.

Every capture shows Q31, and `rode-dsp` hardcodes it (see
[Unresolved](#firmware-212-and-older)).

### Index conversion

Every table index is scaled to `0…255`, **truncated** (never rounded), then
masked to 8 bits with no clamp, so an out-of-range value wraps. Truncation was
confirmed on hardware: 92.5 % harmonics (`92.5/100 × 255` = 235.875) reads back
as index 235. `rode-dsp` clamps its own inputs to the UI range, which makes the
mask a no-op.

### Compressor

The application has two paths, chosen by device type; the NT-USB Mini takes the
lookup-table path.

| Param | Index expression | Inverse | Table |
|---|---|---|---|
| `0x00` enable | raw byte | — | — |
| `0x01` threshold | `(1.0 − (dB + 60.0)/60.0) × 255` | `(1 − idx/255) × 60 − 60` | threshold |
| `0x02` ratio | `((r − 1.5)/3.0) × 255` | `idx/255 × 3 + 1.5` | none — the index is the payload |
| `0x03` attack | `log(ms/0.1) / log(100.0) × 255` | `0.1 × 100^(idx/255)` | attack |
| `0x04` release | `log(ms/5.0) / log(40.0) × 255` | `5.0 × 40^(idx/255)` | release |
| `0x05` gain | `(dB/9.0) × 255` | `idx/255 × 9` | gain |

The application recovers the index by binary search over the table, then applies
the inverse. `decode.go` uses a nearest-entry scan, which agrees on every value
the device can return.

### Noise gate

No lookup tables. Each result is scaled by Q31 and truncated.

| Param | Unit in | Value before scaling |
|---|---|---|
| `0x00` enable | — | raw byte |
| `0x01` threshold | dB | `10^(dB / 20)` |
| `0x02` attack | ms | one-pole transform, below |
| `0x03` hold | seconds | `1 / (s × 48000)` |
| `0x04` release | seconds | `1 / (s × 48000)` |
| `0x05` range | dB | `10^(dB / 20)` |
| `0x06` hysteresis | fraction 0…1 | `10^((−1.0 − 7.0 × h) / 20)` |

The unit inconsistency is the application's: attack arrives in milliseconds,
hold and release in seconds. The Go API takes milliseconds throughout.
Hysteresis maps 0…1 onto −1.0…−8.0 dB.

The attack coefficient:

```
w   = 5.0 / ((ms / 1000.0) × 48000.0)
c   = cos(w)
out = sqrt(c² − 4c + 3) + c − 1
```

With `b = 2 − c`, this is `out = 1 − (b − sqrt(b² − 1))`, the textbook one-pole
time-constant design; that exact match confirms the inner function is `cos`. The
reader inverts it as `w = acos((3 − b²) / (4 − 2b))` with `b = coef + 1`, then
`ms = 5000 / (w × 48000)`.

### Aural Exciter

| Param | Index expression | Table |
|---|---|---|
| `0x00` enable | raw byte | — |
| `0x01` harmonics | `pct / 100 × 255` | harmonics/drive |
| `0x02` tune | `(Hz − 600) / 4400 × 255` | two tables, same index |
| `0x03` | never SET; reads `0x1f` | — |

Both parameters send the coefficient followed by the raw index byte, so decoding
needs no table search. Tune sends two coefficients, putting its index byte at
data offset 8.

### Big Bottom

| Param | Index expression | Table |
|---|---|---|
| `0x00` enable | raw byte | — |
| `0x01` drive | `pct / 100 × 255` | harmonics/drive, shared with Aural Exciter harmonics |
| `0x02` tune | `(Hz − 60) / 252 × 255` | none — the index is the payload |

### Fidelity limits

- The application evaluates `powf`, `logf` and `cos` in float32; Go uses float64
  and rounds once at the end. This can differ by one LSB (for example noise gate
  threshold at −58.8 dB: one part in 2.4 million).
- The final scaling is done in float32 to match the application.
- Values quantise to the device's 256 steps, so a value read back can differ
  from the one written by up to one step.
- Captures labelled with a slider's minimum were taken slightly inside it,
  because the sweeps never reached the end stop (the noise gate ones fall about
  2 % short). The encoders are correct; `TestCaptureLabelsAreApproximate`
  asserts the implied inputs.

## No equaliser, high-pass filter or de-esser

RØDE Connect contains equaliser, high-pass and de-esser panels but never shows
them for this microphone. The firmware has no matching DSP blocks:

1. **Storage.** Effect IDs `0x04`–`0x0b` acknowledge packets, but reading them
   returns zeros, and a SET followed by a GET still returns zeros. The same test
   on effect `0x02` reads back the written bytes.
2. **Addressing.** The effect ID is `channel × 4 + index` with index
   `0x00`–`0x03` in every encoder and reader: four blocks per channel.
3. **No encoder exists.** Only the four effect encoders and their readers call
   the packet builder.
4. **The hidden handlers cannot send.** Each panel handler updates its slider
   with JUCE's `dontSendNotification` (defined as `0`), so no listener hears the
   change.

`rode-dsp probe-effects --write-probe` reruns the hardware checks, for other
RØDE hardware or a firmware update.

## Unresolved

None of these affect the NT-USB Mini on current firmware.

### Parameter `0x03` on Aural Exciter and Big Bottom

Both answer a read of parameter `0x03` with `0x1f`, even when disabled:

```
$ rode-dsp send-raw --i-know-what-this-does --debug 04 02 03 03
RECV: 03 02 41 1f 00 00 00 …
```

RØDE Connect reads the Aural Exciter's value but never sets it. A constant shared
by two effects looks like a capability or size field (coefficient count,
version, filter order) rather than a coefficient.

### Aural Exciter tune's two tables

Tune sends two coefficients from two tables at the same index. The second stays
at `0x24000000` for indices 0–53 and then rises, suggesting two cascaded filter
stages, though a numerator/denominator or low-pass/high-pass pair would also
fit. The encoding is verified either way.

### Compressor Mode 1

The compressor encoder has a second path writing seven direct Q16 coefficients.
Which RØDE devices use it, and what it computes, is unknown.

### Firmware 2.1.2 and older

These use Q16 rather than Q31. `rode-dsp` does not read the firmware version and
always uses Q31; support would need the version passed through from the device
and a one-line change in `encode.go`.
