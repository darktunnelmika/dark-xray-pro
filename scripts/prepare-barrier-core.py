#!/usr/bin/env python3
"""Apply the small, fail-closed TCP drain overlay to ONE immutable Xray commit.

The resulting core is experimental and never replaces an installed Xray.
Build it separately with the Go version in the pinned upstream go.mod.
"""
import hashlib
from pathlib import Path
import shutil
import subprocess
import sys

PIN = "52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120"
HASHES = {
    "proxy/vless/inbound/inbound.go": "9108a2292f8e1d1cbf506a44836f54103ad3fcfb3769c4f815f726e93acb9e32",
    "common/task/task.go": "fe387a8e47158aefc6135505861245401c7c49d7df60759f25f61761453fcb0e",
    "app/stats/command/command.go": "d43685fff16f6b6034233879f59fe1146978cb7413efc49b40a3a5bc9969da0d",
    "app/dispatcher/default.go": "d7e095dc32594f860d01ddbb564a4560150de0a95982fc64f1f293c668c01122",
}
if len(sys.argv) != 2:
    raise SystemExit("usage: prepare-barrier-core.py PINNED_XRAY_CHECKOUT")
source = Path(sys.argv[1]).resolve()
repo = Path(__file__).resolve().parent.parent
if subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip() != PIN:
    raise SystemExit("wrong upstream commit")
if subprocess.check_output(["git", "-C", str(source), "status", "--porcelain", "--untracked-files=all"], text=True).strip():
    raise SystemExit("upstream checkout must be completely clean before applying the overlay")
contents = {}
for name, digest in HASHES.items():
    raw = (source / name).read_bytes()
    if hashlib.sha256(raw).hexdigest() != digest:
        raise SystemExit(f"unexpected upstream file: {name}")
    contents[name] = raw.decode()

def replace(name, old, new):
    if contents[name].count(old) != 1:
        raise SystemExit(f"ambiguous patch: {name}")
    contents[name] = contents[name].replace(old, new)

for name in HASHES:
    replace(name, '"context"', '"context"\n\tdarkpro "github.com/xtls/xray-core/common/darkpro"')

vless = "proxy/vless/inbound/inbound.go"
anchor = "func (h *Handler) Process(ctx context.Context, network net.Network, connection stat.Connection, dispatch routing.Dispatcher) error {"
replace(vless, anchor, anchor + '''
    if darkpro.Enabled() {
        child, cancel := context.WithCancel(ctx)
        tracked, done, ok := darkpro.Enter(child, func(){ cancel(); connection.Close() })
        if !ok { cancel(); connection.Close(); return errors.New("DARK PRO TCP epoch is draining") }
        defer done()
        defer cancel()
        ctx = tracked
    }
''')
replace(vless, '\taccount := request.User.Account.(*vless.MemoryAccount)\n', '''
    // Detached mux/XUDP/reverse work is outside this proof. Reject it before
    // dispatch, even when a client requests it without a server mux setting.
    if darkpro.Enabled() && request.Command != protocol.RequestCommandTCP {
        return errors.New("DARK PRO final-counter scope permits direct TCP only")
    }
\taccount := request.User.Account.(*vless.MemoryAccount)
''')
task = "common/task/task.go"
replace(task, '\t\tgo func(f func() error) {', '\t\tfinish := darkpro.Track(ctx)\n\t\tgo func(f func() error) {\n\t\t\tdefer finish()')
dispatcher = "app/dispatcher/default.go"
anchor = "func (d *DefaultDispatcher) Dispatch(ctx context.Context, destination net.Destination) (*transport.Link, error) {"
replace(dispatcher, anchor, anchor + '''
    // Admitted VLESS TCP uses synchronous DispatchLink. Never let a tracked
    // session escape through the separate asynchronous/mux dispatch path.
    if darkpro.Tracked(ctx) { return nil, errors.New("unsupported DARK PRO asynchronous dispatch") }
''')
command = "app/stats/command/command.go"
for method, req, resp in (("GetStats", "GetStatsRequest", "GetStatsResponse"), ("GetUsersStats", "GetUsersStatsRequest", "GetUsersStatsResponse"), ("QueryStats", "QueryStatsRequest", "QueryStatsResponse")):
    anchor = f"func (s *statsServer) {method}(ctx context.Context, request *{req}) (*{resp}, error) {{"
    extra = '\n\tif darkpro.Enabled() && request.Reset_ { return nil, status.Error(codes.FailedPrecondition, "DARK PRO counters cannot be reset") }\n'
    if method == "QueryStats":
        extra += '\tif strings.HasPrefix(request.Pattern, darkpro.Protocol) { return s.darkProFinalStats(ctx, request) }\n'
    replace(command, anchor, anchor + extra)

# Validate all patches before modifying the checkout.
for name, text in contents.items():
    (source / name).write_text(text)
target = source / "common/darkpro"
target.mkdir()
for name in ("barrier.go", "barrier_test.go"):
    shutil.copyfile(repo / "internal/corebarrier" / name, target / name)
shutil.copyfile(repo / "scripts/core-overlay/final_stats.go.txt", source / "app/stats/command/darkpro.go")
print(f"Applied experimental direct-TCP barrier to {PIN}; no services installed")
