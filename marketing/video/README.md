# Dark VPN intro video (61 s, 1920x1080, 30 fps)

- `v.html` draws every frame: open it in headless Chromium (viewport 1920x1080), call `render(t)` for
  t = k/30 and take JPEG screenshots (quality 92). `STARTS` and `TOTAL` give the scene boundaries
  (0, 6, 10, 18, 25, 32, 40, 48, 54, 61 s). Encode one mp4 per scene (libx264 crf 18, yuv420p), then
  concat with `-c copy`.
- `narration.fa.txt`: one line per scene; each clip starts 0.6 s after its scene starts and must be
  shorter than the scene.
- Voice wanted by the owner: Microsoft neural Persian (edge-tts, `fa-IR-FaridNeural`); needs network
  access to speech.platform.bing.com. The first cut used sherpa-onnx with piper `fa_IR-amir-medium`.
- Music: a soft synthesized pad under the voice (volume about 0.3), a bell at each scene start.
- `../banner-hamkari.html`: the 1080x1700 banner for the colleagues' group (screenshot it as PNG).
