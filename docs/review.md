# Code review — rode-dsp

Reviewed at `1c6a227`, against the current Go toolchain (1.24.7) and the
project's own goals: a cross-platform CLI + local web GUI for the RØDE NT-USB
Mini's Aphex DSP.

The reverse-engineering work is the strong part of this repository. The
protocol, encoders and lookup tables are documented, pinned against captures
and tested (`capture_oracle_test.go`, `baseline_test.go`), and `internal/dsp`
and `internal/protocol` compile without cgo so that correctness layer is
verifiable anywhere. Nothing below touches that.

The findings are concentrated in three places: the web server's exposure model,
the HID device lifecycle, and the CLI's "apply everything" path.

---

## 1. Findings

### 1.1 The WebSocket origin check does not check the origin — HIGH

`internal/server/server.go:22`

```go
CheckOrigin: func(r *http.Request) bool {
    host := r.Host                 // ← the server's own address
    if i := strings.LastIndex(host, ":"); i >= 0 {
        host = host[:i]
    }
    return host == "localhost" || host == "127.0.0.1"
},
```

`r.Host` is the `Host` header — the address the client dialled, i.e. *this
server*. It is `localhost:8080` for every request that arrives, whoever sent it.
The `Origin` header — the only thing that identifies the calling page — is never
read. The check therefore returns `true` unconditionally for any request that
reaches the port.

Concretely: a page on `https://evil.example` can run
`new WebSocket("ws://localhost:8080/ws")`, the browser attaches
`Origin: https://evil.example` and `Host: localhost:8080`, the check passes, and
that page then has the full control surface — `set_param`, `set_enable`,
`import_state`, `save_preset`, `delete_preset` — over the microphone and over
`config.json`/`presets.json` on disk.

The sharp edge is that **deleting this function entirely is strictly safer than
keeping it**. gorilla/websocket's documented default when `CheckOrigin` is nil
is to fail the handshake when `Origin` is present and its host differs from
`Host` — which is exactly the check this code was trying to write. The custom
function overrides that default with one that always passes.

Fix — read the right header:

```go
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    if origin == "" {
        return true // non-browser client; no ambient authority to abuse
    }
    u, err := url.Parse(origin)
    if err != nil {
        return false
    }
    return strings.EqualFold(u.Host, r.Host)
},
```

`u.Host` keeps the port, so `http://localhost:8080` matches and
`http://localhost:9999` does not. Or drop the field and take the library
default.

Note the port-stripping is independently wrong for IPv6: `strings.LastIndex(host, ":")`
on `[::1]:8080` yields `[::1]`, which matches neither literal. `net.SplitHostPort`
is the right tool. Moot once the check reads `Origin`, but the same mistake
recurs anywhere hosts are split by hand.

### 1.2 The GUI listens on every interface — HIGH

`internal/server/server.go:257`

```go
addr := fmt.Sprintf(":%d", s.port)
```

An empty host binds `0.0.0.0` (and `::`), not loopback. The readme says "web UI
on localhost" and every log line says `http://localhost:8080`, but the listener
is reachable from the whole LAN. There is no authentication of any kind — the
security model is entirely "only local processes can reach it", and that
premise does not hold.

Together with 1.1 this is one issue, not two: anyone on the network can drive
the microphone directly, and any web page the user visits can drive it through
their browser.

Fix:

```go
host := "127.0.0.1"                       // default
addr := net.JoinHostPort(host, strconv.Itoa(s.port))
```

and add a `--host` flag for the deliberate case (`--host 0.0.0.0`), so exposing
it is a choice someone typed rather than the default. Log the address actually
bound rather than a hardcoded `localhost`.

### 1.3 `client.send` is written from two goroutines, one of which may have closed it — MEDIUM

`internal/server/server.go` — `sendError`, `sendState`, `sendPresets`,
`handlePreview` all do `client.send <- msg` from the **readPump** goroutine,
while the hub closes that same channel:

```go
case message := <-h.broadcast:
    for client := range h.clients {
        select {
        case client.send <- message:
        default:
            close(client.send)          // ← hub closes it
            delete(h.clients, client)
        }
    }
```

Two consequences:

- **Panic.** Once the hub drops a slow client and closes `send`, the next
  message that client sends produces `client.send <- …` on a closed channel:
  `panic: send on closed channel`, which takes down the whole process — server,
  HID connection and all. The client does not have to be malicious; a browser
  tab that stops draining (backgrounded, suspended laptop) while advanced-mode
  previews stream in is enough to fill the 256-slot buffer.
- **Goroutine leak.** Even without the close, a full buffer blocks the readPump
  goroutine on the channel send. It is no longer reading, so the read deadline
  never fires; nothing ever unblocks it.

The canonical gorilla hub avoids this by making the hub the *only* writer to
`send`. Either route per-client messages through the hub, or keep direct writes
but make them non-blocking and let the hub be the only closer:

```go
func (c *Client) trySend(b []byte) {
    select {
    case c.send <- b:
    default:
        c.hub.unregister <- c   // hub closes and deletes; never close here
    }
}
```

with `unregister` made idempotent (it already checks map membership) and the
`default:` branch in `broadcast` changed to `delete` + `close` exactly once.

### 1.4 Data race on the hub's client map — MEDIUM

`internal/server/server.go:137,148`

```go
h.mu.Lock()
h.clients[client] = true
h.mu.Unlock()
if len(h.clients) == 1 {   // ← read outside the lock
```

Same pattern in the `unregister` case. `len` on a map concurrently written by
the broadcast branch is a race the runtime detector will flag. Move the read
inside the critical section:

```go
h.mu.Lock()
h.clients[client] = true
n := len(h.clients)
h.mu.Unlock()
if n == 1 { … }
```

Also worth noting: `mu` is an `RWMutex` but `RLock` is never called — the
broadcast branch takes the write lock. Either use `RLock` for the read paths or
make it a plain `Mutex`, so the type does not imply a concurrency story the code
does not have.

### 1.5 `load` and `defaults --send` cannot turn an effect off — MEDIUM (functional)

`internal/hid/device.go` — `SendAllParams`:

```go
if state.IsEnabled(effID) {
    packet := protocol.BuildEnablePacket(effID, true)
    …
}
// no else: a disabled effect sends no packet at all
```

The microphone keeps whatever enable state it already had. So:

- `rode-dsp defaults` resets the config to all-effects-off, prints "New default
  state: … DISABLED", and leaves the compressor running on the device. The
  readme advertises this command as "reset everything".
- `rode-dsp load` with a config that disables an effect the device currently has
  enabled silently fails to disable it.
- `rode-dsp connect` has the same gap.

The server already knows about this — `applyStateToDevice` exists precisely to
fix it and its doc comment explains why `SendAllParams` is wrong for the job —
but the fix never made it back to the CLI. The result is that the GUI and the
CLI apply the same config to the same device and get different outcomes.

Fix: move `applyStateToDevice`'s logic into `internal/hid` (it belongs there —
it is a device operation, not a server one) and have both callers use it.
`SendAllParams` can keep its RØDE-Connect-mimicking ordering for the handshake
path, but it should not be what "apply my settings" resolves to.

While there: `SendAllParams` sends the enable packet *before* the parameters,
so an effect switching on processes with the old values for the duration of the
burst. `applyStateToDevice` deliberately reversed that. Same reasoning applies.

### 1.6 Disconnect swaps out the channels its goroutines are still using — MEDIUM (latent)

`internal/hid/device.go:120`

```go
close(d.done)
time.Sleep(20 * time.Millisecond)   // "don't wait long"
…
d.done = make(chan struct{})        // ← replaced while goroutines may still run
d.workerDone = make(chan struct{})
d.readDone = make(chan struct{})
```

`workerDone` and `readDone` are created and closed but nothing ever waits on
them, so the 20 ms sleep is the entire synchronisation. If a goroutine has not
exited within it — quite likely, since `sendPacketSync` holds `mu.RLock` across
a write plus up to a 50 ms ACK wait, and `Disconnect` holds the write lock the
whole time — then:

- the old goroutine's `defer close(d.workerDone)` closes the *new* channel
  (fields are read at defer-execution time), so a later disconnect panics with
  `close of closed channel`;
- the old `readLoop` selects on the *new*, still-open `done` and never exits,
  leaving two readers competing for ACKs after a reconnect;
- the field reads and writes themselves race.

Replace the sleep with real synchronisation:

```go
close(d.done)
d.mu.Unlock()          // let the goroutines make progress
<-d.workerDone
<-d.readDone
d.mu.Lock()
```

or hold the goroutine handles in a `sync.WaitGroup` captured per-connection, so
each generation of goroutines closes over its own channels rather than reading
mutable fields.

This is latent today because the CLI is one-shot and the GUI never disconnects,
but any future reconnect-on-replug support walks straight into it.

### 1.7 `readLoop` has no backoff on error — LOW

`internal/hid/device.go:396`

```go
n, err := dev.ReadWithTimeout(buf, 10*time.Millisecond)
if err != nil {
    // Timeout is expected and not an error
    continue
}
```

The comment describes the benign case. The malignant one is a persistent error —
device physically removed while the GUI is running — where the read returns
immediately every time and this becomes a spin loop pinning a core, with no
`select` on `done` on the fast path either. A short sleep on consecutive errors,
and giving up after N of them (marking the device disconnected, which the GUI
can then report), covers both.

### 1.8 Errors from queued sends are discarded — LOW

`Device.Send` enqueues and returns `nil`; `workerLoop` calls `sendPacketSync`
and throws away its bool; `sendPacketSync` returns `true` on ACK timeout by
design ("assuming success"). Nothing in the chain can report failure, so
`rode-dsp comp --threshold -20` prints `Sending updates to device... OK` even
if every write failed with `EIO`.

Given the exit-code contract in the readme (0 applied / 1 failure / 2 not
connected), "applied" currently means "enqueued". A result channel per packet,
or an error field on the device that `Flush` returns, would make the contract
true. The "assume success on ACK timeout" behaviour is fine to keep — an
observed write error is a different thing from a missing ACK.

### 1.9 `disconnect` is a no-op — LOW

`internal/cli/connect.go` — each CLI invocation is a fresh process with a fresh
`Context`, so `ctx.Device.Connected()` is always `false` and the command always
prints "Device is not connected." and returns. There is no daemon for it to talk
to. Either remove it or make it mean something (e.g. `defaults --send` +
release).

### 1.10 A 10 MB build artifact is committed, and the installer prefers it — LOW/HIGH depending on your luck

`rode-dsp` (10 MB, mode 755) and `RODE Connect.exe` (39 MB) are both tracked;
`.gitignore` covers neither. The pack is 28 MB.

The one with teeth is `rode-dsp`, because `packaging/linux/install.sh` does:

```sh
[ -x "$REPO/rode-dsp" ] || die "no rode-dsp binary in $REPO -- run 'go build' first"
…
install -Dm755 "$REPO/rode-dsp" "$BIN_DIR/rode-dsp"
```

The guard passes on a fresh clone because the committed binary is there and
executable. So `git clone && sudo ./packaging/linux/install.sh` installs
whatever binary happened to be committed — a Linux/amd64 build from an unknown
tree — into `/usr/local/bin`, with no build step and no error. On a
non-amd64 host it installs an unrunnable file; whenever the committed binary
lags `main`, it silently installs stale code.

Add `/rode-dsp` to `.gitignore`, `git rm --cached` it, and ship releases as
attached artifacts instead. Consider whether redistributing `RODE Connect.exe`
is something you want in the repository at all — it is RØDE's copyrighted
binary, the RE docs already pin it by SHA-256, and `tools/frida/` documents how
to obtain it.

### 1.11 Smaller items

| Where | Issue |
|---|---|
| `internal/cli/root.go:88` | `Usage()` iterates `commands`, a map — help output lists commands in a different random order on every run. Sort by name. |
| `internal/hid/device.go:57` | `initOnce.Do` sets `initErr` only on the first call. If `gohid.Init()` fails, every later `Connect()` skips the `Do` body, sees a nil `initErr`, and proceeds as if the library were initialised. Store the error in a field. |
| `internal/hid/device.go` | `gohid.Exit()` is never called. |
| `internal/hid/device.go:65` | `Enumerate` runs purely to count, then `OpenFirst` enumerates again. Take the path from the first enumeration and `Open` it. |
| `internal/hid/device.go` | `Flush` polls `len(sendQueue)` then sleeps 50 ms hoping the in-flight packet lands. A done-signal per packet removes the guess. |
| `internal/hid/device.go` — `ReadRaw` | On timeout the spawned goroutine is left blocked in `Read` forever, still writing into `buf`. |
| `internal/dsp/config.go`, `presets.go` | The atomic write is `WriteFile` + `Rename` with no `Sync` before the rename, so a crash can leave the rename durable and the contents not. The temp name is fixed (`config.json.tmp`), so `fileMu` — a *process*-local mutex — does not stop the CLI and a running GUI from colliding. `os.CreateTemp(dir, "config-*.json")` + `f.Sync()` fixes both. |
| `internal/protocol/packet.go:12` | `copy(packet[4:], value)` silently truncates an over-long value. The callers are all internal, but a length check costs one line. |
| `internal/cli/gui.go:88` | `time.Sleep(500 * time.Millisecond)` before opening the browser races the listener. Bind the listener in `Start`, hand back the resolved address, open the browser after that. |
| `internal/cli/gui.go:97` | On SIGTERM the process just returns — no `server.Shutdown(ctx)`, so in-flight writes and WebSocket closes are cut off. `Start` doesn't expose the `*http.Server`, so shutdown isn't currently possible. |
| `main.go:24` | `--version` is hardcoded `v0.1.0`. `debug.ReadBuildInfo()` (or `-ldflags -X`) gives a real version, and includes the VCS revision for free. |
| `main.go:48` | The `init()` that rewrites `os.Args[0]` mutates global state to make usage text prettier. A package-level `progName = filepath.Base(os.Args[0])` in `cli` does the same without the side effect. |
| `internal/cli/context.go:63` | `...interface{}` — `any` since Go 1.18. Same in `state.go`'s `map[string]interface{}`. |
| everywhere | No tests for `internal/server` or `internal/hid`. 1.1 and 1.4 are both things a handler test and `go test -race` would have caught. `httptest` + a `gorilla/websocket` client needs no hardware; a small `hid` interface would let the server tests run against a fake device. |

---

## 2. Cross-platform and packaging

The portable parts are right, and worth saying so: `os.UserConfigDir` rather than
hand-rolled `$HOME` logic, `filepath.Join` throughout, `runtime.GOOS` (not `%OS%`)
for the browser opener, `embed` for the static assets so there is one file to
ship, and correct handling of `cmd /c start ""`'s title argument.

The gaps:

**CI only builds one platform.** `.github/workflows/ci.yml` runs
`ubuntu-latest` twice. For a project whose pitch is "RØDE Connect is Windows and
macOS only; this works anywhere", macOS and Windows are exactly the platforms
where a break would be invisible until a user reports it. cgo means you cannot
cross-compile, but you can matrix:

```yaml
strategy:
  matrix:
    os: [ubuntu-latest, macos-latest, windows-latest]
runs-on: ${{ matrix.os }}
```

hidapi needs no extra dependency on macOS (IOKit) or Windows; only the Linux
job needs the `libudev-dev` step, which `if: runner.os == 'Linux'` handles.
That also gives you release binaries for the three platforms from tags.

**No static analysis beyond `go vet`.** `staticcheck` or `golangci-lint` would
flag several items above (the `len()`-outside-lock, the unused `RWMutex` read
path, `interface{}`) without anyone having to look.

**`go.mod` says `go 1.22`, CI uses 1.24.** Not wrong — the directive is a floor —
but it means the module opts out of 1.23+ language and vet behaviour for no
stated reason. `golang.org/x/sys v0.8.0` is a 2023 release; `go get -u` is
overdue.

**Non-systemd Linux is documentation-only.** The readme tells Alpine/Void/OpenRC
users to swap `uaccess` for `GROUP="plugdev"` by hand. Shipping the alternative
rule file as `70-rode-nt-usb-mini-plugdev.rules` and an `--group` installer flag
turns a paragraph into a command.

**No macOS or Windows packaging at all.** On macOS the microphone's HID
interface is claimed by the kernel unless the vendor page is non-standard —
worth a note in the readme either way, since "it built" and "it can open the
device" are different questions there. On Windows, HID access needs no
privileges, so that platform is likely the easiest and is currently undocumented.

---

## 3. Environment variables

The current surface is one variable, `RODE_DSP_CONFIG`, read once in
`DefaultConfigPath`:

```go
if p := os.Getenv(ConfigEnvVar); p != "" {
    return p, nil
}
```

The precedence — `--config` beats `$RODE_DSP_CONFIG` beats the platform default —
is the conventional one and is correctly implemented (the flag is checked in each
command before the env var is ever consulted). Treating empty-as-unset is the
right call here: an empty `RODE_DSP_CONFIG` is a shell accident, not a request to
open `""`, so `LookupEnv`'s extra information would not change what you do with
it. `LookupEnv` earns its keep for *required* config that must fail fast, which
this isn't.

What would improve it:

- **Document it in `--help`, not only in the readme.** `Usage()` never mentions
  `RODE_DSP_CONFIG`. An `Environment:` section next to `Examples:` costs three
  lines.
- **Make the systemd unit use it.** The unit currently carries `@CONFIG@`
  substituted by `sed` in two places, plus a comment explaining why `%h` cannot
  be used. `Environment=RODE_DSP_CONFIG=/path` in a drop-in
  (`/etc/systemd/system/rode-dsp.service.d/config.conf`) would leave the shipped
  unit a static file — no generation step, and `systemctl edit` becomes the
  supported way to change it. `ConditionPathExists=` still needs the literal
  path, so one substitution remains, but the `ExecStart` line stops carrying
  configuration.
- **Resolve `~` and relative paths.** `RODE_DSP_CONFIG=~/foo.json` set from a
  config file rather than an interactive shell arrives with a literal `~`;
  `ConfigPath` calls `filepath.Abs`, which turns it into `$PWD/~/foo.json`.
  Either expand it or reject it with a clear message.
- **If more variables appear, keep them in one place.** A single `env.go` in
  `internal/dsp` listing every variable, its default and its meaning, is what
  keeps an env surface documentable. Reach for `koanf`/`viper` only if layered
  config files ever become a requirement — for one variable and a flag, the
  standard library is the correct answer and adding a dependency would be a
  regression.
- **`NO_COLOR`** is worth honouring if the CLI ever grows colour output.

One thing to be careful of if tests ever cover this: `os.Setenv` is not
goroutine-safe, which is why `t.Setenv` refuses to run under `t.Parallel()`.
`paths_test.go` already uses `t.Setenv` — keep it that way.

---

## 4. Package layout and dependencies

The layout is sound. `internal/` correctly prevents anything outside the module
importing these packages; the split is by role (`protocol` = wire format,
`dsp` = registry and state, `hid` = transport, `cli` = commands,
`server` = GUI) rather than by kind (`models`, `utils`), which is the idiomatic
cut. The dependency direction is acyclic and points inward, and the deliberate
decision to keep `protocol` and `dsp` cgo-free so the correctness tests run
without libudev is a genuinely good piece of design — it is what makes the
lookup tables verifiable on any machine, including CI's second job.

Two dependencies, both well-chosen: `sstallion/go-hid` (maintained hidapi
binding) and `gorilla/websocket` (back under active maintenance since 2023, and
still the reference implementation). No dependency bloat to speak of.

Improvements:

- **`internal/hid` has no interface.** `Server` and `Context` both hold a
  concrete `*hid.Device`, so neither can be tested without hardware — which is
  most of why `internal/server` has no tests. A three-method interface
  (`Send`, `Connected`, `ReadState`) declared *at the consumer* (in `server`,
  per Go convention) would open both packages up to testing with no change to
  the production wiring.
- **`applyStateToDevice` lives in the wrong package** (see 1.5) — it is device
  behaviour sitting in the HTTP layer, which is why the CLI does not benefit
  from it.
- **`log` vs `log/slog`.** The server uses the standard `log` package and
  `--quiet` implements itself as `log.SetOutput(io.Discard)` — a global mutation
  from a command handler. `log/slog` (stdlib since 1.21) gives per-logger levels,
  so `--quiet` and `--debug` become `slog.LevelWarn`/`LevelDebug` on a logger the
  server is handed, and `d.debug` in `internal/hid` — currently ~15 `if d.debug`
  blocks with `fmt.Printf` — collapses into `logger.Debug(...)` calls that route
  to stderr with the rest. That also fixes a smaller problem: `internal/hid`'s
  debug output goes to **stdout**, where it interleaves with `status --json`'s
  machine-readable output.
- **`ParamCounts` in `protocol` vs `Effects` in `dsp`** encode overlapping facts
  (how many parameters each effect has) in two packages that can drift.
  `ReadState` iterates `ParamCounts`; everything else iterates `Effects`. A test
  asserting they agree would be cheap; the AE 0x03 exception is already
  documented, so it would need one allowance.

---

## 5. What research says about the device

Little external to lean on. RØDE documents the NT-USB Mini's DSP only as a
feature list — compressor, noise gate, Aphex Aural Exciter and Big Bottom,
"unlocked using RØDE Connect" — and publishes nothing about the HID protocol.
The nearest prior art is `Jordan-Milner/rodecaster-control`, an open-source Linux
alternative to RØDE Central for the RODECaster Pro II; it uses hidapi from
Python and covers a different device with a different protocol, but it is
evidence for how these devices are approached and a reasonable place to compare
notes on RØDE's HID conventions. I found no other open-source NT-USB Mini DSP
tool, which makes this repository's `docs/re/` the most complete public
description of that protocol I can find.

That raises the value of two things already in the repo: the SHA-256 pin on the
binary the encoders were read from, and the firmware caveat (Q31 above 2.1.2,
Q16 at or below). Both are the kind of detail that lets someone else reproduce
or extend the work, and they are the reason the accuracy claims in the readme
are checkable rather than asserted.

---

## 6. Suggested order of work

1. `CheckOrigin` reads `Origin`; bind to loopback with an opt-in `--host`. (1.1, 1.2)
2. Untangle `client.send` ownership; move `len(h.clients)` inside the lock. (1.3, 1.4)
3. Share one apply-state path between CLI and GUI so `defaults` and `load` can disable an effect. (1.5)
4. `.gitignore` the built binary, `git rm --cached` it, and make `install.sh` require a fresh build. (1.10)
5. CI matrix across Linux/macOS/Windows, plus staticcheck. (§2)
6. Introduce a device interface and add `internal/server` tests, including an origin-rejection test. (§4)
7. The device lifecycle work — proper goroutine shutdown, read backoff, propagated send errors. (1.6–1.8)
