import { sha256 } from "@noble/hashes/sha2.js";

// A File is structured-cloned without reading its entire contents. Only this
// fixed slice is materialized at a time; terminating the worker cancels hashing.
const chunkBytes = 1 << 20;
let started = false;
self.onmessage = async (event: MessageEvent<{ file: File }>) => {
  if (started) return;
  started = true;
  const file = event.data?.file;
  if (!(file instanceof File) || !Number.isSafeInteger(file.size) || file.size < 1) {
    self.postMessage({ type: "error" });
    return;
  }
  const hash = sha256.create();
  let lastProgress = 0;
  try {
    for (let offset = 0; offset < file.size; offset += chunkBytes) {
      const end = Math.min(offset + chunkBytes, file.size);
      hash.update(new Uint8Array(await file.slice(offset, end).arrayBuffer()));
      if (end === file.size || performance.now() - lastProgress >= 100) {
        self.postMessage({ type: "progress", bytes: end });
        lastProgress = performance.now();
      }
    }
    const digest = Array.from(hash.digest(), value => value.toString(16).padStart(2, "0")).join("");
    self.postMessage({ type: "complete", sha256: digest });
  } catch {
    self.postMessage({ type: "error" });
  } finally {
    hash.destroy();
  }
};
