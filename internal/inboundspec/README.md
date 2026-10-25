# Private inbound specification compiler

`inboundspec` validates inbound authoring documents, renders server configuration, and exports configuration for one business-owned account. It is independent of the database, API, browser editor, and runtime supervisor. It does not start a process, create a listener, access the configured certificate files, issue certificates, discover REALITY targets, or scan the network.

The schema is based on the stable [MHSanaei/3x-ui v3.10.2 source](https://github.com/MHSanaei/3x-ui/tree/v3.10.2/frontend/src/schemas/protocols). The native engine contract is pinned to [Xray commit `701af60772cda123492f86e383e1fdba066614a9`](https://github.com/XTLS/Xray-core/tree/701af60772cda123492f86e383e1fdba066614a9), the dependency of that release. This package is an independent implementation of those configuration contracts. Later documentation or newer engine features do not silently expand the accepted schema.

## Document and API contract

`Spec` version 1 has `version`, `protocol`, `network`, `security`, `settings`, `stream`, `tls`, `reality`, and `client`. Each settings map uses a typed recursive allowlist; it is not arbitrary engine JSON. Unknown fields and incompatible combinations fail with `FieldError{Path, Code}`, which wraps `ErrInvalid`. Error messages contain field paths and codes rather than submitted credentials. The maximum document size is 1 MiB.

| Operation | Contract |
| --- | --- |
| `Decode` | Strict JSON decoding, size limit, no unknown top-level fields or trailing document; then normalization. |
| `Normalize` / `Validate` | Deep-copy, canonicalize aliases, apply defaults, and validate. Neither generates secret keys or modifies its input. |
| `EnsureSecrets` | Explicit create-time credential generation using `crypto/rand`; also supplies random authoring row IDs. Do not call it to fill omitted secrets during an edit. |
| `Public` / `SecretPaths` | Produce a redacted authoring view and sorted dot-notation presence markers. No mask string can recover a secret. |
| `MergeSecrets` | Reconcile a public edit with the previous private document before normalizing it. Omitted secrets are retained; explicit empty/null values clear them. |
| `Render` | Produce a private Xray inbound or a private external-engine adapter payload and manifest. It does not perform deployment. |
| `ExportClient` | Produce private client material for exactly one authorized account and explicit endpoint. Unsupported representations return `ErrUnsupportedFormat`. |

Store the complete `Spec` encrypted. Public API responses, receipts, revision summaries, and browser snapshots must use `Public`, not a JSON serialization of the private struct. A public document may intentionally fail standalone validation because required write-only fields have been removed; edit validation happens after merging with the stored private document.

`settings.clients`, `users`, `accounts`, and `peers` are not accepted. The caller supplies `[]User` from the business binding store. Account protocols require at least one bound identity at render time. `tun` and `tunnel` reject account bindings because they are listener/device definitions. An authoring preview may use transient synthetic users in its own copy; that does not authorize persisting or deploying those identities.

### Preserving secrets safely during edits

Write-only fields include passwords, private keys, VLESS decryption/test seeds, shared mask secrets, ECH server keys, MLDSA seeds, TLS certificate/key PEM and file paths, key-log paths, headers, credential-bearing mask/masquerade URLs, and the entire XDrive template. `Public` removes them and lists their paths. Shared credential fields needed by an authorized client are handled separately by `ExportClient`.

TLS certificate rows and FinalMask rows carry an opaque 32-hex-character authoring `id`. Preserve these IDs when moving or deleting rows in the editor. Secret merging follows the row ID rather than the numerical array index. Changing a mask type must create a new row identity. Ambiguous anonymous edits that could attach a retained secret to a different row fail with `secret_reindex_requires_material`. Deleted rows stay deleted. These authoring IDs are stripped from engine configuration. Normalization provides deterministic IDs for legacy or preview documents using only their redacted public fields, scope, and index; changing a secret cannot change the derived ID or create an offline password verifier. Create-time `EnsureSecrets` supplies random IDs for new rows.

## Protocol and transport coverage

The generic transport set is `tcp`, `kcp`, `ws`, `grpc`, `httpupgrade`, `xhttp`, and `xdrive`. `raw`/`RAW` is a naming alias for `tcp`. `Catalog` describes protocol-level capabilities; `Normalize` and `ExportClient` apply the more specific transport/security/format restrictions.

| Protocol | Engine | Network | Security | Account model |
| --- | --- | --- | --- | --- |
| VLESS | Xray | Generic transports | None, TLS, REALITY | Bound UUID |
| VMess | Xray | Generic transports | None, TLS | Bound UUID |
| Trojan | Xray | Generic transports | None, TLS, REALITY | Bound password |
| Shadowsocks | Xray | Generic transports | None, TLS | Bound password or derived 2022 key |
| WireGuard | Xray | `wireguard` | Native | Derived peer keys and managed addresses |
| Hysteria 2 | Xray | `hysteria` | TLS | Bound authentication string |
| HTTP | Xray | `tcp` | None | Bound username/password |
| Mixed | Xray | `tcp` | None | Bound SOCKS/HTTP account settings |
| Tunnel | Xray | `tcp` | None | Listener only |
| TUN | Xray | `tun` | None | Device only |
| MTProto | External adapter | `mtproto` | Native FakeTLS | Per-account FakeTLS secret |
| AmneziaWG | External adapter | `amneziawg` | Native | Derived peer keys and managed addresses |
| TUIC | External adapter | `tuic` | TLS | Bound UUID/password |
| MASQUE | Xray | `masque` | TLS | Bound username/password |

TLS on generic transports is accepted for TCP, WebSocket, gRPC, HTTPUpgrade, and XHTTP. REALITY is accepted for VLESS/Trojan over TCP, gRPC, or XHTTP. KCP and XDrive do not accept those generic TLS/REALITY combinations. Dedicated protocols use their own transport contract. The matrix is validated rather than merely presented as dropdown values.

The package covers six main advanced-setting families:

1. Typed native protocol settings, account derivation, VLESS/Trojan fallbacks, WireGuard/AWG addressing, TUN routing controls, and dedicated external server settings. Shared Xray sniffing is authored at `settings.sniffing` and rendered at the inbound root. Fallbacks require TCP and TLS.
2. Actual transport settings under `stream.tcpSettings`, `kcpSettings`, `wsSettings`, `grpcSettings`, `httpupgradeSettings`, `xhttpSettings`, and `xdriveSettings`. XDrive has its own Google Drive/local/template storage schema, credentials, operations, and tuning.
3. Typed `stream.sockopt`, including custom socket options and Happy Eyeballs fields. These are listener settings; arbitrary server socket controls are not copied into a subscriber's client.
4. FinalMask TCP/UDP/QUIC settings, typed custom packet transforms, mask credentials, and QUIC congestion/hop options. Unsupported server mask types are rejected. A client UDP-hop declaration is translated into the native client `udphop` mask and excluded from the server.
5. TLS server certificates, files, versions, ALPN, cipher suites, curve preferences, resumption, ECH keys, and ECH Sockopt. Inline certificate/private-key pairs are parsed and matched. A file reference must be absolute; file availability and permissions are deployment checks.
6. REALITY target/server names/private key/short IDs/time bounds/fallback limits/MLDSA seed, and separate client fingerprint, public key, short ID, spider path, MLDSA verification key, ECH config, certificate pins, and mux settings.

Pinned-core constraints are deliberate: Shadowsocks `ivCheck=true` is rejected because the pinned engine removed the field; Xray `allowInsecure=true` is rejected because the engine removed that bypass. REALITY requires concrete allowed server names. Certificate SHA-256 pins normalize to the pinned engine's lowercase hex format; Base64 and colon-separated hex input can be migrated. Incompatible or absent capabilities must remain unavailable in the editor rather than being silently discarded.

### Server/client separation

`tls` and `reality` hold server-side material. `client` holds subscription-side hints and options. Client export derives the REALITY public key and checks that the chosen SNI is accepted by the server. Server TLS keys, REALITY private keys, MLDSA signing seeds, and WireGuard server private keys are not exported to subscribers.

XHTTP client pool settings (`xmux`, client post intervals/chunking, and client-only header controls) are omitted from the listener and preserved in the Xray client. Server buffering/header-limit settings stay on the listener. The core's outbound-only `downloadSettings` is not a field of Sanaei v3.10.2's inbound schema and is not accepted as an inbound setting here. Generic mux is a client outbound option, not an inbound listener option.

Client exports can legitimately contain the selected account password, its derived WireGuard private key, and protocol-required shared material such as Shadowsocks 2022 server key components, XDrive storage credentials, or FinalMask passwords. Export results therefore belong only in an authorized single-user subscription path and must not be reused as public profile views.

WireGuard/AWG keys are domain-separated, deterministic per-account derivations from the server private material and business UUID. Address allocation uses `User.Addresses` when supplied, otherwise stable business `User.ID` (a UUID hash fallback exists for synthetic previews). It rejects collisions and subnet exhaustion. The account store should supply stable IDs or managed allocations; removing another account must not renumber existing peers.

## External engine boundary

`Rendered.Engine` is authoritative. MTProto, TUIC, and AmneziaWG never produce pretend Xray inbounds. Every manifest identifies its adapter, required capabilities, file references where applicable, and `requires_engine_validation`.

| Engine | Adapter | Private payload | Supervisor responsibilities |
| --- | --- | --- | --- |
| MTProto | `mtg-multi-v1` | Actual MTG configuration map; `RenderTOML` serializes it deterministically | Obtain and verify the compatible engine, validate it, manage lifecycle and per-secret accounting. When `requires_socks_bridge` is set, allocate a managed bridge for the requested outbound before activation. |
| TUIC | `tuic-v5-v1` | `tuic-adapter-json`: listener, server settings, bound clients, and TLS material | Load the compatible TUIC engine/adapter, consume inline TLS or safely materialize managed certificate files, validate UDP support, manage lifecycle and account traffic. This is an adapter contract, not a claim that any TUIC CLI accepts this JSON directly. |
| AmneziaWG | `amneziawg-v3.1-v1` | `amneziawg-adapter-json`: listener, tunnel address, server settings, and bound peers | Translate into the supported UAPI/engine format, provision managed interfaces/routes/addressing with required privileges, validate the engine, and manage per-peer traffic and cleanup. |

The package does not install those engines, mutate network interfaces, create bridges, manage certificates, or prove live traffic/accounting. A valid adapter payload is necessary for deployment but not sufficient. The integration must refuse activation when the required adapter or platform capability is unavailable. The manifest explicitly reports that HTTP/mixed native accounting does not provide reliable per-user attribution; downstream metering must not fabricate it.

## Client export formats and limits

| Format | Supported representation |
| --- | --- |
| `json` / `xray` | Complete Xray `outbounds` configuration for native account protocols. `json` for external protocols returns their documented client representation; `xray` rejects external engines. |
| `uri` | VLESS, VMess, Trojan, Shadowsocks, Hysteria 2, MTProto `tg://proxy`, TUIC, compatible WireGuard links, AmneziaWG `vpn://`, and HTTP/SOCKS account links. |
| `wireguard` / `conf` | Native WireGuard or AmneziaWG text configuration including the required peer and shared obfuscation settings. |
| `sing-box` | A constrained conversion for common VLESS/VMess/Trojan/Shadowsocks/HTTP/mixed/Hysteria/TUIC settings. Unsupported Xray extensions fail explicitly. |

There is no invented URI for XDrive or MASQUE. TUN and tunnel have no subscriber export. Enabled client mux, custom RAW HTTP headers, additional WebSocket/HTTPUpgrade headers, advanced gRPC or KCP tuning, and WireGuard DNS/allowed-IP/peer options that the link cannot preserve return an unsupported-format error; use JSON or native WireGuard configuration. Nonzero Xray-specific WireGuard reserved bytes require Xray JSON because native WireGuard text configuration has no corresponding field. VMess share objects preserve the conventional gRPC `type=multi`, XHTTP `type=<mode>`, and KCP `mtu`/`tti` fields. XHTTP and FinalMask URI metadata require a client that supports those extension conventions.

The sing-box converter rejects Xray-only KCP/XHTTP/XDrive, FinalMask, mux, VLESS encryption extensions, unsupported TLS verification extensions, MLDSA65, RAW HTTP headers, unsupported gRPC modes/authority/timeouts/window/keepalive/user-agent tuning, and WebSocket heartbeat settings. TUIC URI also rejects client certificate pins, ECH config, and a certificate-name verification override because that link representation does not preserve them. For these TUIC verification extensions, the current external JSON/sing-box representation is also unavailable; the exporter returns an explicit error instead of weakening the requested verification. These converters are not a claim of compatibility with every client version or every transport extension. No external sing-box executable is exercised by this package's tests.

## Verification

`testdata/protocols.json` contains fourteen portable private test specs using public synthetic credentials. It must never be deployed. Tests inject business identities separately, verify render/export behavior, reject arbitrary account arrays, check redaction and explicit secret clearing, and cover stable certificate/mask row reordering and deletion.

Run the package tests with the project Go toolchain:

```sh
go test ./internal/inboundspec -count=1
```

The opt-in native engine test additionally renders and parses 30 server and 28 client configurations with the pinned Xray executable, including all eleven native protocols, seven generic transports, TLS/REALITY combinations, certificate pins, gRPC/Sockopt, TCP masks, and Hysteria Gecko/QUIC/hop settings. It uses `xray run -test -config` with synthetic material and does not start listeners or make configured destination connections. Config acceptance does not demonstrate a successful handshake, external target availability, traffic routing, or end-to-end metering.

```sh
DXP_INBOUNDSPEC_XRAY=/path/to/pinned/xray \
DXP_INBOUNDSPEC_XRAY_SHA256=<verified-executable-sha256> \
go test ./internal/inboundspec -count=1
```

Both variables must be absent to skip this gate; a partial pair or digest mismatch fails it. The executable is hashed before use. Build the immutable commit above, whose module version is `v1.260327.1-0.20261010092107-701af60772cd` and module checksum is `h1:Am/9Pzw5PZr7+hMN1Vs5faeSXRoutnrwMAgtUDnh/J8=`. Executable hashes depend on the reproducible build recipe; the CI job must verify the artifact it actually supplies rather than relying on a local filename.
