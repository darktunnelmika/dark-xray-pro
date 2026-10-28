# Seller management, orders and volume return HTTPS routes

The new browser APIs need the existing owner HTTPS reverse proxy to forward
their exact routes. A working localhost API does not prove that the HTTPS
panel can reach it: the existing proxy's blanket denial can still return 403.

Include `ops/nginx/business-management.conf` inside the existing owner HTTPS
server, before broad regex locations. Preserve the server's existing Host
forwarding and API deny fallback. The snippet permits only the nine reviewed
seller-management, seller-order and volume-return route shapes with their required methods;
GET also permits HEAD, as in the Go handlers. Identity paths match the Store's
bounded 64-character validation. Feature gates, authentication, tenant scope,
financial permissions and CSRF remain enforced in the HUB.

Seller orders add only the exact POST collection and client extension paths
documented in [seller-funded-browser-api-v1.md](seller-funded-browser-api-v1.md).
Their bodies are limited to 4 KiB. The snippet does not enable the feature;
`DARK_PRO_ENABLE_SELLER_ORDERS_V1=1` and its accounting prerequisites remain
required. With the gate disabled, the HUB does not register those routes.

On the independently verified LAB, preserve the effective proxy configuration
and run `nginx -t` before editing. Add this snippet, validate again, then reload
only after validation succeeds. Probe the deployed HTTPS origin: anonymous
enabled allowed paths should reach HUB authorization, while wrong methods, extra
suffixes and private Agent/admin paths remain denied. Restore the saved proxy
configuration if validation or acceptance fails. Do not replace the server
configuration or broadly allow `/api/`.

`python3 scripts/test-business-management-proxy.py` exercises the shipped snippet
with real nginx and a disposable fake authentication backend. It checks allowed
requests reach that backend as 401 with the inherited Host, while denied
requests generate no backend call. It uses ephemeral loopback listeners and
explicit temporary paths for the PID, logs and every nginx temporary directory;
it neither contacts an installed HUB nor reloads an existing nginx service.
Missing nginx fails the test rather than reporting skipped acceptance as success.
