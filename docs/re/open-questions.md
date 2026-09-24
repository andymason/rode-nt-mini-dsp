# Open questions

What is still unknown about the protocol, and what answering it would give.

## Closed

One line each, so that a stale claim elsewhere can be traced to its answer.

| # | Question | Answer |
|---|---|---|
| Q1 | Noise gate threshold and range look 1.2–2.0 dB out | The encoder is correct: `trunc(10^(dB/20) × 2^31)`, saturating. The capture sweeps never reached the bottom of the slider. Every minimum-end endpoint falls about 2 % short, and every maximum-end endpoint is exact. |
| Q2 | Does the index truncate or round? | Truncate. Confirmed on hardware: 92.5 % harmonics reads back as index 235, and `92.5/100 × 255` = 235.875. |
| Q3 | Where does compressor gain's 9.0 come from? | It is a float constant in the application that the first sweep for constants missed. The range is 0…9 dB. |
| Q4 | Clamping order | There is no clamping; an out-of-range index wraps. `rode-dsp` clamps its own inputs instead, and the sliders make the difference unobservable. |
| Q5 | Do effects `0x04` and above exist in the firmware? | No. See below. |

### Q5: there is no equaliser, high-pass filter or de-esser

This is the question the hidden panels in RØDE Connect invite people to ask
again, so the evidence is kept here in full.

RØDE Connect contains equaliser, high-pass and de-esser panels, but never shows
them for this microphone. The firmware has no matching DSP blocks. Four
independent findings:

1. **Storage.** Effect IDs `0x04`–`0x0b` acknowledge every packet, but the
   firmware acknowledges any ID, so an acknowledgement sweep proves nothing.
   Reading these IDs returns all zeros, and a SET followed by a GET still
   returns zeros. The same test on effect `0x02` reads back the written bytes
   exactly.
2. **Addressing.** The effect ID is `channel × 4 + index`, with index
   `0x00`–`0x03`, in every encoder and reader. The structure allows four blocks
   per channel.
3. **No encoder exists.** Only the four effect encoders and their four readers
   call the packet builder. Nothing in the application encodes an equaliser,
   high-pass filter or de-esser for any device.
4. **The hidden handlers cannot send.** Each of the three panel handlers only
   updates its slider, using JUCE's `dontSendNotification`, so no listener
   hears the change and nothing reaches the device.

`rode-dsp probe-effects --write-probe` reruns the hardware checks. It is worth
running on other RØDE hardware or after a firmware update.

## Q6: Aural Exciter and Big Bottom parameter `0x03`

**Open. Cosmetic; nothing depends on it.**

Aural Exciter and Big Bottom both answer a read of parameter `0x03` with
`0x1f` (31). They do so even when both effects are disabled and every other
parameter differs:

```
$ rode-dsp send-raw --i-know-what-this-does --debug 04 02 03 03
RECV: 03 02 41 1f 00 00 00 …
```

RØDE Connect reads the Aural Exciter's value at startup but never sets it. The
Big Bottom's bulk-write form carries a fourth field, which is also never set on
this firmware. A constant shared by two effects looks more like a capability or
size field than a coefficient: a coefficient count, a version or a filter
order. `TestParamCountsCoverRegistry` pins the current counts, so a change will
show up.

## Q7: compressor Mode 1

**Open. Matters only if this tool supports another device.**

The compressor encoder has a second path that writes seven direct Q16
coefficients instead of table values. Which RØDE devices use it, and what it
computes, is unknown.

## Q8: the two tables for Aural Exciter tune

**Open. Cosmetic; the encoding is verified either way.**

Tune sends two coefficients from two tables at the same index. The second table
stays at `0x24000000` for indices 0–53 and then rises. That suggests two
cascaded filter stages, but a numerator/denominator pair or a low-pass/high-pass
pair would also fit.

## Q9: firmware 2.1.2 and older

**Open. Affects only microphones on old firmware.**

Firmware 2.1.2 and older use the Q16 scale rather than Q31 (see
[`encoders.md`](encoders.md)). `rode-dsp` does not read the firmware version and
always uses Q31. Supporting old firmware would take a one-line change in
`encode.go`, plus passing the firmware version through from the device.

## Outstanding

`TestKnownDeviationsFromCapture` pins the noise gate endpoint values from the
February 2026 captures. Q1 found those to be sweep artefacts, so the test
currently pins a measurement error. A new capture that parks each slider exactly
at its minimum would settle it.
