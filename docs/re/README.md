# Reverse engineering the NT-USB Mini's DSP protocol

RØDE publish no specification for the NT-USB Mini's on-board processing, and
their control application, RØDE Connect, runs only on Windows and macOS. This
directory records how the protocol was recovered so that `rode-dsp` could drive
the microphone from Linux. The work was done with LLM coding agents connected to
reverse-engineering tools over MCP.

All of the capture, debugging and testing was done on Windows, where RØDE
Connect runs, so that its behaviour could be observed directly. The Linux
version was refined once the Windows one worked.

Nothing from RØDE Connect is in this repository: no binary, and no decompiled or
disassembled code. What is here is the method, the protocol facts it produced,
and the scripts written along the way. The purpose throughout was
interoperability: making a microphone the author owns work on an operating
system its vendor does not support.

| File | Contents |
|---|---|
| this file | tools, workflow, and what each stage found |
| [`protocol.md`](protocol.md) | the USB wire format |
| [`encoders.md`](encoders.md) | how UI values become DSP coefficients, and back |
| [`open-questions.md`](open-questions.md) | what is still unknown |

## Toolchain

| Job | Tool |
|---|---|
| Static analysis of the Windows application | [Ghidra](https://ghidra-sre.org/) 11.4 |
| Giving an LLM agent access to Ghidra | [GhidrAssistMCP](https://github.com/symgraph/GhidrAssistMCP), an MCP server running inside Ghidra |
| Agents | Claude Code and DeepSeek |
| Recovering class layouts and vtables | the public [JUCE](https://github.com/juce-framework/JUCE) source, version 7.0.9 |
| Capturing USB traffic | Wireshark with USBPcap on Windows; `tshark` for scripted analysis |
| Checking formulas against the running application | [Frida](https://frida.re/), scripts in [`tools/frida/`](../../tools/frida/) |
| Reading and writing the device directly | `rode-dsp status`, `probe-effects`, `read-raw`, `send-raw` |

Ghidra showed *why* the application sends what it sends. USB captures, Frida and
the microphone itself showed *what* is actually sent. No formula was accepted
until two of these agreed.

## Workflow

### 1. Capture USB traffic

USBPcap recorded RØDE Connect talking to the microphone while each slider was
swept through its range, with every effect enabled and then disabled. DSP
commands are HID `SET_REPORT` control transfers, which this filter isolates:

```
usb.bmRequestType == 0x21 && usb.setup.bRequest == 0x09
```

The agents worked through the captures with `tshark` rather than the Wireshark
GUI. This command pairs each outgoing packet with the device's reply, which is
what showed that the burst of apparently empty packets sent at startup were
reads, not resets:

```
tshark -r capture.pcapng -T fields \
  -e frame.number -e usb.endpoint_address.direction \
  -e usb.data_fragment -e usb.capdata \
  | awk -F'\t' '$3 != "" || $4 != ""'
```

This stage produced the packet layout, the effect and parameter IDs, and
several hundred `(UI value, payload)` pairs. Those pairs are
`internal/protocol/testdata/baseline_v1.csv`, and
`internal/protocol/capture_oracle_test.go` still tests the encoders against
them.

### 2. Static analysis with Ghidra over MCP

Captures give sample points, not the formulas behind them, and some parameters
were only ever captured over part of their range. For the formulas, Ghidra
analysed the application, and GhidrAssistMCP exposed that analysis to the agents
as MCP tools (function search, cross-references, decompilation, data reads):

```json
{
  "mcpServers": {
    "ghidra": { "type": "http", "url": "http://127.0.0.1:8080/mcp" }
  }
}
```

GhidrAssistMCP was chosen over the alternatives because it runs inside Ghidra
with no separate bridge process, and exposes about 50 tools rather than
several hundred. It also ships with script execution and program export
disabled, which matters when analysing a proprietary binary that must not leave
the machine. The setup was hardened accordingly:

- The server bound to `127.0.0.1` only, with an inbound firewall rule on the
  port. It has no authentication.
- Script execution, file import and program export stayed disabled throughout.
  With script execution enabled, the server would let the model run arbitrary
  code on the machine.
- A packet capture on first launch confirmed that nothing left the host.

Keeping the context window under control mattered as much as the tools did:

- Function listings were always filtered and paginated.
- Decompilation was requested only for functions already known to be small. The
  large UI constructors were reached through cross-references instead.
- Decompile requests go through an asynchronous queue, so they were submitted in
  batches and collected together.
- Findings were written to notes as the work went, so any one agent transcript
  could be discarded.

The starting points were the numbers already known from the captures. Searching
for float constants such as `255.0`, `48000.0` and `-60.0` led to the four
per-effect encoder functions. From there, cross-references led to the packet
builder, the matching reader functions, and a firmware-version check that
chooses between two fixed-point scales. What came out is written up in
[`encoders.md`](encoders.md) as mathematics rather than code.

The executable had no symbols, but its RTTI had survived: 337 JUCE class names.
Ghidra's RTTI analyser recovered those class names automatically.

### 3. Using the JUCE source

RØDE Connect statically links JUCE 7.0.9; a `"JUCE v7.0.9"` string in the binary
gives the version. JUCE is open source, so its headers could settle questions
the binary left open. The class declarations in the public repository gave the
virtual-method order, and from that the vtable slots:

| Class | Slot | Method | Used for |
|---|---|---|---|
| `juce::Component` | `0x58` | `setVisible` | following panel visibility logic |
| `juce::InputStream` | 3 | `read` | parsing device replies |
| `juce::InputStream` | 8 | `readInt` | little-endian coefficients in replies |

The same headers settled the hidden-panels question: `dontSendNotification` is
defined as `0`, so a slider set with it notifies no listener (see Q5 in
[`open-questions.md`](open-questions.md)).

### 4. Lookup tables

Four of the fifteen parameters index a 256-entry table of 32-bit coefficients
rather than evaluating a formula. The captures covered only 40–91 entries of
each, and the earlier implementation interpolated between them. The complete
tables were found in the application's read-only data. The method:

- Split the candidate region into runs of monotonically increasing or
  decreasing values. This gave exactly seven runs of 256 words.
- Check every captured value for membership in each run. Each capture set
  matched exactly one table, 100 % of its values, with no other table above
  2 %.
- Read the same tables from the live process with Frida. The two readings
  agreed byte for byte.

The tables are in `internal/protocol/luts/`. They are the only data taken from
the application, and they are what the microphone needs to be sent.

### 5. Checking the result against the running application

Frida hooked the application's four encoder functions and its packet builder,
then logged each UI value next to the packet it produced. That gave exact
`(input, output)` pairs over the full range of every slider. It also answered
two questions the captures could not: indexes are truncated, not rounded, and
nothing is clamped.

Two Frida API changes had to be worked around:

- Frida 17 removed the static `Module` lookups. `rode.js` tries the current API,
  then the old one, then a scan of the loaded modules.
- The Windows x64 ABI passes floats in XMM registers, which Frida's CPU context
  did not expose until 17.16. The hooks therefore wrap each function in a typed
  `NativeFunction` and let Frida marshal the arguments itself.

```
frida -n "RODE Connect.exe" -l tools/frida/rode.js -l tools/frida/log-encoders.js
python tools/frida/host.py log-encoders
```

The function addresses in `rode.js` are specific to one build of RØDE Connect,
SHA-256 `c3015816eacc3360d5b935a08fd05f4e948105604a2f36b812b9d58262a1023c`.

### 6. Asking the microphone

Some questions only the hardware could answer. Does the firmware have blocks
for the equaliser, high-pass filter and de-esser that the application hides?
What is the undocumented parameter that reads `0x1f`? `rode-dsp probe-effects`
and `read-raw` write to the device and read back, and their findings are in
[`open-questions.md`](open-questions.md).

## Results

- Every parameter of all four processors is encoded exactly as RØDE Connect
  encodes it. The one exception is a single least-significant bit at a few
  noise gate settings, from float32 versus float64 arithmetic.
- Reading back from the microphone works, so `rode-dsp status` reports what the
  device actually holds rather than what was last saved.
- The equaliser, high-pass filter and de-esser do not exist in this
  microphone's firmware. The application's hidden panels for them are
  display-only.
