'use strict';

// Shared helpers for the RØDE Connect instrumentation scripts.
//
// Ghidra shows addresses against the PE's preferred image base of 0x140000000.
// Windows applies ASLR, so every address from the Ghidra database must be
// rebased against the module's live base before Frida can use it.
//
// Load this first:
//   frida -n "RODE Connect.exe" -l tools/frida/rode.js -l tools/frida/log-encoders.js

const MODULE_NAME = 'RODE Connect.exe';
const GHIDRA_BASE = ptr('0x140000000');

// Frida 17 overhauled the Module API: the static shortcuts (Module.findBaseAddress,
// Module.findExportByName, ...) were removed in favour of Module instances obtained
// from Process. Try the current API first, then the pre-17 one, then a
// case-insensitive scan — Windows module names are not case-stable.
function findModule(name) {
  if (typeof Process.findModuleByName === 'function') {
    const m = Process.findModuleByName(name);
    if (m !== null) return m;
  }
  if (typeof Module.findBaseAddress === 'function') {
    const base = Module.findBaseAddress(name);
    if (base !== null) return { name: name, base: base };
  }
  const lower = name.toLowerCase();
  const all = Process.enumerateModules();
  for (const m of all) {
    if (m.name.toLowerCase() === lower) return m;
  }
  return null;
}

function moduleBase() {
  const m = findModule(MODULE_NAME);
  if (m === null) {
    const names = Process.enumerateModules().slice(0, 10).map(function (x) {
      return x.name;
    });
    throw new Error(
      'module ' + MODULE_NAME + ' not found. Loaded modules start: ' +
      names.join(', ') + '. The executable name may differ by release; check ' +
      'with Process.enumerateModules().');
  }
  return m.base;
}

const BASE = moduleBase();

// rebase converts a Ghidra address to a live one.
function rebase(ghidraAddr) {
  return BASE.add(ptr(ghidraAddr).sub(GHIDRA_BASE));
}

// Addresses recovered in the February 2026 analysis. These are pinned to one
// build (SHA-256 c3015816eacc3360d5b935a08fd05f4e948105604a2f36b812b9d58262a1023c);
// verify the installed executable's hash before trusting them, because nothing
// here can detect a mismatch: a wrong address silently hooks whatever code
// happens to live there.
const ADDR = {
  // Per-effect parameter encoders. Each converts UI floats to DSP coefficients
  // and calls SendDspPacket once per changed parameter.
  setCompressorParams: '0x140375130',
  setNoiseGateParams: '0x1403738a0',
  setAuralExciterParams: '0x140372910',
  setBigBottomParams: '0x140372080',

  // Builds the 29-byte report and hands it to the HID write path.
  sendDspPacket: '0x140375ec0',

  // Bilinear transform used for noise gate attack. A pure math leaf: no `this`,
  // no global state, so it is safe to call directly with NativeFunction.
  bilinearTransform: '0x1401031b0',
};

function banner(what) {
  console.log('[rode] ' + MODULE_NAME + ' base=' + BASE + ' (ghidra base ' + GHIDRA_BASE + ')');
  console.log('[rode] ' + what);
}

// Exported on globalThis rather than via module.exports: the Frida CLI has no
// module loader, so each -l script is evaluated in the shared global scope.
globalThis.RODE = {
  MODULE_NAME: MODULE_NAME,
  GHIDRA_BASE: GHIDRA_BASE,
  BASE: BASE,
  ADDR: ADDR,
  rebase: rebase,
  banner: banner,
};
