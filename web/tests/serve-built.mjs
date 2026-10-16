// Disposable test HTTP server. It serves the production build only on loopback.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, extname, sep } from 'node:path';
const reviewPort = Number(process.env.DARK_PRO_TEST_PORT ?? '5173');
if (!Number.isInteger(reviewPort) || reviewPort < 1024 || reviewPort > 65535) throw new Error('invalid test port');
const root = fileURLToPath(new URL('../dist/', import.meta.url));
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png' };
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://127.0.0.1:' + reviewPort);
    const path = decodeURIComponent(url.pathname);
    if (req.method !== 'GET' && req.method !== 'HEAD') { res.writeHead(405).end(); return; }
    if (path.startsWith('/api/')) { res.writeHead(404).end(); return; }
    const file = resolve(root, '.' + (extname(path) ? path : '/index.html'));
    if (!file.startsWith(resolve(root) + sep)) { res.writeHead(403).end(); return; }
    const body = await readFile(file);
    res.writeHead(200, { 'Content-Type': types[extname(file)] ?? 'application/octet-stream', 'Cache-Control': 'no-store' });
    res.end(req.method === 'HEAD' ? undefined : body);
  } catch { res.writeHead(404).end(); }
});
server.listen(reviewPort, '127.0.0.1', () => console.log('Compiled UI test server ready'));
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => server.close(() => process.exit(0)));
