# USB DSP protocol — RØDE NT-USB Mini

Established from USB packet captures, static analysis of RØDE Connect, and
readback against real hardware; see [`README.md`](README.md) for the method. All
four effects are confirmed end to end.

Encoding formulas are in [`encoders.md`](encoders.md); this file is the wire
format.

## Transport

| Field | Value |
|---|---|
| VID / PID | `0x19F7` / `0x0015` |
| bcdDevice | `0x0236` |
| Interface | HID interface 3, vendor-defined (usage page `0xFF00`) |
| Transfer | SET_REPORT control — `bmRequestType=0x21`, `bRequest=0x09`, `wValue=0x0204` |
| Packet size | 29 bytes (1 report ID + 28 payload) |
| ACK | single byte `0x41` (`'A'`), 500 ms timeout in RØDE Connect |

The device enumerates four interfaces: audio control, audio streaming in, audio
streaming out, and HID. Audio is 48 kHz / 24-bit, mono in / stereo out
(USB Audio Class 1.0).

### HID report descriptor

78 bytes, eight reports in four IN/OUT pairs, all vendor-defined:

| Report | Direction | Data | Purpose |
|---|---|---|---|
| `0x01` | IN | 9 | unknown (status?) |
| `0x02` | OUT | 9 | unknown |
| **`0x03`** | **IN** | **28** | **DSP replies — ACK and read data** |
| **`0x04`** | **OUT** | **28** | **DSP commands** |
| `0x05` | IN | 14 | unknown |
| `0x06` | OUT | 14 | unknown |
| `0x07` | IN | 27 | unknown |
| `0x08` | OUT | 27 | unknown |

## Requests — report `0x04`

```
byte 0    0x04         report ID
byte 1    effect ID
byte 2    command
byte 3    parameter ID
byte 4+   value        encoding varies by parameter
```

### Effect IDs

`channel × 4 + index`, index `0x00`–`0x03`, computed the same way by every
encoder and reader in RØDE Connect. The NT-USB Mini is single-channel, so its
IDs are:

| ID | Effect |
|---|---|
| `0x00` | Compressor |
| `0x01` | Noise Gate |
| `0x02` | Aural Exciter |
| `0x03` | Big Bottom |

Four blocks per channel is a hard structural limit. IDs `0x04`+ acknowledge
packets but hold nothing — see Q5 in [`open-questions.md`](open-questions.md).

### Commands

Four commands in two symmetric pairs:

| Value | Command | Notes |
|---|---|---|
| `0x00` | SET all | every parameter of an effect in one packet |
| `0x01` | GET all | every parameter of an effect in one reply |
| `0x02` | SET one | the form this microphone uses |
| `0x03` | GET one | the form this microphone uses |

Which pair RØDE Connect uses is decided by a firmware version check. The
device answers the bulk forms regardless —
`rode-dsp send-raw 04 00 01` returns the whole compressor in one reply.

A GET's value field is unused and sent as zeros, which is why a capture of one
looks like an "init" or "clear" packet. It is not: the device answers with the
live coefficient. Anything describing command `0x03` as INIT/CLEAR predates
August 2026 and is wrong.

### Value encodings

| Type | Width | Layout |
|---|---|---|
| flag | 1 byte @ 4 | `0x00` off, `0x01` on |
| index | 1 byte @ 4 | direct linear map to the UI range |
| LUT | 4 bytes LE @ 4:8 | 32-bit value from a lookup table |
| LUT + index | 5 bytes @ 4:9 | 4-byte LUT value then a 1-byte index |
| dual LUT + index | 9 bytes @ 4:13 | two 4-byte LUT values then a 1-byte index |
| Q31 | 4 bytes LE @ 4:8 | signed 32-bit, value × 2³¹, saturating |

## Replies — report `0x03`

```
byte 0    0x03         report ID
byte 1    effect ID    echoed from the request
byte 2    0x41 'A'     ACK
byte 3+   data         same field layout as the SET payload
```

The 26 bytes from offset 3 are read field by field, in order. Multi-byte fields
are little-endian.

The device ACKs **every** effect and parameter ID it is sent, including ones no
firmware block backs. An ACK proves receipt, nothing more; what discriminates is
whether a read returns data.

`internal/protocol/decode.go` implements this and `rode-dsp status` shows the
result.

## Startup sequence — read, then write back

1. **20 GET packets (`0x03`)** in effect order: Comp 6 (`0x00`–`0x05`), Gate 7
   (`0x00`–`0x06`), AE 4 (`0x00`–`0x03`), BB 3 (`0x00`–`0x02`). These counts are
   exactly what the four reader functions iterate to. The device answers each
   with its live coefficient.
2. **SET packets (`0x02`)** for the parameters whose UI value differs from what
   was just read. Each encoder diffs against the state cached during the read
   and skips anything unchanged, which is why the SET burst is shorter than 20
   and varies in length between captures.

In one capture with every effect enabled, frame 35 requests compressor param `0x01`, frame 37 answers `03 00 41 00 37 07 00`, and
frame 127 writes the identical bytes back.

This also explains the old observation that the all-disabled and all-enabled
captures differ by exactly four packets: those four are the enable packets, and
every coefficient already matched what the device held.

## Parameters

Ranges and index formulas are in [`encoders.md`](encoders.md). Parameter IDs:

| ID | Compressor | Noise Gate | Aural Exciter | Big Bottom |
|---|---|---|---|---|
| `0x00` | Enable | Enable | Enable | Enable |
| `0x01` | Threshold −60…0 dB | Threshold −60…0 dB | Harmonics 0…100 % | Drive 0…100 % |
| `0x02` | Ratio 1.5…4.5:1 | Attack 0.1…1000 ms | Tune 600…5000 Hz | Tune 60…312 Hz |
| `0x03` | Attack 0.1…10 ms | Hold 50…2000 ms | — reads `0x1f`, never SET | — reads `0x1f`, not read by RØDE Connect |
| `0x04` | Release 5…200 ms | Release 50…2000 ms | | |
| `0x05` | Gain 0…9 dB | Range −100…0 dB | | |
| `0x06` | | Hysteresis 0…100 % | | |

Aural Exciter harmonics and Big Bottom drive share one lookup table,
confirmed by cross-checking every overlapping index between the
two sweeps. Aural Exciter tune indexes two tables with the same index and sends
both coefficients — see Q8 in [`open-questions.md`](open-questions.md).
