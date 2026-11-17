// Opening screen of the Mini App: the same honeycomb pulse as the panel, in
// the colours of the user's Telegram theme. Shown once per session, for about
// a second, until the account has loaded; a tap skips it.
(function () {
  var c = document.getElementById("splash");
  if (!c) return;
  var seen = false;
  try { seen = sessionStorage.getItem("splash") === "1"; sessionStorage.setItem("splash", "1"); } catch (e) {}
  if (seen || !c.getContext || matchMedia("(prefers-reduced-motion: reduce)").matches) { c.remove(); return; }
  var light = false;
  try { light = window.Telegram.WebApp.colorScheme === "light"; } catch (e) {}
  var BG = light ? "#f2f3f7" : "#05070b", ION = light ? "4,135,95" : "40,247,160", SKY = light ? "31,111,209" : "75,185,255";
  c.style.background = BG;
  var x = c.getContext("2d"), TAU = Math.PI * 2, W, H, D, s, cells, done = false, t0 = performance.now();
  function glow(cx, cy, r, col, a) {
    var g = x.createRadialGradient(cx, cy, 0, cx, cy, r);
    g.addColorStop(0, "rgba(" + col + "," + a + ")"); g.addColorStop(1, "rgba(" + col + ",0)");
    x.fillStyle = g; x.fillRect(cx - r, cy - r, 2 * r, 2 * r);
  }
  function resize() {
    D = Math.min(window.devicePixelRatio || 1, 2); W = innerWidth; H = innerHeight;
    c.width = W * D; c.height = H * D; x.setTransform(D, 0, 0, D, 0, 0);
    s = Math.max(17, Math.min(26, Math.min(W, H) / 22));
    var hx = s * 1.5, hy = s * Math.sqrt(3); cells = [];
    for (var i = -1; i < W / hx + 1; i++) for (var j = -1; j < H / hy + 1; j++) {
      var cx = i * hx, cy = j * hy + (i % 2 ? hy / 2 : 0);
      cells.push([cx, cy, Math.hypot(cx - W / 2, cy - H / 2)]);
    }
  }
  function draw(u) {
    x.globalCompositeOperation = "source-over"; x.fillStyle = BG; x.fillRect(0, 0, W, H);
    if (!light) x.globalCompositeOperation = "lighter";
    glow(W / 2, H / 2, Math.max(W, H) * 0.6, ION, 0.07);
    var k = s / 17;
    for (var n = 0; n < cells.length; n++) {
      var p = cells[n], d = p[2] / k;
      var w = Math.pow(0.5 + 0.5 * Math.cos(d / 38 - u * TAU * 2), 6), w2 = Math.pow(0.5 + 0.5 * Math.cos(d / 90 + u * TAU), 3);
      x.strokeStyle = "rgba(" + (w > 0.5 ? ION : SKY) + "," + (0.07 + 0.8 * w + 0.12 * w2) + ")"; x.lineWidth = 0.8 + w * 1.2;
      x.beginPath();
      for (var q = 0; q <= 6; q++) { var an = q * TAU / 6, px = p[0] + Math.cos(an) * (s - 2), py = p[1] + Math.sin(an) * (s - 2); q ? x.lineTo(px, py) : x.moveTo(px, py); }
      x.stroke();
      if (w > 0.75) { x.fillStyle = "rgba(" + ION + "," + (w - 0.75) * 1.1 + ")"; x.fill(); }
    }
    glow(W / 2, H / 2, 46 * k, light ? ION : "255,255,255", light ? 0.3 : 0.55);
  }
  function finish() {
    if (done) return;
    done = true; c.classList.add("out");
    setTimeout(function () { c.remove(); removeEventListener("resize", resize); }, 350);
  }
  // Leave once the account is on screen and the pulse has been seen (1 s),
  // or after 4 s whatever happens.
  var app = document.getElementById("app"), ready = false;
  function check() { if (ready && performance.now() - t0 >= 1000) finish(); }
  if (app) new MutationObserver(function () { if (!app.querySelector(".boot")) { ready = true; check(); } }).observe(app, { childList: true, subtree: true });
  setTimeout(finish, 4000);
  c.addEventListener("pointerdown", finish);
  addEventListener("resize", resize); resize();
  (function loop(t) { if (c.isConnected) { draw((t / 4000) % 1); if (!done) check(); requestAnimationFrame(loop); } })(0);
})();
