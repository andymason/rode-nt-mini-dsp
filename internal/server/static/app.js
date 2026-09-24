// RØDE NT-USB Mini — DSP control.
//
// The interface is generated from /api/schema, which the Go server renders from
// the parameter registry in internal/dsp/params.go. Nothing about ranges,
// defaults, units or encodings is written down here; a parameter added on the
// Go side appears in the browser with no change to this file.
//
// Two display modes. Simple shows only what changes the sound. Advanced adds the
// table index, the payload bytes, the full 29-byte report and the index formula
// for each parameter — all computed server-side by the same functions that drive
// the device, so what is shown is what was sent.
//
// DSP parameters are readable: command 0x03 asks the device for one and it
// answers with the live coefficient. The server's state is what it last sent,
// which agrees with the device to within its 256-step quantisation unless
// something else has driven the microphone since.

"use strict";

const WS_URL = `ws://${location.host}/ws`;
const PREVIEW_INTERVAL_MS = 40;
const LIVE_REDRAW_MS = 50;

/* ------------------------------------------------------------------ helpers */

const el = (tag, cls, text) => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
};

const SVG_NS = "http://www.w3.org/2000/svg";
const svg = (tag, attrs) => {
  const n = document.createElementNS(SVG_NS, tag);
  for (const k in attrs) n.setAttribute(k, attrs[k]);
  return n;
};

const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));

// A control's position is a number 0..1000 along its travel. Linear parameters
// could use their own units directly, but routing both scales through one
// normalised position keeps the mapping in a single place and lets log
// parameters (attack, release, hold, tune) feel right instead of bunching at
// one end of the dial.
const POSITION_STEPS = 1000;

// How far the time parameters' controls lean towards logarithmic. 0 is linear,
// 1 is fully logarithmic, and the value is the exponent of a geometric blend of
// the two mappings.
//
// A fully logarithmic control mirrors the encoder — compressor attack really is
// log(ms / 0.1) / log(100) in the device's index space — but it spends four
// fifths of its travel on the bottom decade, so the last stretch of the slider
// covers most of the range and the parameter is hard to dial in. Fully linear
// solves that and makes sub-millisecond attacks unreachable instead. Halfway
// between the two keeps the small values selectable without the top of the
// range being a cliff: compressor attack now runs 0.1, 0.7, 1.7, 3.2, 5.6,
// 10 ms across the travel rather than 0.1, 0.25, 0.6, 1.6, 4, 10.
const LOG_BLEND = 0.5;

// curveValue is the control's position-to-value mapping, before snapping.
function curveValue(p, t) {
  if (p.scale === "log" && p.min > 0) {
    const lin = p.min + t * (p.max - p.min);
    const log = p.min * Math.pow(p.max / p.min, t);
    return Math.pow(lin, 1 - LOG_BLEND) * Math.pow(log, LOG_BLEND);
  }
  return p.min + t * (p.max - p.min);
}

function toPosition(p, value) {
  const v = clamp(value, p.min, p.max);
  if (p.scale !== "log" || p.min <= 0) {
    return Math.round(((v - p.min) / (p.max - p.min)) * POSITION_STEPS);
  }
  // The blend has no closed-form inverse. It is monotonic, so bisecting over
  // positions finds the right one in ten iterations.
  let lo = 0;
  let hi = POSITION_STEPS;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (curveValue(p, mid / POSITION_STEPS) < v) lo = mid;
    else hi = mid;
  }
  const dLo = Math.abs(curveValue(p, lo / POSITION_STEPS) - v);
  const dHi = Math.abs(curveValue(p, hi / POSITION_STEPS) - v);
  return dLo <= dHi ? lo : hi;
}

// Snap to the registry's resolution so the value sent matches what is shown.
// Rounding to the step alone leaves binary-floating-point residue —
// Math.round(2.5 / 0.1) * 0.1 is 2.5000000000000004 — which ends up in the
// config file, so the result is trimmed to the precision the step carries.
function snap(p, v) {
  const stepped = p.step > 0 ? Math.round(v / p.step) * p.step : v;
  const dp = p.step >= 1 ? 0 : p.step >= 0.1 ? 1 : 2;
  return clamp(Number(stepped.toFixed(dp)), p.min, p.max);
}

// Where a value sits along its control's travel, 0 at the bottom of the range
// and 1 at the top.
const toNorm = (p, value) => toPosition(p, value) / POSITION_STEPS;

/* ------------------------------------------------------------------- knobs
 *
 * Parameters are dials rather than sliders. This is a rack of processors on a
 * microphone, and a dial is what the parameter is on the hardware it imitates —
 * but the reason it is here is space: a dial states its position in a square,
 * so five of them sit in the width one slider used to need, and a card that ran
 * to five stacked rows now fits on one.
 *
 * The pointer sweeps 270°, the conventional throw, leaving the bottom open for
 * the value to sit under the number.
 */

const KNOB_START = 135; // degrees, screen coordinates: 90 points down
const KNOB_SWEEP = 270;
const KNOB_RING_R = 42; // in the 100x100 viewBox the dial is drawn in
const KNOB_CAP_R = 31; // the cap the value sits in, clear of the pointer

// How far a pointer must travel for a knob to cross its whole range. Turning a
// dial by dragging is a linear gesture whatever the control looks like; 190px
// is about a comfortable forearm movement, and holding shift stretches it to a
// quarter of the sensitivity for setting an exact value.
const KNOB_TRAVEL_PX = 190;
const KNOB_FINE_PX = 760;

function knobPoint(r, t) {
  const a = ((KNOB_START + t * KNOB_SWEEP) * Math.PI) / 180;
  return [50 + r * Math.cos(a), 50 + r * Math.sin(a)];
}

function knobArc(r, t0, t1) {
  const [x0, y0] = knobPoint(r, t0);
  const [x1, y1] = knobPoint(r, t1);
  const large = (t1 - t0) * KNOB_SWEEP > 180 ? 1 : 0;
  return `M ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1}`;
}

// Decimal places follow the registry's resolution, so the readout never claims
// more precision than the parameter actually carries.
const formatNumber = (p, v) => v.toFixed(p.step >= 1 ? 0 : p.step >= 0.1 ? 1 : 2);

// The unit sits beside the editable number rather than inside it, so what the
// field contains is exactly what can be typed back into it.
const unitLabel = (p) => p.unit || "";

// Accepts what someone would actually type: "2.5", "2.5 ms", "-13.5 dB", "3:1".
function parseTyped(text) {
  const m = String(text).match(/-?\d*\.?\d+/);
  return m ? Number(m[0]) : NaN;
}

const hexPairs = (h) => (h ? h.replace(/(..)/g, "$1 ").trim() : "");

/* ------------------------------------------------------- device signal maths
 *
 * These mirror internal/protocol/encode.go. They are here so the graphs are
 * driven by the quantities the microphone is actually sent rather than by a
 * shape chosen to look plausible: the gate's one-pole attack coefficient, its
 * hold and release ramps and its hysteresis offset are the same expressions the
 * encoder transmits, so the plotted curve moves for the reasons the device's
 * own gate would. See docs/protocol.md.
 */

const FS = 48000; // the device runs at 48 kHz; every time constant is in samples

const dbToAmp = (db) => Math.pow(10, db / 20);
const ampToDb = (a) => (a > 1e-7 ? 20 * Math.log10(a) : -140);

// Noise gate attack, FUN_1401031b0: with b = 2 - cos(w) this is 1 - (b - sqrt(b^2 - 1)),
// the textbook one-pole time-constant design.
function gateAttackCoef(ms) {
  const w = 5.0 / ((ms / 1000) * FS);
  const c = Math.cos(w);
  return Math.sqrt(c * c - 4 * c + 3) + c - 1;
}

// Hold and release are the bare reciprocal 1/(s x 48000) — a per-sample
// decrement, so the counter traverses its full range in exactly the stated time.
const gateRamp = (ms) => 1 / ((ms / 1000) * FS);

// Hysteresis maps 0..100% onto -1..-8 dB below the opening threshold.
const gateHysteresisDb = (pct) => -1 - 7 * (pct / 100);

// A one-pole coefficient stepped dt ms at a time, used for the compressor
// envelope. Exact for any dt, so the plot can be drawn at a few hundred points
// instead of at 48 kHz.
const poleStep = (tauMs, dtMs) => (tauMs > 0 ? 1 - Math.exp(-dtMs / tauMs) : 1);

/* --------------------------------------------------------- the test signal
 *
 * The compressor and the gate act on level over time, not on frequency, so
 * what shows their effect is a waveform: the same passage before and after,
 * with the difference between the two visible directly. Both run on the
 * programme below.
 *
 * It is fixed rather than derived from the current settings. An earlier version
 * moved the test signal's noise floor with the gate's threshold, which kept the
 * picture tidy but meant dragging the threshold barely changed it. Against a
 * fixed programme every slider has a visible consequence — including setting
 * the threshold under the noise floor, where the gate correctly stops working.
 */

const PROGRAMME_MS = 620; // length of the phrase itself, before any tail
const FLOOR_DB = -52; // room noise between phrases
const SYLLABLES = [
  { at: 0.04, len: 0.15, db: -4 },
  { at: 0.26, len: 0.12, db: -16 },
  { at: 0.45, len: 0.17, db: -7 },
  { at: 0.7, len: 0.13, db: -22 },
];

// Amplitude of the programme at t in 0..1 of its length: syllables with a fast
// onset and a decaying tail, over the noise floor.
function programmeAmp(t) {
  let a = dbToAmp(FLOOR_DB);
  for (const s of SYLLABLES) {
    if (t < s.at || t >= s.at + s.len) continue;
    const u = (t - s.at) / s.len;
    const shape = u < 0.1 ? u / 0.1 : Math.pow(1 - (u - 0.1) / 0.9, 0.8);
    a = Math.max(a, dbToAmp(s.db) * shape);
  }
  return a;
}

// A voice-like carrier so the drawn waveform is a waveform and not a smooth
// blob. Peak-normalised, so the programme's dB values are the real peak levels.
const CARRIER_NORM = 1 / 1.1;
function carrier(t) {
  const p = 2 * Math.PI * 165 * t;
  return CARRIER_NORM * (Math.sin(p) + 0.35 * Math.sin(2 * p + 1) + 0.18 * Math.sin(3 * p + 2));
}

// Waveforms are drawn on a dB height scale, not a linear one. At -52 dB the
// noise floor is 0.25% of full scale and simply invisible linearly, which is
// exactly the part of the picture the gate is there to change.
const WAVE_FLOOR_DB = -66;
const waveHeight = (amp) => clamp((ampToDb(amp) - WAVE_FLOOR_DB) / -WAVE_FLOOR_DB, 0, 1);

// A peak detector feeding the gain computer, which is how a compressor or gate
// decides what the level is. Instant attack, decaying over PEAK_DECAY_MS, so
// the gain follows the syllable rather than the individual carrier cycles.
const PEAK_DECAY_MS = 8;

/* ---------------------------------------------------------- signal sourcing */

// signalSource decides what the level processors run on: the microphone when
// the level monitor is on, otherwise the synthetic phrase.
//
// d is device samples per simulation step, the unit the gate's coefficients are
// expressed in. Live audio derives it from the browser's sample rate, so a
// stream that is not at the device's 48 kHz still gets the right time
// constants; the synthetic phrase decimates its own 48 kHz timeline to keep the
// step count bounded when hold and release are long.
function signalSource(p, cols, live, synthSpanMs) {
  if (live && live.samples.length > 1) {
    const rate = live.sampleRate || FS;
    return {
      live: true,
      n: live.samples.length,
      d: FS / rate,
      dtMs: 1000 / rate,
      spanMs: (live.samples.length / rate) * 1000,
      sampleAt: (i) => live.samples[i],
    };
  }

  const total = Math.round((synthSpanMs / 1000) * FS);
  // Keep the step at or under 0.05 ms so even the fastest attack resolves.
  const d = Math.max(1, Math.min(Math.floor((0.05 * FS) / 1000), Math.floor(total / 6000)) || 1);
  const n = Math.max(2, Math.floor(total / d));
  const dtMs = (d / FS) * 1000;

  return {
    live: false,
    n,
    d,
    dtMs,
    spanMs: synthSpanMs,
    sampleAt: (i) => {
      const tMs = i * dtMs;
      return programmeAmp(tMs / PROGRAMME_MS) * carrier(tMs / 1000);
    },
  };
}

/* ---------------------------------------------------------- the simulations */

// simulateGate runs the gate the device is configured to run, sample by sample.
// Decimating by d is exact rather than approximate: a one-pole's pole raised to
// the power d is the same filter observed every d samples, and the linear ramps
// simply scale.
function simulateGate(p, cols, live) {
  const openAmp = dbToAmp(p.thr);
  const closeAmp = openAmp * dbToAmp(gateHysteresisDb(p.hyst));
  const floor = dbToAmp(p.range);

  // d is device samples per simulation step, which is what the coefficients are
  // expressed in. Live audio sets it from the browser's sample rate; the
  // synthetic programme decimates its own 48 kHz timeline.
  const src = signalSource(p, cols, live, 620 + p.hold + p.release * 1.15 + 80);
  const { n, d, dtMs, spanMs, sampleAt } = src;

  const aCoef = 1 - Math.pow(1 - gateAttackCoef(p.attack), d);
  const hStep = gateRamp(p.hold) * d;
  const rStep = gateRamp(p.release) * d;
  const peakDecay = 1 - poleStep(PEAK_DECAY_MS, dtMs);

  const inPeak = new Float32Array(cols);
  const outPeak = new Float32Array(cols);
  // The detector level and the gain applied to it, both recorded at the column's
  // loudest sample. Taking them at different instants — a maximum for one and
  // whatever happened to be last for the other — made the two level traces
  // disagree by a few dB even with the gate wide open.
  const envPeak = new Float32Array(cols);
  const gainAtPeak = new Float32Array(cols);

  let gain = floor;
  let env = 0;
  let holdLeft = 0;
  let open = false;

  for (let i = 0; i < n; i++) {
    const x = sampleAt(i);
    const mag = Math.abs(x);
    env = Math.max(mag, env * peakDecay);

    // Hysteresis: once open the gate stays open until the level falls below the
    // lower closing threshold, not merely below the opening one.
    open = open ? env > closeAmp : env > openAmp;

    if (open) holdLeft = 1;
    else if (holdLeft > 0) holdLeft -= hStep;

    const target = open || holdLeft > 0 ? 1 : floor;
    if (target > gain) gain += (target - gain) * aCoef;
    else gain = Math.max(target, gain - rStep);

    const y = x * gain;
    const c = Math.min(cols - 1, Math.floor((i / n) * cols));
    if (mag > inPeak[c]) inPeak[c] = mag;
    if (Math.abs(y) > outPeak[c]) outPeak[c] = Math.abs(y);
    if (env > envPeak[c]) {
      envPeak[c] = env;
      gainAtPeak[c] = gain;
    }
  }

  const inDb = new Float32Array(cols);
  const outDb = new Float32Array(cols);
  for (let c = 0; c < cols; c++) {
    inDb[c] = ampToDb(envPeak[c]);
    outDb[c] = ampToDb(envPeak[c] * gainAtPeak[c]);
  }

  return {
    inPeak,
    outPeak,
    inDb,
    outDb,
    spanMs,
    live: src.live,
    openDb: p.thr,
    closeDb: ampToDb(closeAmp),
  };
}

// simulateComp is the feed-forward compressor that threshold, ratio, attack,
// release and make-up gain describe, run over the same programme.
function simulateComp(p, cols, live) {
  const src = signalSource(p, cols, live, 620 + Math.max(0, p.release - 120));
  const { n, dtMs, spanMs, sampleAt } = src;

  const aStep = poleStep(p.attack, dtMs);
  const rStep = poleStep(p.release, dtMs);
  const peakDecay = 1 - poleStep(PEAK_DECAY_MS, dtMs);

  const inPeak = new Float32Array(cols);
  const outPeak = new Float32Array(cols);
  const grDb = new Float32Array(cols);

  let gr = 0;
  let env = 0;
  let peakGr = 0;

  for (let i = 0; i < n; i++) {
    const x = sampleAt(i);
    const mag = Math.abs(x);
    env = Math.max(mag, env * peakDecay);

    const level = ampToDb(env);
    const target = level > p.thr ? (level - p.thr) * (1 - 1 / p.ratio) : 0;
    gr += (target - gr) * (target > gr ? aStep : rStep);
    if (gr > peakGr) peakGr = gr;

    const y = x * dbToAmp(p.gain - gr);
    const c = Math.min(cols - 1, Math.floor((i / n) * cols));
    if (mag > inPeak[c]) inPeak[c] = mag;
    if (Math.abs(y) > outPeak[c]) outPeak[c] = Math.abs(y);
    if (gr > grDb[c]) grDb[c] = gr;
  }

  return { inPeak, outPeak, grDb, spanMs, peakGr, live: src.live };
}

// Aural Exciter and Big Bottom are the honest exception in this file. Their
// corner frequency is exact — it is the number the packet carries — but the
// device's filter topology is not recovered: ae_tune_1 and ae_tune_2 are two
// Q31 coefficients whose meaning docs/protocol.md does not settle, and
// Big Bottom's tune sends a bare index with no coefficient at all. So the skirt
// is drawn as a conventional second-order shelf, which is right about which
// part of the spectrum is touched and where it rolls off, and the caption on
// each graph says as much.
function shelfDb(f, corner, gainDb, side) {
  const r = Math.pow(f / corner, 4);
  const w = side === "low" ? 1 / (1 + r) : r / (1 + r);
  return gainDb * w;
}

/* ------------------------------------------------------- canvas primitives */

const VIZ_FONT = '10px ui-monospace, "Cascadia Mono", Consolas, monospace';

function vizPalette() {
  const css = getComputedStyle(document.documentElement);
  const v = (n, f) => css.getPropertyValue(n).trim() || f;
  return {
    accent: v("--accent", "#4c9aff"),
    accentSoft: v("--accent-soft", "rgba(76,154,255,0.14)"),
    grid: v("--border", "#232a35"),
    gridStrong: v("--border-strong", "#323b49"),
    dim: v("--text-faint", "#6b7889"),
    text: v("--text-dim", "#9aa7b8"),
    warn: v("--warn", "#d29922"),
  };
}

function line(ctx, x1, y1, x2, y2, color, dash) {
  ctx.save();
  ctx.strokeStyle = color;
  ctx.lineWidth = 1;
  if (dash) ctx.setLineDash(dash);
  // Half-pixel offsets keep single-pixel rules from smearing across two rows.
  ctx.beginPath();
  ctx.moveTo(Math.round(x1) + 0.5, Math.round(y1) + 0.5);
  ctx.lineTo(Math.round(x2) + 0.5, Math.round(y2) + 0.5);
  ctx.stroke();
  ctx.restore();
}

function text(ctx, s, x, y, color, align, baseline) {
  ctx.save();
  ctx.fillStyle = color;
  ctx.font = VIZ_FONT;
  ctx.textAlign = align || "left";
  ctx.textBaseline = baseline || "middle";
  ctx.fillText(s, x, y);
  ctx.restore();
}

// plot traces a series, optionally filling to a baseline and optionally dashed.
function plot(ctx, n, at, color, width, fillTo, dash) {
  ctx.save();
  ctx.beginPath();
  for (let i = 0; i < n; i++) {
    const [x, y] = at(i);
    if (i === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  }
  if (fillTo !== undefined) {
    ctx.save();
    ctx.lineTo(at(n - 1)[0], fillTo);
    ctx.lineTo(at(0)[0], fillTo);
    ctx.closePath();
    ctx.fillStyle = color;
    ctx.globalAlpha *= 0.16;
    ctx.fill();
    ctx.restore();
  }
  ctx.strokeStyle = color;
  ctx.lineWidth = width || 2;
  ctx.lineJoin = "round";
  if (dash) ctx.setLineDash(dash);
  ctx.stroke();
  ctx.restore();
}

// arrowHead draws a solid triangle at (x, y) pointing along the unit vector
// (dx, dy). Used on its own to show direction of travel, and in pairs by span().
function arrowHead(ctx, x, y, dx, dy, color, size) {
  const s = size || 4;
  ctx.save();
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.moveTo(x, y);
  ctx.lineTo(x - dx * s - dy * s * 0.55, y - dy * s + dx * s * 0.55);
  ctx.lineTo(x - dx * s + dy * s * 0.55, y - dy * s - dx * s * 0.55);
  ctx.closePath();
  ctx.fill();
  ctx.restore();
}

// span draws a double-headed measuring arrow with a label beside it.
//
// Range and hysteresis are both *distances* — how far the floor sits below
// unity, and how much quieter the signal must get before the gate lets go. Two
// threshold lines imply the second and a flattened trace implies the first, but
// neither states the quantity. Drawing them as measured distances does, and it
// is why these two parameters were previously hard to see the effect of.
function span(ctx, x1, y1, x2, y2, label, color, labelSide) {
  const len = Math.hypot(x2 - x1, y2 - y1);
  ctx.save();
  ctx.strokeStyle = color;
  ctx.lineWidth = 1;
  ctx.beginPath();
  ctx.moveTo(x1, y1);
  ctx.lineTo(x2, y2);
  ctx.stroke();
  ctx.restore();

  // Below ~11px the two heads meet and the line reads as a blob; the label
  // still carries the number, so the heads are simply dropped.
  if (len >= 11) {
    const dx = (x2 - x1) / len;
    const dy = (y2 - y1) / len;
    arrowHead(ctx, x2, y2, dx, dy, color, 4);
    arrowHead(ctx, x1, y1, -dx, -dy, color, 4);
  }

  const mx = (x1 + x2) / 2;
  const my = (y1 + y2) / 2;
  const vertical = Math.abs(y2 - y1) > Math.abs(x2 - x1);
  if (labelSide === "top" || !vertical) {
    // Above the span rather than beside it. A short vertical span in a crowded
    // corner has no horizontal room for a label, but the lane above it is free.
    text(ctx, label, mx, Math.min(y1, y2) - 6, color, "center", "bottom");
  } else {
    text(ctx, label, mx + (labelSide === "left" ? -6 : 6), my, color,
      labelSide === "left" ? "right" : "left");
  }
}

const sourceLabel = (sim) =>
  sim.live ? "Your microphone, live," : "A test phrase";

// What the browser receives has already been through the microphone's own DSP —
// there is no way to tap the signal ahead of it — so a live picture shows what
// these settings would do on top of whatever the device is already doing. Worth
// saying plainly, since with the effect enabled it is being applied twice.
const LIVE_CAVEAT = (sim) =>
  sim.live
    ? " The stream is the microphone's output, so the device's own processing is already in it."
    : "";

// legend names the two waveform traces, right-aligned so it clears the axis
// labels down the left-hand edge.
function legend(ctx, x, y, pal) {
  ctx.save();
  ctx.font = VIZ_FONT;
  const gap = ctx.measureText("out").width + 8;
  ctx.restore();
  text(ctx, "out", x, y, pal.accent, "right");
  text(ctx, "in", x - gap, y, pal.text, "right");
}

// drawWave draws a peak-per-column waveform mirrored about the centre of a
// rect, the way an audio editor shows a clip.
function drawWave(ctx, rect, peak, color, mode) {
  const cols = peak.length;
  const mid = rect.y + rect.h / 2;
  const half = rect.h / 2;
  const xAt = (c) => rect.x + (c / (cols - 1)) * rect.w;

  ctx.save();
  ctx.beginPath();
  for (let c = 0; c < cols; c++) ctx.lineTo(xAt(c), mid - waveHeight(peak[c]) * half);
  for (let c = cols - 1; c >= 0; c--) ctx.lineTo(xAt(c), mid + waveHeight(peak[c]) * half);
  ctx.closePath();

  if (mode === "fill") {
    ctx.fillStyle = color;
    ctx.globalAlpha *= 0.38;
    ctx.fill();
  } else {
    ctx.fillStyle = color;
    ctx.globalAlpha *= 0.5;
    ctx.fill();
    ctx.globalAlpha /= 0.5;
    ctx.strokeStyle = color;
    ctx.lineWidth = 1;
    ctx.stroke();
  }
  ctx.restore();
}

// The level processors carry two stacked panels and need the room; the two
// frequency graphs are a single plot and do not. Anything absent here uses the
// height in the stylesheet.
const VIZ_HEIGHT = { Compressor: 168, "Noise Gate": 168 };

// Second graphs, shown only in advanced mode.
// A second graph under the main one, for the effects whose parameters the
// time-domain picture cannot show. The waveform answers "what does this do to
// my voice"; these answer "what do these numbers mean", which is the question
// threshold, ratio, gain, range and hysteresis actually pose.
const AUX_VIZ = {
  Compressor: { label: "transfer curve", draw: "drawCompressorCurve" },
  "Noise Gate": { label: "gate transfer and hysteresis loop", draw: "drawGateTransfer" },
};

/* -------------------------------------------------------------- application */

class App {
  constructor() {
    this.ws = null;
    this.connected = false;
    this.schema = null;
    this.params = new Map(); // "effectId:paramId" -> control record
    this.effects = new Map(); // effectId -> { card, enabledInput, schema }
    this.values = new Map(); // "effectId:paramId" -> current value
    this.enabled = new Map(); // effectId -> bool
    this.previewTimers = new Map();
    this.reconnectAttempts = 0;
    // Live FFT of the microphone, set only while the level monitor is running.
    // The frequency graphs draw it behind their curves so the band an effect
    // lifts can be compared against the band the voice actually occupies.
    this.spectrum = null;
    // A rolling window of real samples, also only while monitoring. The
    // compressor and gate models run over it in place of the synthetic phrase,
    // so their sliders can be dialled in against actual speech.
    this.liveAudio = null;

    this.statusEl = document.getElementById("status");
    this.connectBtn = document.getElementById("connect-btn");
    this.bannerEl = document.getElementById("banner");

    this.presets = [];
    // Set while a Delete click is waiting for its confirming second click.
    this.deleteArmed = null;

    this.initTheme();
    this.initAdvanced();
    this.initPresets();
    this.initTransfer();
    this.initMonitor();

    this.connectBtn.addEventListener("click", () => {
      if (this.connected) this.disconnect();
      else this.connect();
    });

    window.addEventListener("beforeunload", () => this.ws && this.ws.close());

    this.boot();
  }

  async boot() {
    try {
      const res = await fetch("/api/schema");
      if (!res.ok) throw new Error(`schema request failed: ${res.status}`);
      this.schema = await res.json();
    } catch (err) {
      this.showBanner(`Could not load the parameter schema: ${err.message}`);
      return;
    }

    this.renderDevice(this.schema.device);
    this.renderEffects(this.schema.effects);
    this.setControlsEnabled(false);
    this.connect();
  }

  /* ---------------------------------------------------------- presentation */

  initTheme() {
    const saved = localStorage.getItem("theme");
    if (saved) document.documentElement.dataset.theme = saved;
    document.getElementById("theme-btn").addEventListener("click", () => {
      const next =
        document.documentElement.dataset.theme === "light" ? "dark" : "light";
      document.documentElement.dataset.theme = next;
      localStorage.setItem("theme", next);
      this.drawAll();
    });
  }

  initAdvanced() {
    const toggle = document.getElementById("advanced-toggle");
    const on = localStorage.getItem("advanced") === "1";
    toggle.checked = on;
    this.applyAdvanced(on);

    toggle.addEventListener("change", (e) => {
      this.applyAdvanced(e.target.checked);
      localStorage.setItem("advanced", e.target.checked ? "1" : "0");
      // Encoding details are only fetched while advanced is on, so fill them in
      // the moment it is switched on rather than waiting for the next drag.
      if (e.target.checked) this.refreshAllEncodings();
    });
  }

  applyAdvanced(on) {
    document.body.toggleAttribute("data-advanced", on);
    document.getElementById("device-panel").hidden = !on;
    // Advanced-only graphs have no layout while hidden, so they can only be
    // drawn once the attribute is set.
    this.drawAll();
  }

  /* ------------------------------------------------------------------ presets
   *
   * A preset is a whole DSP state — every parameter of every effect, and which
   * effects are on. The server owns them; this side only names one and shows
   * what came back.
   */

  initPresets() {
    this.presetSelect = document.getElementById("preset-select");
    this.presetNote = document.getElementById("preset-note");
    this.presetNameInput = document.getElementById("preset-name");
    this.presetLoadBtn = document.getElementById("preset-load");
    this.presetSaveBtn = document.getElementById("preset-save");
    this.presetDeleteBtn = document.getElementById("preset-delete");

    this.presetSelect.addEventListener("change", () => {
      this.disarmDelete();
      this.updatePresetControls();
    });

    this.presetLoadBtn.addEventListener("click", () => {
      const name = this.presetSelect.value;
      if (name) this.send({ type: "load_preset", name });
    });

    const save = () => {
      const name = this.presetNameInput.value.trim();
      if (!name) {
        this.presetNameInput.focus();
        return;
      }
      this.pendingPreset = name; // select it once the new list arrives
      this.send({ type: "save_preset", name });
      this.presetNameInput.value = "";
    };

    this.presetSaveBtn.addEventListener("click", save);
    this.presetNameInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") save();
    });
    this.presetNameInput.addEventListener("input", () => this.updatePresetControls());

    // Delete confirms in place rather than through confirm(): a modal dialog
    // blocks the page, and the second click says the same thing.
    this.presetDeleteBtn.addEventListener("click", () => {
      const name = this.presetSelect.value;
      if (!name) return;
      if (this.deleteArmed !== name) {
        this.deleteArmed = name;
        this.presetDeleteBtn.textContent = "Delete?";
        this.presetDeleteBtn.classList.add("btn-danger");
        setTimeout(() => this.disarmDelete(), 4000);
        return;
      }
      this.disarmDelete();
      this.send({ type: "delete_preset", name });
    });
  }

  disarmDelete() {
    this.deleteArmed = null;
    this.presetDeleteBtn.textContent = "Delete";
    this.presetDeleteBtn.classList.remove("btn-danger");
  }

  setPresets(list) {
    this.presets = Array.isArray(list) ? list : [];

    // Keep the selection across a refresh: the list is rebuilt on every save
    // and delete, and losing the highlighted row each time is disorienting.
    const want = this.pendingPreset || this.presetSelect.value;
    this.pendingPreset = null;

    this.presetSelect.textContent = "";
    if (!this.presets.length) {
      const opt = el("option", null, "No presets");
      opt.value = "";
      this.presetSelect.append(opt);
    }
    for (const p of this.presets) {
      const opt = el("option", null, p.builtin ? `${p.name} (built-in)` : p.name);
      opt.value = p.name;
      this.presetSelect.append(opt);
    }
    if (want && this.presets.some((p) => p.name === want)) this.presetSelect.value = want;

    this.disarmDelete();
    this.updatePresetControls();
  }

  currentPreset() {
    return this.presets.find((p) => p.name === this.presetSelect.value) || null;
  }

  updatePresetControls() {
    const sel = this.currentPreset();
    const live = this.connected;

    this.presetSelect.disabled = !live || !this.presets.length;
    this.presetLoadBtn.disabled = !live || !sel;
    // Built-ins live in the binary, so the server would refuse both of these.
    this.presetDeleteBtn.disabled = !live || !sel || sel.builtin;
    this.presetNameInput.disabled = !live;
    this.presetSaveBtn.disabled = !live || !this.presetNameInput.value.trim();
    // Export only reads; import writes to the device, so it needs the link.
    this.importBtn.disabled = !live;

    this.presetNote.textContent = sel
      ? sel.description || (sel.builtin ? "Built-in preset." : "Saved on this machine.")
      : "Dial the effects in, then save the whole state under a name.";
  }

  /* -------------------------------------------------- import and export
   *
   * The file is the server's own state document — byte for byte what the tool
   * writes to its config file — so an export can be handed back to the CLI with
   * 'load --config <file>', and a config file can be imported here.
   */

  initTransfer() {
    this.exportBtn = document.getElementById("export-btn");
    this.importBtn = document.getElementById("import-btn");
    const file = document.getElementById("import-file");

    this.exportBtn.addEventListener("click", () => this.exportSettings());
    this.importBtn.addEventListener("click", () => file.click());

    file.addEventListener("change", async () => {
      const f = file.files && file.files[0];
      // Clearing the input matters: picking the same file twice in a row
      // otherwise fires no second change event.
      file.value = "";
      if (f) await this.importSettings(f);
    });
  }

  async exportSettings() {
    let text;
    try {
      // Straight from the server rather than rebuilt from the sliders, so the
      // file is the state the device was actually given.
      const res = await fetch("/api/state");
      if (!res.ok) throw new Error(`state request failed: ${res.status}`);
      text = JSON.stringify(await res.json(), null, 2);
    } catch (err) {
      this.showBanner(`Could not export settings: ${err.message}`);
      return;
    }

    const stamp = new Date().toISOString().slice(0, 10);
    const url = URL.createObjectURL(new Blob([text], { type: "application/json" }));
    const a = el("a");
    a.href = url;
    a.download = `rode-dsp-settings-${stamp}.json`;
    a.click();
    // Not revoked immediately: the click only starts the download, and pulling
    // the URL out from under it in the same tick is a race. A second is far
    // longer than the browser needs to take a copy of a few hundred bytes.
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  async importSettings(file) {
    let doc;
    try {
      doc = JSON.parse(await file.text());
    } catch (err) {
      this.showBanner(`${file.name} is not valid JSON: ${err.message}`);
      return;
    }

    // Check the shape here as well as on the server. The server rejects a
    // document with no recognised effects too, but saying so before anything
    // is sent keeps a mis-picked file from looking like a device error.
    const known = this.schema && this.schema.effects.some((e) => doc && doc[e.key]);
    if (!known) {
      this.showBanner(`${file.name} does not contain any RØDE DSP settings.`);
      return;
    }

    this.hideBanner();
    this.send({ type: "import_state", value: doc });
  }

  setStatus(state, text) {
    this.statusEl.dataset.state = state;
    this.statusEl.querySelector(".status-text").textContent = text;
  }

  showBanner(msg) {
    this.bannerEl.textContent = msg;
    this.bannerEl.hidden = false;
  }

  hideBanner() {
    this.bannerEl.hidden = true;
  }

  renderDevice(d) {
    const dl = document.getElementById("device-facts");
    dl.textContent = "";
    const facts = [
      ["Vendor", d.vid],
      ["Product", d.pid],
      ["Report ID", d.reportId],
      ["Report size", `${d.packetSize} bytes`],
      ["Coefficient scale", d.scale],
    ];
    for (const [k, v] of facts) {
      const wrap = el("div");
      wrap.append(el("dt", null, k), el("dd", null, v));
      dl.append(wrap);
    }
    document.getElementById("device-note").textContent =
      `Encodings transcribed from ${d.source}. Values shown are the ones this ` +
      `server last sent; the microphone can also be asked what it holds — run ` +
      `"rode-dsp status" to read it back.`;
  }

  renderEffects(effects) {
    const host = document.getElementById("effects");
    host.textContent = "";

    for (const eff of effects) {
      const card = el("section", "card");
      card.dataset.enabled = "false";

      // --- header: name, id chip, reset, enable switch
      const head = el("div", "card-head");
      head.append(el("h2", null, eff.name));

      const idChip = el("span", "card-id adv-only", `effect 0x${eff.id.toString(16).padStart(2, "0")}`);
      head.append(idChip);

      const reset = el("button", "btn btn-ghost", "Reset");
      reset.addEventListener("click", () => this.send({ type: "reset_defaults", effect: eff.id }));
      head.append(reset);

      const sw = el("label", "effect-switch switch-inline");
      sw.title = `Enable ${eff.name}`;
      const swInput = el("input");
      swInput.type = "checkbox";
      const track = el("span", "switch-track");
      track.append(el("span", "switch-thumb"));
      sw.append(swInput, track);
      swInput.addEventListener("change", (e) =>
        this.send({ type: "set_enable", effect: eff.id, enabled: e.target.checked })
      );
      head.append(sw);
      card.append(head);

      // --- body: visualisation then parameters
      const body = el("div", "card-body");

      const figure = el("figure", "viz-figure");
      const canvas = el("canvas", "viz");
      canvas.setAttribute("role", "img");
      canvas.setAttribute("aria-label", `${eff.name} response`);
      if (VIZ_HEIGHT[eff.name]) canvas.style.height = `${VIZ_HEIGHT[eff.name]}px`;
      figure.append(canvas);

      // A second graph, where an effect has a view worth showing that does not
      // belong in the main picture. It used to be advanced-only; it carries the
      // only depiction of range, hysteresis, ratio and make-up gain, so hiding
      // it left those parameters with no visualisation at all.
      let aux = null;
      if (AUX_VIZ[eff.name]) {
        aux = el("canvas", "viz viz-aux");
        aux.setAttribute("role", "img");
        aux.setAttribute("aria-label", `${eff.name} ${AUX_VIZ[eff.name].label}`);
        figure.append(aux);
      }

      // The caption explains what is plotted and, on the frequency graphs, which
      // part of the shape is measured and which is drawn by convention. That is
      // a protocol claim, so it belongs with the rest of them behind Advanced.
      const caption = el("figcaption", "viz-caption adv-only");
      figure.append(caption);
      body.append(figure);

      const params = el("div", "params");
      for (const p of eff.params) params.append(this.renderParam(eff, p));
      body.append(params);

      card.append(body);
      host.append(card);

      this.effects.set(eff.id, { card, enabledInput: swInput, schema: eff, canvas, aux, caption, reset });
    }

    this.drawAll();
    window.addEventListener("resize", () => this.drawAll());
  }

  renderParam(eff, p) {
    const key = `${eff.id}:${p.id}`;
    const wrap = el("div", "param");
    wrap.append(el("span", "param-name", p.name));

    // --- the dial
    const knob = el("div", "knob");
    knob.tabIndex = 0;
    knob.setAttribute("role", "slider");
    knob.setAttribute("aria-label", `${eff.name} ${p.name}`);
    knob.setAttribute("aria-valuemin", p.min);
    knob.setAttribute("aria-valuemax", p.max);
    knob.setAttribute("aria-disabled", "true");

    const dial = svg("svg", { class: "knob-dial", viewBox: "0 0 100 100", "aria-hidden": "true" });
    const track = svg("path", { class: "knob-track", d: knobArc(KNOB_RING_R, 0, 1) });
    const arc = svg("path", { class: "knob-arc", d: "" });
    const cap = svg("circle", { class: "knob-cap", cx: 50, cy: 50, r: KNOB_CAP_R });
    const pointer = svg("line", { class: "knob-pointer", x1: 50, y1: 50, x2: 50, y2: 50 });
    dial.append(track, arc, cap, pointer);
    knob.append(dial);

    // The readout sits in the cap, where the number is on a mixing desk. It is
    // a field, not a label: however well the dial's curve is chosen, some
    // values are easier to say than to find, and typing 2.5 asks for 2.5.
    const entry = el("span", "knob-readout");
    const valueEl = el("input", "param-input");
    valueEl.type = "text";
    valueEl.inputMode = "decimal";
    valueEl.autocomplete = "off";
    valueEl.spellcheck = false;
    valueEl.value = "—";
    valueEl.disabled = true;
    valueEl.setAttribute("aria-label", `${eff.name} ${p.name} value`);
    entry.append(valueEl);
    const unit = unitLabel(p);
    if (unit) entry.append(el("span", "param-unit", unit));
    knob.append(entry);
    wrap.append(knob);

    const detail = el("div", "detail");
    wrap.append(detail);

    const rec = { eff, p, knob, valueEl, detail, encoding: null, dragging: false };
    this.params.set(key, rec);

    const shown = () => this.values.get(key) ?? p.default;
    const disabled = () => knob.getAttribute("aria-disabled") === "true";

    // paint moves the dial to a value without announcing it anywhere.
    const paint = (v) => {
      const t = toNorm(p, v);
      // At the very bottom of the range the arc has no length, and a round cap
      // would draw it as a dot sitting outside the track.
      arc.setAttribute("d", t > 0.004 ? knobArc(KNOB_RING_R, 0, t) : "");
      // The pointer rides the clear ring between the cap and the track, so it
      // never crosses the number the cap is there to hold.
      const [x1, y1] = knobPoint(KNOB_CAP_R + 1, t);
      const [x2, y2] = knobPoint(KNOB_RING_R - 4, t);
      pointer.setAttribute("x1", x1);
      pointer.setAttribute("y1", y1);
      pointer.setAttribute("x2", x2);
      pointer.setAttribute("y2", y2);
      knob.setAttribute("aria-valuenow", v);
      knob.setAttribute("aria-valuetext", `${formatNumber(p, v)}${unit ? " " + unit : ""}`);
      if (document.activeElement !== valueEl) valueEl.value = formatNumber(p, v);
    };
    rec.paint = paint;

    // hold updates everything on this side — the dial, the readout, the
    // graphs, the advanced encoding — without writing to the device.
    const hold = (v) => {
      this.values.set(key, v);
      paint(v);
      this.draw(eff.id);
      this.queuePreview(eff.id, p.id, v);
    };

    // commit does that and sends it. Turning the dial holds while it moves and
    // commits when it is let go, so a gesture across the range is one write
    // rather than several hundred.
    const commit = (raw) => {
      const v = snap(p, raw);
      hold(v);
      this.send({ type: "set_param", effect: eff.id, param: p.id, value: v });
    };

    /* --- turning it
     *
     * Dragging, not tracking the angle of the pointer. Absolute angle means a
     * touch that lands anywhere but the current position jumps the value there
     * — under a fingertip that covers the whole dial, the wrong value every
     * time. A drag has no such moment: the knob starts from where it is and
     * follows the finger. Vertical and horizontal both count, so the gesture
     * works whichever way the hand happens to move.
     */
    let drag = null;

    knob.addEventListener("pointerdown", (e) => {
      if (disabled() || e.target === valueEl) return;
      e.preventDefault();
      // Capture keeps the gesture alive when the finger or cursor wanders off
      // the dial, which on a control this small it will. Losing the capture is
      // not a reason to refuse the turn.
      try {
        knob.setPointerCapture(e.pointerId);
      } catch {}
      drag = { x: e.clientX, y: e.clientY, t: toNorm(p, shown()) };
      rec.dragging = true;
      knob.classList.add("knob-turning");
    });

    knob.addEventListener("pointermove", (e) => {
      if (!drag) return;
      // Accumulated rather than measured from the start of the gesture, so
      // reaching for shift part-way through changes the sensitivity from that
      // point on instead of teleporting the value.
      const span = e.shiftKey ? KNOB_FINE_PX : KNOB_TRAVEL_PX;
      drag.t = clamp(drag.t + ((e.clientX - drag.x) - (e.clientY - drag.y)) / span, 0, 1);
      drag.x = e.clientX;
      drag.y = e.clientY;
      hold(snap(p, curveValue(p, drag.t)));
    });

    const endDrag = () => {
      if (!drag) return;
      drag = null;
      rec.dragging = false;
      knob.classList.remove("knob-turning");
      this.send({ type: "set_param", effect: eff.id, param: p.id, value: shown() });
    };

    knob.addEventListener("pointerup", endDrag);
    knob.addEventListener("pointercancel", endDrag);

    knob.addEventListener(
      "wheel",
      (e) => {
        if (disabled()) return;
        e.preventDefault();
        commit(shown() + (e.deltaY < 0 ? 1 : -1) * (p.step || 1));
      },
      { passive: false }
    );

    // Back to the default, the way a plugin's knob does it.
    knob.addEventListener("dblclick", () => {
      if (!disabled()) commit(p.default);
    });

    // Arrow keys step by the parameter's own resolution rather than by some
    // fraction of the dial's travel, which near the bottom of a curved range
    // would be far less than one step and so do nothing at all.
    const KEY_STEPS = {
      ArrowUp: 1,
      ArrowRight: 1,
      ArrowDown: -1,
      ArrowLeft: -1,
      PageUp: 10,
      PageDown: -10,
    };
    const stepKey = (e) => {
      const n = KEY_STEPS[e.key];
      if (n === undefined) return false;
      e.preventDefault();
      if (!disabled()) commit(shown() + n * (p.step || 1));
      return true;
    };

    knob.addEventListener("keydown", (e) => {
      if (stepKey(e)) return;
      if (e.key === "Home") {
        e.preventDefault();
        commit(p.min);
      } else if (e.key === "End") {
        e.preventDefault();
        commit(p.max);
      }
    });

    valueEl.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        valueEl.value = formatNumber(p, shown());
        valueEl.blur();
        return;
      }
      if (e.key === "Enter") {
        valueEl.blur(); // fires change below
        return;
      }
      stepKey(e);
    });

    // change covers both Enter and losing focus.
    valueEl.addEventListener("change", () => {
      const n = parseTyped(valueEl.value);
      if (Number.isFinite(n)) commit(n);
      else valueEl.value = formatNumber(p, shown());
    });

    valueEl.addEventListener("focus", () => valueEl.select());

    return wrap;
  }

  renderDetail(rec) {
    const { p, detail, encoding } = rec;
    detail.textContent = "";

    const row = (k, v, cls) => {
      const r = el("div", "detail-row");
      r.append(el("span", "detail-key", k));
      const val = el("span", "detail-val" + (cls ? " " + cls : ""), v);
      r.append(val);
      detail.append(r);
    };

    row("param", `0x${p.id.toString(16).padStart(2, "0")}`);

    if (encoding) {
      if (encoding.index >= 0) {
        row("index", `${encoding.index} / 255`);
      }
      row("payload", `${hexPairs(encoding.hex)}  (${encoding.bytes} B)`);
      row("report", hexPairs(encoding.packet));
    } else {
      row("payload", "—");
    }

    if (p.table) row("table", p.table);

    if (p.formula) {
      const f = el("div", "detail-formula", p.formula);
      detail.append(f);
    }
  }

  /* ------------------------------------------------------------- transport */

  connect() {
    this.setStatus("connecting", "Connecting");
    try {
      this.ws = new WebSocket(WS_URL);
    } catch (err) {
      this.setStatus("error", "Failed");
      this.showBanner(`WebSocket could not be opened: ${err.message}`);
      return;
    }

    this.ws.onopen = () => {
      this.connected = true;
      this.reconnectAttempts = 0;
      this.hideBanner();
      this.setStatus("connected", "Connected");
      this.connectBtn.textContent = "Disconnect";
      this.setControlsEnabled(true);
      this.send({ type: "get_state" });
      this.send({ type: "list_presets" });
    };

    this.ws.onmessage = (e) => {
      let msg;
      try {
        msg = JSON.parse(e.data);
      } catch {
        return;
      }
      this.handle(msg);
    };

    this.ws.onerror = () => this.setStatus("error", "Error");

    this.ws.onclose = () => {
      const wasConnected = this.connected;
      this.connected = false;
      this.ws = null;
      this.connectBtn.textContent = "Connect";
      this.setControlsEnabled(false);
      this.setStatus("disconnected", "Disconnected");
      // Only auto-retry a connection that dropped, never one the user closed.
      if (wasConnected && this.reconnectAttempts < 5) {
        this.reconnectAttempts++;
        setTimeout(() => this.connect(), 1500 * this.reconnectAttempts);
      }
    };
  }

  disconnect() {
    this.reconnectAttempts = 99; // suppress the auto-retry
    if (this.ws) this.ws.close();
  }

  send(obj) {
    if (this.ws && this.connected) this.ws.send(JSON.stringify(obj));
  }

  handle(msg) {
    switch (msg.type) {
      case "state":
        if (msg.enabled !== undefined && msg.effect !== undefined) {
          this.setEnabled(msg.effect, msg.enabled);
        } else if (msg.value) {
          this.applyState(msg.value);
        }
        break;

      case "param_update":
        this.setValue(msg.effect, msg.param, msg.value, msg.encoding);
        break;

      case "preview":
        this.setEncoding(msg.effect, msg.param, msg.encoding);
        break;

      case "presets":
        this.setPresets(msg.presets || []);
        break;

      case "error":
        this.showBanner(msg.error || "Unknown server error");
        break;
    }
  }

  /* ----------------------------------------------------------------- state */

  // The server marshals state keyed by name ("compressor": {"threshold": ...}).
  // The schema carries the same keys, so the two are matched here without
  // reimplementing the key derivation.
  applyState(state) {
    if (!this.schema) return;

    for (const eff of this.schema.effects) {
      const effState = state[eff.key];
      if (!effState) continue;

      if (typeof effState.enabled === "boolean") {
        this.setEnabled(eff.id, effState.enabled);
      }
      for (const p of eff.params) {
        const v = effState[p.key];
        if (typeof v === "number") this.setValue(eff.id, p.id, v, null);
      }
    }

    if (document.body.hasAttribute("data-advanced")) this.refreshAllEncodings();
  }

  setValue(effectId, paramId, value, encoding) {
    const key = `${effectId}:${paramId}`;
    const rec = this.params.get(key);
    if (!rec || typeof value !== "number") return;

    // Do not fight the hand on the dial: a broadcast that arrives mid-gesture
    // is this client's own earlier value coming back.
    if (rec.dragging) return;

    this.values.set(key, value);
    rec.paint(value);
    if (encoding) {
      rec.encoding = encoding;
    }
    this.renderDetail(rec);
    this.draw(effectId);
  }

  setEncoding(effectId, paramId, encoding) {
    const rec = this.params.get(`${effectId}:${paramId}`);
    if (!rec || !encoding) return;
    rec.encoding = encoding;
    this.renderDetail(rec);
  }

  setEnabled(effectId, on) {
    const rec = this.effects.get(effectId);
    if (!rec) return;
    this.enabled.set(effectId, on);
    rec.enabledInput.checked = on;
    rec.card.dataset.enabled = String(on);
    this.draw(effectId);
  }

  setControlsEnabled(on) {
    for (const rec of this.params.values()) {
      rec.knob.setAttribute("aria-disabled", String(!on));
      rec.knob.tabIndex = on ? 0 : -1;
      rec.valueEl.disabled = !on;
    }
    for (const rec of this.effects.values()) {
      rec.enabledInput.disabled = !on;
      rec.reset.disabled = !on;
    }
    this.updatePresetControls();
  }

  // Previews are throttled per parameter: a drag fires input events far faster
  // than there is any point asking the server to re-encode.
  queuePreview(effectId, paramId, value) {
    if (!document.body.hasAttribute("data-advanced")) return;
    const key = `${effectId}:${paramId}`;
    if (this.previewTimers.has(key)) return;

    this.previewTimers.set(
      key,
      setTimeout(() => {
        this.previewTimers.delete(key);
        this.send({
          type: "preview",
          effect: effectId,
          param: paramId,
          value: this.values.get(key),
        });
      }, PREVIEW_INTERVAL_MS)
    );
  }

  refreshAllEncodings() {
    for (const [key, value] of this.values) {
      const [effectId, paramId] = key.split(":").map(Number);
      this.send({ type: "preview", effect: effectId, param: paramId, value });
    }
  }

  /* --------------------------------------------------------- visualisation */

  get(effectId, paramName) {
    const eff = this.effects.get(effectId);
    if (!eff) return undefined;
    const p = eff.schema.params.find((x) => x.name === paramName);
    if (!p) return undefined;
    const v = this.values.get(`${effectId}:${p.id}`);
    return v === undefined ? p.default : v;
  }

  drawAll() {
    for (const id of this.effects.keys()) this.draw(id);
  }

  // prepare sizes a canvas for the display's pixel ratio and hands back a
  // cleared context, or null when the canvas is not currently laid out — an
  // advanced-mode graph is display:none in simple mode and has no size to draw
  // into.
  prepare(c, effectId) {
    if (!c || !c.clientWidth) return null;
    const dpr = window.devicePixelRatio || 1;
    const w = c.clientWidth;
    const h = c.clientHeight || 132;
    if (c.width !== w * dpr || c.height !== h * dpr) {
      c.width = w * dpr;
      c.height = h * dpr;
    }
    const ctx = c.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    ctx.globalAlpha = this.enabled.get(effectId) !== false ? 1 : 0.4;
    return { ctx, w, h };
  }

  draw(effectId) {
    const rec = this.effects.get(effectId);
    if (!rec) return;

    const main = this.prepare(rec.canvas, effectId);
    if (!main) return;
    const { ctx, w, h } = main;

    const pal = vizPalette();

    const auxSpec = AUX_VIZ[rec.schema.name];
    if (auxSpec) {
      const a = this.prepare(rec.aux, effectId);
      if (a) this[auxSpec.draw](a.ctx, a.w, a.h, pal);
    }

    let caption = "";
    switch (rec.schema.name) {
      case "Compressor":
        caption = this.drawCompressor(ctx, w, h, pal);
        break;
      case "Noise Gate":
        caption = this.drawGate(ctx, w, h, pal);
        break;
      case "Aural Exciter":
        caption = this.drawTilt(ctx, w, h, pal, {
          corner: this.get(2, "Tune"),
          amount: this.get(2, "Harmonics"),
          side: "high",
          what: "Harmonics are generated above the tune frequency",
        });
        break;
      case "Big Bottom":
        caption = this.drawTilt(ctx, w, h, pal, {
          corner: this.get(3, "Tune"),
          amount: this.get(3, "Drive"),
          side: "low",
          what: "Bass is enhanced below the tune frequency",
        });
        break;
    }

    ctx.globalAlpha = 1;
    if (rec.caption) rec.caption.textContent = caption;
  }

  // The waveform before and after, with gain reduction on the same time axis
  // underneath. Threshold and ratio show up as flattened peaks, make-up gain as
  // the whole clip lifting, attack and release as the shape of the reduction
  // trace. The static transfer curve that used to be here says the same thing
  // about threshold and ratio but nothing at all about the rest, so it has
  // moved to the second canvas, in advanced mode.
  drawCompressor(ctx, w, h, pal) {
    const thr = this.get(0, "Threshold") ?? -20;
    const ratio = this.get(0, "Ratio") ?? 3;
    const gain = this.get(0, "Gain") ?? 0;
    const attack = this.get(0, "Attack") ?? 0.7;
    const release = this.get(0, "Release") ?? 21;

    const A = { x: 26, y: 8, w: w - 34, h: Math.round(h * 0.52) };
    const cols = Math.max(24, Math.round(A.w));
    const sim = simulateComp({ thr, ratio, gain, attack, release }, cols, this.liveAudio);

    // Waveform panel. Input first, output over it: where the output is narrower
    // the compressor took the peak down, where it is wider the make-up gain
    // pushed it up.
    const midY = A.y + A.h / 2;
    const thrHalf = waveHeight(dbToAmp(thr)) * (A.h / 2);
    line(ctx, A.x, midY, A.x + A.w, midY, pal.grid);
    line(ctx, A.x, midY - thrHalf, A.x + A.w, midY - thrHalf, pal.warn, [2, 3]);
    line(ctx, A.x, midY + thrHalf, A.x + A.w, midY + thrHalf, pal.warn, [2, 3]);

    drawWave(ctx, A, sim.inPeak, pal.text, "fill");
    drawWave(ctx, A, sim.outPeak, pal.accent, "stroke");

    text(ctx, "thr", A.x - 4, midY - thrHalf, pal.warn, "right");
    legend(ctx, A.x + A.w - 2, A.y + 5, pal);

    // Gain reduction over the same time axis, so a peak in the waveform and the
    // reduction it caused line up vertically.
    const B = { x: A.x, y: A.y + A.h + 18, w: A.w, h: h - (A.y + A.h + 18) - 14 };
    const grMax = Math.max(3, sim.peakGr * 1.3);
    const by = (db) => B.y + (clamp(db, 0, grMax) / grMax) * B.h;

    line(ctx, B.x, B.y, B.x + B.w, B.y, pal.gridStrong);
    line(ctx, B.x, B.y + B.h, B.x + B.w, B.y + B.h, pal.grid);
    plot(ctx, cols, (i) => [B.x + (i / (cols - 1)) * B.w, by(sim.grDb[i])], pal.accent, 1.5, B.y);

    text(ctx, "0", B.x - 4, B.y, pal.dim, "right");
    text(ctx, `-${grMax.toFixed(0)}`, B.x - 4, B.y + B.h, pal.dim, "right");
    text(ctx, "gain reduction dB", B.x + 4, B.y - 9, pal.dim, "left");
    text(ctx, `${sim.spanMs.toFixed(0)} ms`, B.x + B.w, h - 11, pal.dim, "right", "top");

    return (
      `${sourceLabel(sim)} before (filled) and after (outlined) the compressor, on a ` +
      `dB height scale. Peaks past the ${thr.toFixed(1)} dB threshold are pulled in at ` +
      `${ratio.toFixed(1)}:1 — up to ${sim.peakGr.toFixed(1)} dB — over ` +
      `${attack.toFixed(2)} ms, recovering over ${release.toFixed(0)} ms, then ` +
      `${gain.toFixed(1)} dB of make-up gain is added.${LIVE_CAVEAT(sim)}`
    );
  }

  // The transfer curve, advanced mode only: input level against output level,
  // which is the clearest statement of what threshold, ratio and make-up gain
  // do, but which cannot show attack or release at all.
  drawCompressorCurve(ctx, w, h, pal) {
    const thr = this.get(0, "Threshold") ?? -20;
    const ratio = this.get(0, "Ratio") ?? 3;
    const gain = this.get(0, "Gain") ?? 0;

    const A = { x: 28, y: 10, w: w - 40, h: h - 26 };
    const lo = -60;
    const hi = 0;
    const x = (db) => A.x + ((db - lo) / (hi - lo)) * A.w;
    const y = (db) => A.y + A.h - ((clamp(db, lo, hi) - lo) / (hi - lo)) * A.h;

    for (const db of [-48, -36, -24, -12]) {
      line(ctx, x(db), A.y, x(db), A.y + A.h, pal.grid);
      line(ctx, A.x, y(db), A.x + A.w, y(db), pal.grid);
      text(ctx, String(db), x(db), A.y + A.h + 6, pal.dim, "center", "top");
      text(ctx, String(db), A.x - 4, y(db), pal.dim, "right");
    }
    line(ctx, A.x, A.y, A.x, A.y + A.h, pal.gridStrong);
    line(ctx, A.x, A.y + A.h, A.x + A.w, A.y + A.h, pal.gridStrong);

    // Unity: where the curve would sit with the compressor doing nothing.
    line(ctx, x(lo), y(lo), x(hi), y(hi), pal.dim, [3, 3]);

    const outAt = (inDb) => (inDb <= thr ? inDb : thr + (inDb - thr) / ratio) + gain;
    plot(
      ctx,
      121,
      (i) => {
        const inDb = lo + (i / 120) * (hi - lo);
        return [x(inDb), y(outAt(inDb))];
      },
      pal.accent,
      2
    );

    line(ctx, x(thr), A.y, x(thr), A.y + A.h, pal.warn, [2, 2]);
    // A threshold near 0 dB puts its line against the right edge, where a
    // left-aligned label runs off the canvas.
    const thrRight = x(thr) > A.x + A.w - 62;
    text(ctx, `thr ${thr.toFixed(1)}`, x(thr) + (thrRight ? -4 : 4), A.y + 5, pal.warn,
      thrRight ? "right" : "left");

    // The knee: where the curve leaves unity, which is the one point on the
    // plot that threshold alone decides.
    ctx.save();
    ctx.fillStyle = pal.warn;
    ctx.beginPath();
    ctx.arc(x(thr), y(outAt(thr)), 3.5, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();

    // Make-up gain as the distance between the curve and where it would sit
    // without it — the parameter's actual contribution, rather than the total
    // offset from unity, which also contains the compression.
    // Measured on the left of the plot rather than the right: a high threshold
    // pushes both its own label and the curve into the top-right corner, and
    // the two collided there. Below the threshold the curve is simply
    // in + gain, so the span reads the same wherever it is taken.
    const gx = lo + (hi - lo) * 0.16;
    if (Math.abs(gain) > 0.05) {
      span(ctx, x(gx), y(outAt(gx) - gain), x(gx), y(outAt(gx)),
        `gain ${gain > 0 ? "+" : ""}${gain.toFixed(1)}`, pal.accent, "right");
    }

    // Ratio, labelled on the segment whose slope it is. Above the curve, which
    // is empty; below it the fill and the unity line are already competing.
    // Skipped when the compressed segment is too short to label honestly.
    if (hi - thr > 8) {
      const midIn = thr + (hi - thr) * 0.45;
      if (x(midIn) > x(thr) + 12 && x(midIn) < A.x + A.w - 26) {
        text(ctx, `${ratio.toFixed(1)}:1`, x(midIn), y(outAt(midIn)) - 9, pal.accent, "center");
      }
    }

    text(ctx, "in dB", A.x + A.w, A.y + A.h + 6, pal.dim, "right", "top");
    // Inside the plot: right-aligned outside it, this ran off the left edge.
    text(ctx, "out dB", A.x + 5, A.y + 6, pal.dim, "left");
  }

  // Waveform first: the phrase before and after, so the parts the gate removes
  // are simply missing from the outlined trace. The level panel underneath puts
  // the same signal on a dB axis against the opening and closing thresholds,
  // which is where hysteresis becomes visible.
  //
  // Everything is run with the coefficients the device is sent — the one-pole
  // attack, the hold and release ramps, the hysteresis offset and the range
  // floor.
  drawGate(ctx, w, h, pal) {
    const thr = this.get(1, "Threshold") ?? -42;
    const attack = this.get(1, "Attack") ?? 0.8;
    const hold = this.get(1, "Hold") ?? 80;
    const release = this.get(1, "Release") ?? 210;
    const range = this.get(1, "Range") ?? -9;
    const hyst = this.get(1, "Hysteresis") ?? 50;

    const A = { x: 26, y: 8, w: w - 34, h: Math.round(h * 0.46) };
    const cols = Math.max(24, Math.round(A.w));
    const sim = simulateGate({ thr, attack, hold, release, range, hyst }, cols, this.liveAudio);

    /* ---- waveform ---- */
    const midY = A.y + A.h / 2;
    line(ctx, A.x, midY, A.x + A.w, midY, pal.grid);
    drawWave(ctx, A, sim.inPeak, pal.text, "fill");
    drawWave(ctx, A, sim.outPeak, pal.accent, "stroke");
    legend(ctx, A.x + A.w - 2, A.y + 5, pal);

    /* ---- level against the thresholds ---- */
    const B = { x: A.x, y: A.y + A.h + 14, w: A.w, h: h - (A.y + A.h + 14) - 14 };
    const lo = -95;
    const hi = 0;
    const x = (i) => B.x + (i / (cols - 1)) * B.w;
    const y = (db) => B.y + B.h - ((clamp(db, lo, hi) - lo) / (hi - lo)) * B.h;

    for (const db of [-25, -50, -75]) {
      line(ctx, B.x, y(db), B.x + B.w, y(db), pal.grid);
      text(ctx, String(db), B.x - 4, y(db), pal.dim, "right");
    }
    line(ctx, B.x, B.y, B.x, B.y + B.h, pal.gridStrong);
    line(ctx, B.x, B.y + B.h, B.x + B.w, B.y + B.h, pal.gridStrong);

    plot(ctx, cols, (i) => [x(i), y(sim.outDb[i])], pal.accent, 1.75, y(lo));
    plot(ctx, cols, (i) => [x(i), y(sim.inDb[i])], pal.text, 1.25, undefined, [3, 3]);

    line(ctx, B.x, y(sim.openDb), B.x + B.w, y(sim.openDb), pal.warn, [4, 3]);
    line(ctx, B.x, y(sim.closeDb), B.x + B.w, y(sim.closeDb), pal.warn, [1, 3]);
    text(ctx, "open", B.x + B.w, y(sim.openDb) - 6, pal.warn, "right");
    text(ctx, "close", B.x + B.w, y(sim.closeDb) + 6, pal.warn, "right");

    text(ctx, "level dB", B.x + 4, B.y - 6, pal.dim, "left");
    text(ctx, `${sim.spanMs.toFixed(0)} ms`, B.x + B.w, h - 11, pal.dim, "right", "top");

    const base =
      `${sourceLabel(sim)} before (filled) and after (outlined) the gate` +
      (sim.live ? "" : `, over a ${FLOOR_DB} dB noise floor`) +
      `. It opens at ${sim.openDb.toFixed(1)} dB and stays open down to ` +
      `${sim.closeDb.toFixed(1)} dB, holds ${hold.toFixed(0)} ms, then closes over ` +
      `${release.toFixed(0)} ms to ${range.toFixed(1)} dB of attenuation.`;

    if (sim.live) return base + LIVE_CAVEAT(sim);

    return (
      base +
      (sim.closeDb > FLOOR_DB
        ? " The gaps between syllables are what it removes."
        : ` Both thresholds sit under the ${FLOOR_DB} dB noise floor, so the gate never closes.`)
    );
  }

  // Input level against output level: the gate's transfer, drawn as the
  // hysteresis loop it actually is.
  //
  // This exists because range and hysteresis were the two parameters whose
  // effect the waveform could not show. Range only appears in the waveform if
  // the gate happens to close during the phrase, and even then as a subtle
  // change in floor height; hysteresis appears only if the level happens to
  // settle between the two thresholds, which for most settings it never does.
  // Here both are structural and always visible: hysteresis is the horizontal
  // gap between the branches, range is the vertical drop of the closed one.
  //
  // The loop is the point. A rising signal follows the lower branch until it
  // reaches the opening threshold; a falling signal stays on the upper branch
  // until the lower closing threshold. Two paths between the same two levels is
  // what hysteresis means, and no single-valued curve can express it.
  drawGateTransfer(ctx, w, h, pal) {
    const thr = this.get(1, "Threshold") ?? -42;
    const range = this.get(1, "Range") ?? -9;
    const hyst = this.get(1, "Hysteresis") ?? 50;

    const openDb = thr;
    const closeDb = thr + gateHysteresisDb(hyst);
    const hystDb = openDb - closeDb;

    const A = { x: 32, y: 14, w: w - 46, h: h - 32 };

    // Independent x and y scales rather than a square plot: the canvas is wide
    // and short, so equal dB-per-pixel would squash the range drop to a few
    // pixels. Unity is therefore not at 45 degrees, and is drawn explicitly.
    const xlo = clamp(Math.min(closeDb - 8, closeDb + range - 4), -110, -26);
    const ylo = xlo + Math.min(range, 0) - 3;
    const x = (db) => A.x + ((clamp(db, xlo, 0) - xlo) / (0 - xlo)) * A.w;
    const y = (db) => A.y + A.h - ((clamp(db, ylo, 0) - ylo) / (0 - ylo)) * A.h;

    // Tick step follows the window: a tight threshold-and-range combination can
    // leave a span of 30 dB, where 20 dB steps give a single labelled tick.
    const xSpan = 0 - xlo;
    const xStep = xSpan > 80 ? 20 : xSpan > 45 ? 10 : 5;
    for (let db = -xStep; db > xlo + 3; db -= xStep) {
      line(ctx, x(db), A.y, x(db), A.y + A.h, pal.grid);
      text(ctx, String(db), x(db), A.y + A.h + 6, pal.dim, "center", "top");
    }
    // The y axis spans further than the x axis by the depth of the range, so it
    // gets its own ticks rather than sharing the x ones.
    const yStep = ylo < -160 ? 60 : ylo < -90 ? 40 : 20;
    for (let db = -yStep; db > ylo + 6; db -= yStep) {
      line(ctx, A.x, y(db), A.x + A.w, y(db), pal.grid);
      text(ctx, String(db), A.x - 4, y(db), pal.dim, "right");
    }
    line(ctx, A.x, A.y, A.x, A.y + A.h, pal.gridStrong);
    line(ctx, A.x, A.y + A.h, A.x + A.w, A.y + A.h, pal.gridStrong);

    // The band the gate can be in either state in.
    ctx.save();
    ctx.fillStyle = pal.warn;
    ctx.globalAlpha *= 0.13;
    ctx.fillRect(x(closeDb), A.y, Math.max(1, x(openDb) - x(closeDb)), A.h);
    ctx.restore();

    // Unity is out = in. With independent x and y scales that is not the box
    // diagonal, so it is plotted from the axis values rather than the corners.
    line(ctx, x(xlo), y(xlo), x(0), y(0), pal.dim, [3, 3]);
    text(ctx, "unity", A.x + A.w - 2, y(-2) + 7, pal.dim, "right", "top");

    // Closed branch: everything is pulled down by the range. Dashed, because it
    // is the state the signal is being held in rather than passed through.
    plot(ctx, 2, (i) => {
      const d = i ? openDb : xlo;
      return [x(d), y(d + range)];
    }, pal.accent, 2, undefined, [5, 3]);

    // Open branch: unity, from the closing threshold upwards.
    plot(ctx, 2, (i) => {
      const d = i ? 0 : closeDb;
      return [x(d), y(d)];
    }, pal.accent, 2);

    // The two transitions, arrowed in the direction the gate travels.
    line(ctx, x(openDb), y(openDb + range), x(openDb), y(openDb), pal.accent, [2, 2]);
    arrowHead(ctx, x(openDb), y(openDb), 0, -1, pal.accent, 5);
    line(ctx, x(closeDb), y(closeDb), x(closeDb), y(closeDb + range), pal.accent, [2, 2]);
    arrowHead(ctx, x(closeDb), y(closeDb + range), 0, 1, pal.accent, 5);

    // Hysteresis: the horizontal gap, measured.
    const bandY = A.y + 9;
    span(ctx, x(closeDb), bandY, x(openDb), bandY, `hyst ${hystDb.toFixed(1)} dB`, pal.warn);

    // Range: the vertical drop, measured well left of the loop so the arrow
    // does not sit on top of the transitions.
    const rx = x(xlo) + Math.max(26, (x(closeDb) - x(xlo)) * 0.45);
    const rdb = xlo + (0 - xlo) * ((rx - A.x) / A.w);
    if (range < -0.05) {
      // A shallow range keeps the loop close to the left edge, leaving no room
      // for the label beside the arrow — and the space to its right belongs to
      // the "close" marker. It goes above the arrow instead.
      const side = rx - A.x > 78 ? "left" : "top";
      span(ctx, rx, y(rdb), rx, y(rdb + range), `range ${range.toFixed(1)} dB`, pal.accent, side);
    }

    text(ctx, "open", x(openDb) + 5, y(openDb) - 8, pal.warn, "left");
    text(ctx, "close", x(closeDb) - 5, y(closeDb + range) + 8, pal.warn, "right");
    text(ctx, "in dB", A.x + A.w, A.y + A.h + 6, pal.dim, "right", "top");
    text(ctx, "out dB", A.x + 4, A.y + 6, pal.dim, "left");
  }

  // Frequency response over the audible band. The corner is exact — it is the
  // frequency the packet carries — but the device's filter shape is not
  // recovered, so the skirt is a conventional second-order shelf and the
  // caption says so rather than implying a measurement.
  drawTilt(ctx, w, h, pal, o) {
    if (o.corner === undefined) return "";
    const amount = o.amount ?? 0;
    const peak = (amount / 100) * 12; // dB of lift at full drive

    const fLo = 20;
    const fHi = 20000;
    const A = { x: 26, y: 8, w: w - 34, h: h - 26 };
    // 0 dB sits exactly on the bottom rule so the response curve's baseline and
    // the live spectrum's baseline are the same line. With the axis starting
    // below zero the two disagreed, and the spectrum appeared to sink under a
    // curve it shares no scale with.
    const dbLo = 0;
    const dbHi = 14;
    const x = (f) => A.x + (Math.log(f / fLo) / Math.log(fHi / fLo)) * A.w;
    const y = (db) => A.y + A.h - ((clamp(db, dbLo, dbHi) - dbLo) / (dbHi - dbLo)) * A.h;

    // Decade rules, with the 1-2-5 minor ticks a frequency plot is read against.
    for (const f of [50, 100, 200, 500, 1000, 2000, 5000, 10000]) {
      const major = f === 100 || f === 1000 || f === 10000;
      line(ctx, x(f), A.y, x(f), A.y + A.h, major ? pal.gridStrong : pal.grid);
      if (major) {
        text(ctx, f >= 1000 ? `${f / 1000}k` : String(f), x(f), A.y + A.h + 6, pal.dim, "center", "top");
      }
    }
    for (const db of [0, 6, 12]) {
      line(ctx, A.x, y(db), A.x + A.w, y(db), db === 0 ? pal.gridStrong : pal.grid, db === 0 ? null : [2, 3]);
      text(ctx, db === 0 ? "0" : `+${db}`, A.x - 4, y(db), pal.dim, "right");
    }

    // Live input spectrum behind the curve, when the level monitor is running.
    // This is real audio: it shows whether the band being lifted is a band the
    // voice actually occupies.
    if (this.spectrum) {
      const { data, sampleRate, fftSize } = this.spectrum;
      const binHz = sampleRate / fftSize;
      ctx.save();
      ctx.globalAlpha *= 0.28;
      ctx.beginPath();
      ctx.moveTo(A.x, A.y + A.h);
      for (let px = 0; px <= A.w; px++) {
        const f = fLo * Math.pow(fHi / fLo, px / A.w);
        const bin = clamp(Math.round(f / binHz), 0, data.length - 1);
        const t = clamp((data[bin] + 100) / 80, 0, 1); // -100..-20 dBFS
        ctx.lineTo(A.x + px, A.y + A.h - t * A.h);
      }
      ctx.lineTo(A.x + A.w, A.y + A.h);
      ctx.closePath();
      ctx.fillStyle = pal.text;
      ctx.fill();
      ctx.restore();
    }

    const N = 220;
    plot(
      ctx,
      N,
      (i) => {
        const f = fLo * Math.pow(fHi / fLo, i / (N - 1));
        return [x(f), y(shelfDb(f, o.corner, peak, o.side))];
      },
      pal.accent,
      2,
      y(0)
    );

    line(ctx, x(o.corner), A.y, x(o.corner), A.y + A.h, pal.warn, [2, 2]);
    const label = `${Math.round(o.corner)} Hz`;
    text(
      ctx,
      label,
      clamp(x(o.corner) + 4, A.x, A.x + A.w - 34),
      A.y + 5,
      pal.warn,
      "left",
      "top"
    );
    text(ctx, "Hz", A.x + A.w, A.y + A.h + 6, pal.dim, "right", "top");

    return (
      `${o.what}. Corner ${Math.round(o.corner)} Hz is exact — it is the value in ` +
      `the packet — and the lift is about ${peak.toFixed(1)} dB at ${amount.toFixed(0)}%. ` +
      `The roll-off is drawn as a second-order shelf: the device's own filter shape ` +
      `has not been recovered.` +
      (this.spectrum ? " Shaded: live input spectrum." : "")
    );
  }

  /* ---------------------------------------------------------- level monitor */

  initMonitor() {
    const btn = document.getElementById("mon-btn");
    const fill = document.getElementById("meter-fill");
    const readout = document.getElementById("meter-readout");
    let stream = null;
    let audioCtx = null;
    let raf = null;

    const stop = () => {
      if (raf) cancelAnimationFrame(raf);
      if (stream) stream.getTracks().forEach((t) => t.stop());
      if (audioCtx) audioCtx.close();
      raf = stream = audioCtx = null;
      fill.style.width = "0%";
      readout.textContent = "—∞ dB";
      btn.textContent = "Monitor";
      btn.classList.remove("btn-on");
      this.spectrum = null;
      this.liveAudio = null;
      this.drawAll();
    };

    btn.addEventListener("click", async () => {
      if (stream) {
        stop();
        return;
      }
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false },
        });
      } catch (err) {
        this.showBanner(`Microphone access denied: ${err.message}`);
        stream = null;
        return;
      }

      audioCtx = new (window.AudioContext || window.webkitAudioContext)();
      const src = audioCtx.createMediaStreamSource(stream);
      const analyser = audioCtx.createAnalyser();
      // 4096 gives ~12 Hz bins at 48 kHz, which is enough resolution to say
      // anything meaningful about where Big Bottom's 60-312 Hz corner sits.
      analyser.fftSize = 4096;
      analyser.smoothingTimeConstant = 0.75;
      src.connect(analyser);

      // A second analyser purely for the waveform window. 32768 samples is the
      // largest the Web Audio API offers — about 680 ms at 48 kHz, enough for a
      // phrase and, on typical settings, for the gate's hold and release to
      // play out. Keeping it separate lets the spectrum stay at a size whose
      // bin count is worth plotting.
      const waveAnalyser = audioCtx.createAnalyser();
      waveAnalyser.fftSize = 32768;
      src.connect(waveAnalyser);

      const buf = new Float32Array(analyser.fftSize);
      const spec = new Float32Array(analyser.frequencyBinCount);
      const wave = new Float32Array(waveAnalyser.fftSize);
      let lastWave = 0;
      btn.textContent = "Stop";
      btn.classList.add("btn-on");

      const tick = () => {
        analyser.getFloatTimeDomainData(buf);
        let sum = 0;
        for (let i = 0; i < buf.length; i++) sum += buf[i] * buf[i];
        const rms = Math.sqrt(sum / buf.length);
        const db = rms > 0 ? 20 * Math.log10(rms) : -Infinity;
        const pct = clamp(((db + 60) / 60) * 100, 0, 100);
        fill.style.width = `${pct}%`;
        readout.textContent = db === -Infinity ? "—∞ dB" : `${db.toFixed(1)} dB`;

        analyser.getFloatFrequencyData(spec);
        this.spectrum = { data: spec, sampleRate: audioCtx.sampleRate, fftSize: analyser.fftSize };
        this.draw(2);
        this.draw(3);

        // The level processors cost two full model runs over 32k samples, so
        // they update at LIVE_REDRAW_MS rather than every frame. Nothing in a
        // waveform this long is legible faster than that anyway.
        const now = performance.now();
        if (now - lastWave >= LIVE_REDRAW_MS) {
          lastWave = now;
          waveAnalyser.getFloatTimeDomainData(wave);
          this.liveAudio = { samples: wave, sampleRate: audioCtx.sampleRate };
          this.draw(0);
          this.draw(1);
        }

        raf = requestAnimationFrame(tick);
      };
      tick();
    });

    window.addEventListener("beforeunload", stop);
  }
}

document.addEventListener("DOMContentLoaded", () => {
  window.app = new App();

  // Surface script failures in the page. Without this a thrown error inside an
  // async boot step just leaves the interface sitting there looking idle, with
  // the reason only in the devtools console.
  const report = (what) => window.app && window.app.showBanner(what);
  window.addEventListener("error", (e) => report(`Script error: ${e.message}`));
  window.addEventListener("unhandledrejection", (e) =>
    report(`Script error: ${(e.reason && e.reason.message) || e.reason}`)
  );
});
