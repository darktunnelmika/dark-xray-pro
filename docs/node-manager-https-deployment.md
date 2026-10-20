# Node Manager owner HTTPS routes

N1–N3 introduced owner session endpoints that also need explicit reverse-proxy
allowlisting. A LAB deployment exposed a gap: loopback returned 401 as expected,
but HTTPS returned the proxy's blanket 403, making the features inaccessible.

`ops/nginx/node-manager.conf` allows only GET name/dependencies and POST rename.
Nginx also permits HEAD wherever GET is permitted. Owner session, same-origin
JSON and CSRF checks remain in the HUB. Agent/admin routes must remain blocked.
Include this file inside the existing owner HTTPS server, before broad regex
locations; preserve Host and the existing deny fallback. Do not replace the
entire server configuration or broaden `/api/` access.

Before editing a LAB proxy, save its complete configuration and validate the
existing configuration with `nginx -t`. Install the snippet, validate again, and
reload nginx. If validation or HTTPS probes fail, restore the saved configuration
and reload. Test anonymous GET name/dependencies and POST rename return HUB 401;
wrong methods and private endpoints must remain denied. This does not activate
Xray or change registry, accounting, node credentials or customer records.

`scripts/test-node-manager-proxy.py` uses real nginx and a disposable loopback
authentication-denial backend to prove routing and deny boundaries. It tests
proxy behavior only; existing Go API tests prove session/CSRF authorization.

Every nginx temporary path is explicitly inside the disposable test directory.
Setting only `-p` does not override distribution-compiled absolute temp paths:
when run as root, nginx can change their ownership to its default worker user.
Never run an older copy of this test on a server with an existing nginx service.
