#!/usr/bin/env python3
"""Apply the independently versioned native-inbound overlay to one clean pin.

The output is a separate runtime artifact. This script never installs a core
or modifies a running service. Upstream source is MPL-2.0; retain its license.
"""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

PIN = "701af60772cda123492f86e383e1fdba066614a9"
HASHES = {
    "transport/internet/xdrive/xdrive.go": "b81a0b502bdeb8baf732467f1a714b1039e3ee16fd9969211c2c986bd05864cc",
    "app/dispatcher/default.go": "29100330a020d75b5f4b77b8eec20779960a8768e765c99c1003e2b1475746fb",
    "app/dispatcher/stats.go": "e768aeb68b100eb980c7ea22a4cfbce474b250ed3026bf13d133948d0a910e46",
    "app/proxyman/inbound/always.go": "f64783e9a8d256348953fb62afe211482f2df60e48f8bf643b77a2cfa9b6c0fe",
    "app/proxyman/inbound/worker.go": "670e88f15842e56b9b6cfd681618d2c737c34c4f9debf850935fb52fb6e196e6",
    "app/stats/command/command.go": "d43685fff16f6b6034233879f59fe1146978cb7413efc49b40a3a5bc9969da0d",
    "common/buf/io.go": "47b92abf0d9981f51126d05eeb954b1f79f2b048db311d04d4d79e4ab519664e",
    "common/mux/server.go": "3e18d91c275e04bb5073e9cbe0fd177f7ae471f07c87c420c078490fe4e8a765",
    "common/task/task.go": "fe387a8e47158aefc6135505861245401c7c49d7df60759f25f61761453fcb0e",
    "core/xray.go": "66edf1a9ed6b9683b4f668612e6c6e876830cffb07521517f03a21e630f7ef15",
    "proxy/freedom/freedom.go": "da88b4394315870465f44684b58f1bc4177f12d440a3d4e7a4747a7de1332d88",
    "proxy/masque/server.go": "42dc16954dab9442681e6a1ad20963347bdb5e6496726c50748716813c4aa0f1",
    "proxy/proxy.go": "e125a49cac5330df937772142960955d4af4fce29c8208a04c9603e20f42d031",
    "proxy/shadowsocks_2022/shadowsocks_2022.go": "c7166b75309370789b6a893ccf27ec1b8e069a7ef3a9b605c409e8843a3a4e37",
    "proxy/socks/server.go": "6247a63dc3d4d14f7e0d0cd52582692e9507bb85970ae3bdb67dd89537e1c363",
    "proxy/tun/handler.go": "f75a647c784144a7e6d62aa7bdca2c72fa742e0a10795f6a076debbf7d4ac67c",
    "proxy/wireguard/client.go": "2c51a38a43fad721b6065ee3e5b8dd71640a1fd49a8a35584b8fceb43ae28670",
    "proxy/wireguard/server.go": "fa15ac6d1ddd9a8ff8c9a9ec8da98371ad20a941d6fdfe43af0b2738097ccbc0",
    "proxy/vless/inbound/inbound.go": "085d5050213104dda60f5385982372e062541d7e0ff3a1d58f9e42d33b06b84a",
    "proxy/vless/outbound/outbound.go": "d9e9d56ba68ee3ed5ae748c96575f238ecb04fc00a9a431066f582bfaa2fceac",
    "proxy/vmess/outbound/outbound.go": "18482f88800fad1d11bd287baa15028c60d37497769ffb8ffb4c6ec2e80cfa31",
    "proxy/trojan/client.go": "84540ed82a8dee860be381b8d9d0b53d6844b9f877b18e830d1e621f9261ec38",
    "proxy/shadowsocks/client.go": "0193b22af44bfeb3c84d5327cf93b65111f3f5dd48be2cee3c0bc60ff94d852e",
    "proxy/shadowsocks_2022/outbound.go": "2aa485f49d73791d692df9585ad52dc368463913d9f6a8837e4612026d6e135a",
    "proxy/socks/client.go": "10703a673c0f7ce689297cba49f2e07a4b9e78e23a69ce80c2401d1972d76546",
    "proxy/http/client.go": "d64c6dbc2681ba37b38cb262cd6f27a44289fa140186d24bc8c4c570840b330c",
    "proxy/hysteria/client.go": "d22797807769355d633b51e25e72ba08877b99aa60cee70042b24754ad5ac4f0",
    "proxy/masque/client.go": "611df9d343d65d2282600d872cc04af9db66728f55632a26af3328a3c87008fe",
    "transport/internet/stat/connection.go": "82972523365445ac9a10d8f5c821b4291b6c62205f602b1a173767f8c23a875d",
    "transport/internet/udp/dispatcher.go": "76bdf02b75c5c71c424bc177c4f6284d15b4b23c271b2b5653de0ced61b86dfe",
    "transport/internet/hysteria/conn.go": "eb63ebfa129add02bdf54b58f593382ff30e19b3a2e0f250b4ae8d8ad23a5aa1",
}

if len(sys.argv) != 2:
    raise SystemExit("usage: prepare-inbound-core.py PINNED_XRAY_CHECKOUT")
source = Path(sys.argv[1]).resolve()
repo = Path(__file__).resolve().parent.parent
if subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip() != PIN:
    raise SystemExit("wrong upstream commit")
if subprocess.check_output(["git", "-C", str(source), "status", "--porcelain", "--untracked-files=all"], text=True).strip():
    raise SystemExit("upstream checkout must be completely clean")
if not HASHES:
    raise SystemExit("source hash manifest is missing")
contents = {}
for name, digest in HASHES.items():
    raw = (source / name).read_bytes()
    if hashlib.sha256(raw).hexdigest() != digest:
        raise SystemExit(f"unexpected upstream file: {name}")
    contents[name] = raw.decode()


def replace(name, old, new, count=1):
    if contents[name].count(old) != count:
        raise SystemExit(f"ambiguous patch: {name}: expected {count} occurrences of {old[:100]!r}")
    contents[name] = contents[name].replace(old, new)


def after(name, anchor, extra, count=1):
    replace(name, anchor, anchor + extra, count)


def import_gate(name):
    replace(name, "import (", 'import (\n\tdarkpro "github.com/xtls/xray-core/common/darkprov2"')


for name in HASHES:
    if name not in ("proxy/vless/inbound/inbound.go", "transport/internet/hysteria/conn.go"):
        import_gate(name)

# The API can start before other features. Activation requires all starts to
# have completed, not merely a successful StatsService RPC.
after("core/xray.go", '\terrors.LogWarning(s.ctx, "Xray ", Version(), " started")', '\n\tdarkpro.MarkReady()')

command = "app/stats/command/command.go"
for method, req, resp in (("GetStats", "GetStatsRequest", "GetStatsResponse"), ("GetUsersStats", "GetUsersStatsRequest", "GetUsersStatsResponse"), ("QueryStats", "QueryStatsRequest", "QueryStatsResponse")):
    anchor = f"func (s *statsServer) {method}(ctx context.Context, request *{req}) (*{resp}, error) {{"
    extra = '\n\tif darkpro.Enabled() && request.Reset_ { return nil, status.Error(codes.FailedPrecondition, "DARK PRO counters cannot be reset") }\n'
    if method == "QueryStats":
        extra += '\tif strings.HasPrefix(request.Pattern, darkpro.Protocol) { return s.darkProInboundStats(ctx, request) }\n'
    after(command, anchor, extra)

always = "app/proxyman/inbound/always.go"
after(always, "func getStatCounter(v *core.Instance, tag string) (stats.Counter, stats.Counter) {", '\n\tif darkpro.IsControl(tag) { return nil, nil }\n')
replace(always, "\tfor _, worker := range h.workers {\n\t\tif err := worker.Start(); err != nil {\n\t\t\treturn err\n\t\t}\n\t}\n\treturn nil", "\tfor _, worker := range h.workers {\n\t\tif err := worker.Start(); err != nil { return err }\n\t}\n\tif err := darkpro.RegisterResource(h.tag, h.Close); err != nil { _ = h.Close(); return err }\n\treturn nil")

worker = "app/proxyman/inbound/worker.go"
for typ in ("tcpWorker", "dsWorker"):
    anchor = f"func (w *{typ}) callback(conn stat.Connection) {{\n\tctx, cancel := context.WithCancel(w.ctx)"
    after(worker, anchor, '''
    ownedConn := conn
    tracked, finish, admitted := darkpro.Enter(ctx, w.tag, func(){ cancel(); _ = ownedConn.Close() })
    if !admitted { cancel(); _ = conn.Close(); return }
    defer finish()
    ctx = tracked
''')
after(worker, "func (w *udpWorker) callback(b *buf.Buffer, source net.Destination, originalDest net.Destination) {", '''
    _, packetDone, admitted := darkpro.Enter(context.Background(), w.tag, nil)
    if !admitted { b.Release(); return }
    defer packetDone()
''')
replace(worker, "\tcancel           context.CancelFunc", "\tcancel           context.CancelFunc\n\tctx              context.Context")
replace(worker, "\tconn := &udpConn{", "\tconnectionCtx, cancel := context.WithCancel(w.ctx)\n\tconn := &udpConn{\n\t\tctx: connectionCtx,\n\t\tcancel: cancel,")
replace(worker, "\t\t\tctx, cancel := context.WithCancel(w.ctx)\n\t\t\tconn.cancel = cancel", "\t\t\tctx, cancel := conn.ctx, conn.cancel")
after(worker, "\t\t\tctx, cancel := conn.ctx, conn.cancel", '''
            tracked, finish, admitted := darkpro.Enter(ctx, w.tag, func(){ cancel(); _ = conn.Close() })
            if !admitted { cancel(); _ = conn.Close(); return }
            defer finish()
            ctx = tracked
''')
after(worker, "func (c *udpConn) ReadMultiBuffer() (buf.MultiBuffer, error) {", '''
    finish, admitted := darkpro.BeginIO(func(){ _ = c.Close() })
    if !admitted { return nil, darkpro.ErrBarrier }
    defer finish()
''')
after(worker, "func (c *udpConn) Write(buf []byte) (int, error) {", '''
    finish, admitted := darkpro.BeginIO(func(){ _ = c.Close() })
    if !admitted { return 0, darkpro.ErrBarrier }
    defer finish()
''')

# Inner network-stack connections use detached base contexts. Give each an
# independent cancellation/ownership root before it reaches accounting.
for name, receiver in (("proxy/wireguard/server.go", "s"), ("proxy/tun/handler.go", "t"), ("proxy/masque/server.go", "s")):
    anchor = f"\tctx, cancel := context.WithCancel({receiver}.ctx)\n\tdefer cancel()"
    after(name, anchor, f'''
    ownedConn := conn
    tracked, finish, admitted := darkpro.Enter(ctx, {receiver}.tag, func(){{ cancel(); _ = ownedConn.Close() }})
    if !admitted {{ return }}
    defer finish()
    ctx = tracked
''')

# All task.Run children are registered before they can run, including tasks
# that return after their parent has already returned a cancellation/error.
task = "common/task/task.go"
replace(task, "\t\tgo func(f func() error) {", "\t\tfinish := darkpro.Track(ctx)\n\t\tgo func(f func() error) {\n\t\t\tdefer finish()")

dispatcher = "app/dispatcher/default.go"
after(dispatcher, "\tinbound, outbound := d.getLink(ctx)", '''
    finish, cancel := func(){}, func(){}
    if darkpro.Enabled() {
        identity := session.InboundFromContext(ctx)
        if identity == nil || identity.Tag == "" { common.Interrupt(inbound.Reader); common.Interrupt(inbound.Writer); return nil, darkpro.ErrBarrier }
        if !darkpro.IsControl(identity.Tag) {
            if !darkpro.Tracked(ctx) { common.Interrupt(inbound.Reader); common.Interrupt(inbound.Writer); return nil, darkpro.ErrBarrier }
            child, stop := context.WithCancel(ctx)
            inReader, inWriter, outReader, outWriter := inbound.Reader, inbound.Writer, outbound.Reader, outbound.Writer
            tracked, done, admitted := darkpro.Enter(child, identity.Tag, func(){
                stop(); common.Interrupt(inReader); common.Interrupt(inWriter)
                common.Interrupt(outReader); common.Interrupt(outWriter)
            })
            if !admitted { stop(); common.Interrupt(inbound.Reader); common.Interrupt(inbound.Writer); return nil, darkpro.ErrBarrier }
            ctx, finish, cancel = tracked, done, stop
        }
    }
''')
replace(dispatcher, "\t\tgo d.routedDispatch(ctx, outbound, destination)", "\t\tgo func(){ defer finish(); defer cancel(); d.routedDispatch(ctx, outbound, destination) }()")
replace(dispatcher, "\t\tgo func() {\n\t\t\tcReader :=", "\t\tgo func() {\n\t\t\tdefer finish()\n\t\t\tdefer cancel()\n\t\t\tcReader :=")
after(dispatcher, "func (d *DefaultDispatcher) DispatchLink(ctx context.Context, destination net.Destination, outbound *transport.Link) error {", '''
    if darkpro.Enabled() {
        identity := session.InboundFromContext(ctx)
        if identity == nil || identity.Tag == "" { return darkpro.ErrBarrier }
        if !darkpro.IsControl(identity.Tag) {
            if !darkpro.Tracked(ctx) { return darkpro.ErrBarrier }
            child, cancel := context.WithCancel(ctx)
            ownedReader, ownedWriter := outbound.Reader, outbound.Writer
            tracked, finish, admitted := darkpro.Enter(child, identity.Tag, func(){ cancel(); common.Interrupt(ownedReader); common.Interrupt(ownedWriter) })
            if !admitted { cancel(); return darkpro.ErrBarrier }
            defer finish()
            defer cancel()
            ctx = tracked
        }
    }
''')

udp = "transport/internet/udp/dispatcher.go"
replace(udp, "\tgo handleInput(ctx, entry, dest, v.callback, v.callClose)", "\tdarkpro.Go(ctx, func(){ handleInput(ctx, entry, dest, v.callback, v.callClose) })")
ss = "proxy/shadowsocks_2022/shadowsocks_2022.go"
replace(ss, "\tgo handleUDPResponse(s, link, dest, responseEncoder)", "\tdarkpro.Go(sessCtx, func(){ handleUDPResponse(s, link, dest, responseEncoder) })")
socks = "proxy/socks/server.go"
replace(socks, "\t\tgo func() {\n\t\t\terrCh <- s.handleUDPPayload(ctx, udpConn, dispatcher)\n\t\t}()", "\t\tdarkpro.Go(ctx, func(){ errCh <- s.handleUDPPayload(ctx, udpConn, dispatcher) })")
mux = "common/mux/server.go"
for old, new in (
    ("\tgo worker.run(ctx)", "\tdarkpro.Go(ctx, func(){ worker.run(ctx) })"),
    ("\tgo worker.monitor()", "\tdarkpro.Go(ctx, func(){ worker.monitor() })"),
    ("\t\tgo handle(ctx, x.Mux, w.link.Writer)", "\t\townedSession := x.Mux\n\t\tdarkpro.Go(ctx, func(){ handle(ctx, ownedSession, w.link.Writer) })"),
    ("\tgo handle(ctx, s, w.link.Writer)", "\tdarkpro.Go(ctx, func(){ handle(ctx, s, w.link.Writer) })"),
):
    replace(mux, old, new)
masque = "proxy/masque/server.go"
replace(masque, "\tgo s.writeToTunnel(t)", "\tdarkpro.Go(ctx, func(){ s.writeToTunnel(t) })")
replace(masque, "\tgo serveAddressRequests(t)", "\tdarkpro.Go(ctx, func(){ serveAddressRequests(t) })")

# XDrive's upstream Serve only starts an asynchronous storage poller. A real
# bounded read/write/delete round trip is required before it may report ready.
xdrive = "transport/internet/xdrive/xdrive.go"
replace(xdrive, "\tlistenerCtx, cancel := context.WithCancel(context.Background())", '''
    if darkpro.Enabled() {
        if err := darkProStorageProbe(ctx, storage); err != nil { storage.Close(); return nil, err }
    }
\tlistenerCtx, cancel := context.WithCancel(context.Background())''')
contents[xdrive] += '''

// darkProStorageProbe touches only its own unpredictable sentinel. Three
// seconds are reserved for Put/Get and the remainder of the five-second total
// deadline for cleanup, including cleanup after a partially successful Put.
func darkProStorageProbe(parent context.Context, storage Storage) error {
    ctx, cancel := context.WithTimeout(parent, 5*time.Second)
    defer cancel()
    nonce, err := newSessionID()
    if err != nil { return errors.New("DARK PRO storage probe entropy unavailable") }
    name := "darkpro-probe-" + nonce
    payload := []byte("darkpro-inbound-v2:" + nonce)
    operations, stop := context.WithTimeout(ctx, 3*time.Second)
    failure := storage.Put(operations, name, payload)
    if failure == nil {
        var got []byte
        got, failure = storage.Get(operations, name)
        if failure == nil && string(got) != string(payload) { failure = errors.New("probe data mismatch") }
    }
    stop()
    cleanup := storage.Delete(ctx, name)
    if failure != nil || cleanup != nil { return errors.New("DARK PRO storage readiness probe failed") }
    return nil
}
'''

# XUDP's timeout-only mode intentionally detaches cancellation in every native
# outbound. Preserve proof ownership while keeping upstream's cancellation
# behavior; task.Run descendants still belong to the admitted customer root.
for name in (
    "proxy/freedom/freedom.go", "proxy/vless/outbound/outbound.go",
    "proxy/vmess/outbound/outbound.go", "proxy/trojan/client.go",
    "proxy/shadowsocks/client.go", "proxy/shadowsocks_2022/outbound.go",
    "proxy/socks/client.go", "proxy/http/client.go", "proxy/hysteria/client.go",
    "proxy/masque/client.go", "proxy/wireguard/client.go",
):
    replace(name, "\t\tctx = newCtx", "\t\tctx = darkpro.Preserve(ctx, newCtx)")

# Vision stores a reflected TLS object's address in uintptr across statements.
# That loses pointer ownership and fails the real Go checkptr acceptance run.
# Keep the object as unsafe.Pointer and use the supported same-allocation Add;
# race/checkptr instrumentation stays enabled rather than hiding the failure.
for name, objects in (
    ("proxy/vless/inbound/inbound.go", ("commonConn", "tlsConn.Conn", "realityConn.Conn")),
    ("proxy/vless/outbound/outbound.go", ("commonConn", "tlsConn.Conn", "utlsConn.Conn", "realityConn.Conn")),
):
    replace(name, "var p uintptr", "var p unsafe.Pointer")
    for obj in objects:
        replace(name, f"p = uintptr(unsafe.Pointer({obj}))", f"p = unsafe.Pointer({obj})")
    for field in ("i", "r"):
        replace(name, f"unsafe.Pointer(p + {field}.Offset)", f"unsafe.Add(p, {field}.Offset)")

# Upstream's UDP cleanup goroutine reads the close flag outside the manager
# lock while finalization closes QUIC sessions. Real race-instrumented traffic
# catches this; the writer's closed check also races with manager shutdown.
hyconn = "transport/internet/hysteria/conn.go"
replace(hyconn, '"sync"', '"sync"\n\t"sync/atomic"')
replace(hyconn, "\tclosed bool", "\tclosed atomic.Bool", count=2)
replace(hyconn, "if c.closed {", "if c.closed.Load() {")
replace(hyconn, "if !udpConn.closed {\n\t\tudpConn.closed = true", "if !udpConn.closed.Swap(true) {")
replace(hyconn, "if m.closed {", "if m.closed.Load() {", count=2)
replace(hyconn, "m.closed = true", "m.closed.Store(true)")

# Retained writers and raw-copy shortcuts cannot create late counter updates.
# The I/O lifetime remains live through the final corresponding counter.Add.
stats = "app/dispatcher/stats.go"
after(stats, "func (w *SizeStatWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {", '''
    finish, admitted := darkpro.BeginIO(func(){ common.Interrupt(w.Writer) })
    if !admitted { buf.ReleaseMulti(mb); return darkpro.ErrBarrier }
    defer finish()
''')
statconn = "transport/internet/stat/connection.go"
for method, arg in (("Read", "b []byte"), ("Write", "b []byte")):
    after(statconn, f"func (c *CounterConnection) {method}({arg}) (int, error) {{", '''
    finish, admitted := darkpro.BeginIO(func(){ _ = c.Connection.Close() })
    if !admitted { return 0, darkpro.ErrBarrier }
    defer finish()
''')
bufio = "common/buf/io.go"
for anchor in ("func (r *TimeoutWrapperReader) ReadMultiBuffer() (MultiBuffer, error) {", "func (r *TimeoutWrapperReader) ReadMultiBufferTimeout(duration time.Duration) (MultiBuffer, error) {"):
    after(bufio, anchor, '''
    if r.Counter != nil {
        finish, admitted := darkpro.BeginIO(func(){
            if c, ok := r.Reader.(interface{ Interrupt() }); ok { c.Interrupt() } else if c, ok := r.Reader.(io.Closer); ok { _ = c.Close() }
        })
        if !admitted { return nil, darkpro.ErrBarrier }
        defer finish()
    }
''')
tun = "proxy/tun/handler.go"
after(tun, "func (w *tunUDPStatsWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {", '''
    finish, admitted := darkpro.BeginIO(func(){ common.Interrupt(w.writer) })
    if !admitted { buf.ReleaseMulti(mb); return darkpro.ErrBarrier }
    defer finish()
''')
wgserver = "proxy/wireguard/server.go"
replace(wgserver, "\t\tif s.uplinkCounter != nil || s.downlinkCounter != nil {", "\t\tif darkpro.Enabled() || s.uplinkCounter != nil || s.downlinkCounter != nil {")
wgclient = "proxy/wireguard/client.go"
after(wgclient, "func (c *PacketCounterConnection) ReadFrom(p []byte) (n int, addr net.Addr, err error) {", '''
    if !darkpro.WaitForActivation() { return 0, nil, darkpro.ErrBarrier }
    finish, admitted := darkpro.BeginIO(func(){ _ = c.PacketConn.Close() })
    if !admitted { return 0, nil, darkpro.ErrBarrier }
    defer finish()
''')
after(wgclient, "func (c *PacketCounterConnection) WriteTo(p []byte, addr net.Addr) (n int, err error) {", '''
    finish, admitted := darkpro.BeginIO(func(){ _ = c.PacketConn.Close() })
    if !admitted { return 0, darkpro.ErrBarrier }
    defer finish()
''')
proxy = "proxy/proxy.go"
replace(proxy, '"github.com/xtls/xray-core/common/buf"', '"github.com/xtls/xray-core/common"\n\t"github.com/xtls/xray-core/common/buf"')
# Kernel splice updates usage only when the long-lived copy exits. The managed
# v2 path uses the same buffered copy path as non-Linux Xray so quota samples
# remain current while a connection stays active.
replace(proxy, 'if runtime.GOOS != "linux" && runtime.GOOS != "android" {', 'if darkpro.Enabled() || (runtime.GOOS != "linux" && runtime.GOOS != "android") {')
after(proxy, "func (w *VisionReader) ReadMultiBuffer() (buf.MultiBuffer, error) {", '''
    finish, admitted := darkpro.BeginIO(func(){ if w.conn != nil { _ = w.conn.Close() } })
    if !admitted { return nil, darkpro.ErrBarrier }
    defer finish()
''')
after(proxy, "func (w *VisionWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {", '''
    finish, admitted := darkpro.BeginIO(func(){ if w.conn != nil { _ = w.conn.Close() } })
    if !admitted { buf.ReleaseMulti(mb); return darkpro.ErrBarrier }
    defer finish()
''')
after(proxy, "func CopyRawConnIfExist(ctx context.Context, readerConn net.Conn, writerConn net.Conn, writer buf.Writer, timer *signal.ActivityTimer, inTimer *signal.ActivityTimer) error {", '''
    ownedReader, ownedWriter := readerConn, writerConn
    finish, admitted := darkpro.BeginIO(func(){
        if ownedReader != nil { _ = ownedReader.Close() }
        if ownedWriter != nil { _ = ownedWriter.Close() }
        common.Interrupt(writer)
    })
    if !admitted { return darkpro.ErrBarrier }
    defer finish()
''')

# Validate every source/anchor before any source checkout mutation occurs.
for name, text in contents.items():
    (source / name).write_text(text)
target = source / "common/darkprov2"
target.mkdir()
for name in ("barrier.go", "barrier_test.go"):
    shutil.copyfile(repo / "internal/corebarrierv2" / name, target / name)
shutil.copyfile(repo / "scripts/core-overlay/inbound_stats.go.txt", source / "app/stats/command/darkprov2.go")
shutil.copyfile(repo / "scripts/core-overlay/inbound_xdrive_test.go.txt", source / "transport/internet/xdrive/darkprov2_test.go")
provenance = {
    "upstream_commit": PIN,
    "protocol": "darkpro-inbound-v2/",
    "upstream_source_sha256": HASHES,
    "overlay_sha256": {
        str(p.relative_to(repo)): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in (Path(__file__).resolve(), repo / "internal/corebarrierv2/barrier.go", repo / "scripts/core-overlay/inbound_stats.go.txt", repo / "scripts/core-overlay/inbound_xdrive_test.go.txt")
    },
    "patched_source_sha256": {
        name: hashlib.sha256((source / name).read_bytes()).hexdigest()
        for name in sorted(list(HASHES) + ["common/darkprov2/barrier.go", "common/darkprov2/barrier_test.go", "app/stats/command/darkprov2.go", "transport/internet/xdrive/darkprov2_test.go"])
    },
}
(source / "darkpro-inbound-provenance.json").write_text(json.dumps(provenance, sort_keys=True, indent=2) + "\n")
print(f"Applied native-inbound v2 overlay to {PIN}; no services installed")
