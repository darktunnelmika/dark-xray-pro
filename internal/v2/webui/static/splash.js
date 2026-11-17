// Opening clip: plays once per browser session when the panel is opened,
// then fades away. A tap skips it; if the browser cannot play it, it is
// dropped at once so the panel is never held up.
(function () {
  var v = document.getElementById("splash");
  if (!v) return;
  var seen = false, done = false;
  try { seen = sessionStorage.getItem("splash") === "1"; sessionStorage.setItem("splash", "1"); } catch (e) {}
  if (seen || !v.play || matchMedia("(prefers-reduced-motion: reduce)").matches) { v.remove(); return; }
  function finish() {
    if (done) return;
    done = true; v.classList.add("out");
    setTimeout(function () { v.pause(); v.remove(); }, 450);
  }
  v.muted = true;
  v.addEventListener("ended", finish);
  v.addEventListener("error", finish);
  v.addEventListener("click", finish);
  var p = v.play();
  if (p && p.catch) p.catch(finish);
  setTimeout(function () { if (v.currentTime === 0) finish(); }, 2500); // never started
  setTimeout(finish, 6000);
})();
