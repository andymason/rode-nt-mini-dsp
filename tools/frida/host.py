#!/usr/bin/env python3
"""Drive the RØDE Connect Frida agents and write their output to disk.

Usage:
    python tools/frida/host.py log-encoders

Requires 64-bit Python and `pip install frida-tools`. Run elevated: attaching to
a process at a higher integrity level otherwise fails, and Frida's injection can
trip Defender heuristics. If that becomes a problem, x64dbg logging breakpoints
are the lower-profile fallback.

Outputs:
    internal/protocol/testdata/observed_*.csv  (ui_value, payload) goldens
    internal/protocol/testdata/sweep_*.csv     direct function sweeps
"""

import argparse
import csv
import hashlib
import pathlib
import sys
from collections import defaultdict

try:
    import frida
except ImportError:
    frida = None  # checked in main(), so --help works without it installed

HERE = pathlib.Path(__file__).resolve().parent
REPO = HERE.parent.parent
TESTDATA_DIR = REPO / "internal" / "protocol" / "testdata"

PROCESS_NAME = "RODE Connect.exe"

MODES = {
    "log-encoders": ["rode.js", "log-encoders.js"],
}

# Effect ID -> config name, mirroring internal/protocol/constants.go.
EFFECT_NAMES = {
    0x00: "compressor",
    0x01: "noise_gate",
    0x02: "aural_exciter",
    0x03: "big_bottom",
    # Anything beyond here is the interesting case: an effect RØDE Connect
    # drives that this tool does not know about.
}


class Collector:
    def __init__(self, binary_sha):
        self.binary_sha = binary_sha
        self.packets = defaultdict(list)  # (effect, param) -> [(ui, hex)]

    def on_message(self, message, data):
        if message["type"] == "error":
            desc = message.get("description", message)
            print(f"[agent error] {desc}", file=sys.stderr)
            if "stack" in message:
                print(message["stack"], file=sys.stderr)
            return

        payload = message.get("payload")
        if not isinstance(payload, dict):
            return

        tag = payload.get("tag")
        if tag == "packet":
            self.record_packet(payload)
        elif tag == "sweep":
            self.write_sweep(payload)

    def record_packet(self, payload):
        effect = payload["effect"]
        param = payload["param"]
        ui = payload.get("uiValue")
        raw = payload["hex"]

        # Payload starts after report ID, effect, cmd, param.
        self.packets[(effect, param)].append((ui, raw[8:]))

    def write_sweep(self, payload):
        TESTDATA_DIR.mkdir(parents=True, exist_ok=True)
        path = TESTDATA_DIR / f"sweep_{payload['name']}.csv"
        with path.open("w", newline="", encoding="utf-8") as fh:
            w = csv.writer(fh)
            w.writerow(["input", "output_hex"])
            for row in payload["rows"]:
                w.writerow([f"{row['input']:.6f}", f"{row['output']:08x}"])
        print(f"[host] wrote {path.relative_to(REPO)} ({len(payload['rows'])} rows)")

    def flush_packets(self):
        if not self.packets:
            return

        TESTDATA_DIR.mkdir(parents=True, exist_ok=True)
        for (effect, param), rows in sorted(self.packets.items()):
            name = EFFECT_NAMES.get(effect, f"effect_{effect:02x}")
            path = TESTDATA_DIR / f"observed_{name}_p{param:02x}.csv"

            # Keep the last observation of each UI value: sliders emit
            # intermediate values while dragging.
            deduped = {}
            for ui, payload_hex in rows:
                deduped[ui] = payload_hex

            with path.open("w", newline="", encoding="utf-8") as fh:
                w = csv.writer(fh)
                w.writerow(["ui_value", "payload_hex"])
                for ui in sorted(deduped, key=lambda x: (x is None, x)):
                    w.writerow(["" if ui is None else f"{ui:.4f}", deduped[ui]])

            print(f"[host] wrote {path.relative_to(REPO)} ({len(deduped)} distinct values)")
            if effect not in EFFECT_NAMES:
                print(f"       ^ effect 0x{effect:02x} is not in the registry — "
                      f"record it in docs/protocol.md")


def build_agent(names):
    """Concatenate agent scripts into a single Frida script.

    Each file is wrapped in an IIFE. They are written to be loaded separately by
    the frida CLI (`-l rode.js -l log-encoders.js`), where each gets its own
    scope, and several declare the same top-level `const R`. Concatenating them
    raw would be a redeclaration SyntaxError. They communicate through
    globalThis.RODE, which an IIFE does not hide.
    """
    parts = []
    for name in names:
        body = (HERE / name).read_text(encoding="utf-8")
        parts.append(f"// ---- {name} ----\n(function () {{\n{body}\n}})();\n")
    return "\n".join(parts)


def sha256_of(path):
    if not path:
        return None

    p = pathlib.Path(path)
    if not p.is_file():
        hint = ""
        if "..." in str(p):
            hint = "\nThat looks like a placeholder. Substitute the real path."
        sys.exit(f"--binary: no such file: {p}{hint}")

    h = hashlib.sha256()
    with p.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("mode", choices=sorted(MODES))
    ap.add_argument("--process", default=PROCESS_NAME,
                    help=f"target process name (default: {PROCESS_NAME})")
    ap.add_argument("--binary", help="path to RODE Connect.exe, to record its SHA-256")
    args = ap.parse_args()

    if frida is None:
        sys.exit("frida not installed. pip install frida-tools (64-bit Python)")

    source = build_agent(MODES[args.mode])

    collector = Collector(sha256_of(args.binary))
    if args.binary:
        print(f"[host] binary sha256: {collector.binary_sha}")
    else:
        print("[host] no --binary given; the executable's hash is unchecked. "
              "The hooked addresses are meaningless for any other build.", file=sys.stderr)

    try:
        session = frida.attach(args.process)
    except frida.ProcessNotFoundError:
        sys.exit(f"process {args.process!r} not found. Is RØDE Connect running?")

    script = session.create_script(source)
    script.on("message", collector.on_message)
    script.load()

    print("[host] agent loaded. Ctrl-C to stop and write results.")
    try:
        sys.stdin.read()
    except KeyboardInterrupt:
        pass
    finally:
        print()
        collector.flush_packets()
        try:
            session.detach()
        except frida.InvalidOperationError:
            pass

    print(f"[host] done ({sum(len(v) for v in collector.packets.values())} packet(s))")


if __name__ == "__main__":
    main()


