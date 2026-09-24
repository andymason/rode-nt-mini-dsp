'use strict';

// Logs every DSP packet RØDE Connect sends, alongside the UI value that caused
// it. Sweeping each slider through its range produces an authoritative
// (parameter, ui_value) -> packet_bytes table for all 15 parameters.
//
//   frida -n "RODE Connect.exe" -l tools/frida/rode.js -l tools/frida/log-encoders.js
//   python tools/frida/host.py log-encoders     # writes observed_*.csv
//
// This is the decisive verification channel. It settled, empirically and in one
// sitting, what the captures could not: whether the LUT index truncates or
// rounds, whether clamping happens before or after scaling, and what the
// compressor gain range really is.
//
// Note on floats: the Microsoft x64 ABI passes them in XMM0-XMM3, and Frida's
// CpuContext did not expose XMM registers before 17.16.0. Rather than depend on
// that, the encoder hooks below wrap each function in a typed NativeFunction and
// let Frida marshal the arguments — which works on every version.

const R = globalThis.RODE;
if (!R) throw new Error('load tools/frida/rode.js first (-l rode.js -l log-encoders.js)');

const PACKET_SIZE = 29;

// Last UI value seen entering an encoder, so the packet can be attributed.
let pending = { effect: null, value: null };

// --- SendDspPacket: capture the outgoing report -----------------------------
//
// Signature from the analysis: builds a 29-byte report and calls the HID write
// path. The report buffer is the interesting argument; which register holds it
// must be confirmed against the disassembly, so all four integer argument
// registers are probed for something that looks like a report.

function looksLikeReport(p) {
  if (p === null || p.isNull()) return false;
  try {
    return p.readU8() === 0x04; // report ID
  } catch (e) {
    return false;
  }
}

Interceptor.attach(R.rebase(R.ADDR.sendDspPacket), {
  onEnter: function (args) {
    let buf = null;
    for (let i = 0; i < 4 && buf === null; i++) {
      if (looksLikeReport(args[i])) buf = args[i];
    }
    if (buf === null) {
      console.log('[send] no argument looked like a report; check the prototype');
      return;
    }

    const bytes = buf.readByteArray(PACKET_SIZE);
    const u8 = new Uint8Array(bytes);
    const hex = Array.prototype.map
      .call(u8, function (b) { return b.toString(16).padStart(2, '0'); })
      .join('');

    const effect = u8[1], cmd = u8[2], param = u8[3];
    const cmdName = cmd === 0x02 ? 'SET' : cmd === 0x03 ? 'GET' : cmd === 0x01 ? 'GET-ALL' : cmd === 0x00 ? 'SET-ALL' : '0x' + cmd.toString(16);

    console.log('[send] effect=0x' + effect.toString(16).padStart(2, '0') +
      ' ' + cmdName +
      ' param=0x' + param.toString(16).padStart(2, '0') +
      ' ui=' + (pending.value === null ? '?' : pending.value) +
      ' payload=' + hex.slice(8));

    send({
      tag: 'packet',
      effect: effect,
      cmd: cmd,
      param: param,
      uiValue: pending.value,
      uiEffect: pending.effect,
      hex: hex,
    });
  },
});

// --- Set*Params: capture the UI value --------------------------------------
//
// Each is a __fastcall member taking `this` in RCX and the parameter struct or
// float alongside. Replacing them with a typed trampoline makes Frida decode
// the float argument regardless of Frida version.
//
// The exact prototypes still need confirming from the decompilation; the
// two-argument (pointer, float) shape below is the hypothesis. If a hooked
// function misbehaves, comment it out and read its signature first.

const ENCODERS = [
  { name: 'compressor', addr: R.ADDR.setCompressorParams },
  { name: 'noise_gate', addr: R.ADDR.setNoiseGateParams },
  { name: 'aural_exciter', addr: R.ADDR.setAuralExciterParams },
  { name: 'big_bottom', addr: R.ADDR.setBigBottomParams },
];

ENCODERS.forEach(function (e) {
  const addr = R.rebase(e.addr);
  const original = new NativeFunction(addr, 'void', ['pointer', 'float'], { abi: 'win64' });

  Interceptor.replace(addr, new NativeCallback(function (self, value) {
    pending = { effect: e.name, value: value };
    console.log('[enc]  ' + e.name + '(' + value + ')');
    original(self, value);
    pending = { effect: null, value: null };
  }, 'void', ['pointer', 'float'], { abi: 'win64' }));
});

// --- Direct sweep of the bilinear transform --------------------------------
//
// FUN_1401031b0 is a pure math leaf with no `this`, so it can be called
// directly. It is what NGAttackToUSB, NGHoldToUSB and NGReleaseToUSB in
// encode.go approximate with reciprocal-linear interpolation that is exact only
// at the endpoints. Call sweepBilinear() from the REPL once the prototype is
// confirmed.

globalThis.sweepBilinear = function (min, max, steps) {
  const fn = new NativeFunction(R.rebase(R.ADDR.bilinearTransform),
    'uint32', ['float'], { abi: 'win64' });

  const rows = [];
  for (let i = 0; i <= steps; i++) {
    const ms = min + (max - min) * i / steps;
    const out = fn(ms) >>> 0;
    rows.push({ input: ms, output: out });
  }
  send({ tag: 'sweep', name: 'bilinear', rows: rows });
  console.log('[sweep] ' + rows.length + ' points sent to host');
  return rows.length;
};

R.banner('logging DSP packets; move every slider through its full range');
console.log('[rode] sweepBilinear(min, max, steps) is available in the REPL');
