# Exact encoder and decoder formulas

Recovered by static analysis of RØDE Connect and confirmed against USB captures
and Frida logs (see [`README.md`](README.md)), not fitted to captures. This is
the reference for what `internal/protocol/encode.go` and `decode.go` implement.
Everything below is described as mathematics; none of it is RØDE's code.

## The two scale factors, and the firmware gate

A check in the application decides how a normalised float becomes an integer.
It depends on the **firmware version**, not only the device model: the NT-USB
Mini (PID `0x15`) gets the Q31 scale on firmware above 2.1.2 and the Q16 scale
on older firmware.

| firmware | scale | conversion |
|---|---|---|
| newer firmware | `2147483648.0` (Q31) | multiply, truncate, then **saturate** |
| older firmware | `65536.0` (Q16) | multiply, truncate, no saturation |

Saturation is the standard sign-disagreement idiom: if the float was positive
but the converted integer is not, the conversion overflowed and the result becomes
`0x7FFFFFFF` (or `0x80000000` when the float was negative). This is why noise
gate threshold at 0 dB sends `0x7FFFFFFF` — `10^0 × 2^31` overflows int32.

Every capture here shows Q31. `rode-dsp` hardcodes it; see Q9 in
[`open-questions.md`](open-questions.md).

## Index conversion

Every table index is computed the same way: scale to `0…255`, **truncate**
(never round), then **mask to 8 bits** with no clamp, so an out-of-range value
wraps. `lutIndex` in `encode.go` reproduces
this; `rode-dsp` clamps its own inputs to the UI range first, which makes the
mask a no-op.

## Compressor

The application has two conversion paths, chosen by device type. Mode 1 writes
seven direct Q16 coefficients; Mode 2 is the lookup-table path the NT-USB Mini
takes.

| param | index expression | inverse | table |
|---|---|---|---|
| `0x00` enable | — raw byte | — | — |
| `0x01` threshold | `(1.0 − (dB + 60.0)/60.0) × 255` | `(1 − idx/255) × 60 − 60` | threshold |
| `0x02` ratio | `((r − 1.5)/3.0) × 255` | `idx/255 × 3 + 1.5` | **none — the index *is* the payload** |
| `0x03` attack | `log(ms/0.1) / log(100.0) × 255` | `0.1 × 100^(idx/255)` | attack |
| `0x04` release | `log(ms/5.0) / log(40.0) × 255` | `5.0 × 40^(idx/255)` | release |
| `0x05` gain | `((dB − 0.0)/9.0) × 255` | `idx/255 × 9` | gain |

UI ranges: threshold −60…0 dB, ratio 1.5…4.5, attack 0.1…10 ms, release
5…200 ms, gain 0…9 dB.

The logarithm's base is irrelevant: it appears once in the numerator and once
in the denominator.

The application recovers the index by binary search over the same table, then
applies the inverse column above. `decode.go` uses a nearest-entry scan instead,
which agrees on every value the device can return and degrades more sensibly on
one that is not a table entry.

## Noise gate

Six parameters, no lookup tables. Each result is scaled by Q31 or Q16 per the
firmware gate, then truncated.

| param | unit in | value before scaling |
|---|---|---|
| `0x00` enable | — | raw byte |
| `0x01` threshold | dB | `10^(dB / 20)` |
| `0x02` attack | **ms** | one-pole transform, below |
| `0x03` hold | **seconds** | `1 / (s × 48000)` |
| `0x04` release | **seconds** | `1 / (s × 48000)` |
| `0x05` range | dB | `10^(dB / 20)` |
| `0x06` hysteresis | **fraction 0…1** | `10^((−1.0 − 7.0 × h) / 20)` |

The unit inconsistency is in the application, not a transcription error: attack
arrives in milliseconds and is divided by 1000 inside the transform, while hold
and release arrive already in seconds. The Go API takes milliseconds throughout.

Hysteresis maps a 0…1 fraction onto −1.0…−8.0 dB.

### The attack coefficient

The inner function was identified as `cos` from its constants: the `π/4`
reduction threshold, `2/π`, and the `1 − x²/2` small-angle path.

```
w   = 5.0 / ((ms / 1000.0) × 48000.0)
c   = cos(w)
out = sqrt(c² − 4c + 3) + c − 1
```

Substituting `b = 2 − c` gives `b² − 1 = c² − 4c + 3` exactly, so

```
out = 1 − (b − sqrt(b² − 1))
```

which is the textbook one-pole time-constant design: `b − sqrt(b² − 1)` is the
pole and the transmitted coefficient is `1 −` that. The algebra matching a
standard form exactly is what confirms `cos` rather than another function with
the same `1 − x²/2` expansion.

The reader inverts it as `w = acos((3 − b²) / (4 − 2b))` with `b = coef + 1`,
then `ms = 5000 / (w × 48000)`.

Hold and release do **not** use this transform — they are the bare reciprocal
`1 / (s × 48000)`, the `w → 0` limit up to the factor of 5.

## Aural Exciter

| param | index expression | table |
|---|---|---|
| `0x00` enable | — raw byte | — |
| `0x01` harmonics | `pct / 100 × 255` | harmonics/drive |
| `0x02` tune | `(Hz − 600) / 4400 × 255` | two tables |
| `0x03` | never SET; reads `0x1f` | — see Q6 in [`open-questions.md`](open-questions.md) |

Both parameters send the coefficient followed by the raw index byte, so decoding
needs no table search — the index is in the reply. Tune sends two coefficients,
putting its index byte at data offset 8 rather than 4.

## Big Bottom

| param | index expression | table |
|---|---|---|
| `0x00` enable | — raw byte | — |
| `0x01` drive | `pct / 100 × 255` | harmonics/drive — **shared with AE harmonics** |
| `0x02` tune | `(Hz − 60) / 252 × 255` | **none — the index *is* the payload** |

## Fidelity limits in the Go implementation

- The application evaluates `powf`, `logf` and `cos` in float32; Go evaluates them in
  float64 and rounds once at the end. That can differ by a single LSB — noise
  gate threshold at −58.8 dB is one such case, one part in 2.4 million on a
  31-bit coefficient. Matching bit-for-bit would mean reimplementing the MSVC
  float32 runtime.
- The final scaling is done in float32 to match the application; float64 would disagree
  by an LSB near index boundaries.
- Decoding is quantised to the device's 256 steps, so a value read back can
  differ from the value written by up to one step. That is the device's
  resolution, not an approximation added by `decode.go`.
