# DARK PRO backup container, version 1

`probackup` is a streaming container and quarantine restore library. Collection
of a consistent native snapshot, source/schema validation, recovery keys,
scheduling, authorization, and service activation belong to its callers.

## Layers and limits

The wire format is one **binary age v1 file**, encrypting one **gzip member**,
whose plaintext is the record sequence below. Encryption uses only native age
X25519 keys through `filippo.io/age`; it does not run age subprocesses, plugins,
SSH key handlers, or a locally invented encryption scheme. Compression precedes
encryption. No file names, manifest fields, or checksums appear outside the
encrypted payload. Public-key encryption does not prove which server produced
a whole archive; callers must select backups from a trusted source and must not
treat a self-described source revision or timestamp as a signature or freshness
guarantee.

Before age parses input, the codec bounds its header to 64 KiB, each line to
1,024 bytes, and the number of recipient stanzas to 32. The native age library
still validates all header syntax, key wrapping, and authentication. The gzip
header has no name, comment, extra data, or modification time. Multiple gzip
members are refused.

Default limits are 66 GiB ciphertext, 64 GiB total uncompressed container,
64 GiB per file, 4,096 files, and 1 MiB manifest. Zero limit fields choose their
defaults; there is no implicit unlimited mode. Applications can choose smaller
limits. Hard ceilings are 2 TiB ciphertext, 1 TiB plaintext/per-file, 65,536
files, and 16 MiB manifest. All length arithmetic uses bounded 64-bit integers;
no payload-sized allocations or seeks occur. The copying buffer is 64 KiB.

## Record encoding

All integer fields are unsigned, big endian. The magic is the 16 bytes
`DARKPROBACKUP\x00\x00\x01`. Files are sorted by their exact ASCII names.

| Record | Bytes |
|---|---|
| File | `0x01`, one-byte role, two-byte name length, eight-byte file size, name, exactly that many file bytes |
| Manifest | `0x02`, four-byte JSON length, canonical JSON manifest |
| End | `0xff`, then the uncompressed stream must end |

There is at least one file, exactly one manifest after all files, and one end
record. Unknown records, versions, and roles fail closed. Names contain at most
240 bytes and eight nonempty ASCII path segments using `A-Z a-z 0-9 . _ -`.
`.` and `..` segments, absolute paths, duplicate names, any file/directory prefix
conflict, and `restore-quarantine.json` or its children are refused. Names are
logical paths within the archive; host absolute paths are never serialized.

The role byte mapping is fixed for v1: 1 business, 2 registry, 3 replacements,
4 config_stage, 5 lab_revisions, 6 node_history, 7 owner_report_db,
8 owner_report_config, 9 owner_report_token, 10 owner_report_runtime,
11 inbound_runtime, 12 runtime_config, 13 agent_tls_cert, 14 agent_tls_key,
15 inventory. Role-to-name and required-file policy belongs to the caller's
validator; the codec does not infer service layout or enable a feature.

The manifest is the exact output of Go `encoding/json.Marshal` for the exported
`Manifest` structure, with SHA-256 of every complete uncompressed file. It is
at the end so hashing and encryption take one source pass. Reading compares
every entry against the observed file records and hashes; duplicate JSON keys,
unknown fields, noncanonical JSON, missing records, and inconsistent sizes or
hashes are refused. SHA-256 provides inventory comparison; age provides the
cryptographic confidentiality and integrity boundary.

Success requires the end record, genuine uncompressed EOF, validated gzip
checksum/footer, genuine decrypted EOF with age's authenticated final chunk,
and ciphertext EOF. Neither reaching a declared length nor closing a decoder
is sufficient. Byte limits never manufacture successful EOF. On creation,
gzip and age finalization errors are propagated; the caller must sync and
atomically publish the ciphertext output separately.

## Source and restore contracts

`FileSource.Open` returns a finite, immutable snapshot and is called once. The
codec verifies exactly `Size` bytes and closes the source on success or failure.
Live WAL database files or changing journal files are not valid snapshots. A
native database snapshot must preserve accounting, immutable order/refund
receipts, usage epochs and finality, exposure history, identity reuse guards,
and schema/triggers together. The container does not reconstruct missing data.

`Verify` authenticates the container and discards its contents. It does not
validate SQLite schemas or application invariants. `Restore` therefore requires
a nonnil validation callback, executed only after complete stream verification.
The callback must be read-only, close its handles, and leave no sidecars or
extra files. The codec rechecks the exact private file inventory, file hashes,
and permissions after the callback, so a validator cannot silently alter what
is published. Callback errors are sanitized and do not expose private paths or
credential-bearing error text.

Restore creates a random mode-0700 sibling stage using a directory-rooted file
API; regular files are mode 0600. The destination must be an absolute, clean,
absent path. Existing empty directories are refused as well. Its existing
parent must not be symlinked or group/other writable. After verification and
validation, Restore writes `restore-quarantine.json`, syncs files/directories,
and publishes with Linux `renameat2(RENAME_NOREPLACE)`. It refuses platforms or
filesystems without this primitive. It never configures or starts a service.

Ordinary failures remove only the invocation's staging directory. A process
kill may leave a private orphan stage, but cannot publish it before verification;
a later attempt uses a different stage and still refuses an existing target.
An error after successful rename is `ErrPublicationUncertain`: the quarantined
destination is retained, since deleting it would misrepresent publication.
The caller should inspect that destination rather than retry over it.

Context is checked while copying. Caller-owned readers and writers must also
be finite or cancellable; the library does not detach goroutines or assume it
can interrupt arbitrary blocked external I/O.

## Validation

Unit tests cover byte limits, header bounds, every truncation of a valid small
archive, tampering, wrong keys, appended/concatenated encrypted or compressed
streams, corrupted gzip CRC, source-size disagreement, strict paths and roles,
manifest mismatches, validation failures/mutation, private permissions, existing
and racing destinations, cleanup, cancellation, and sanitized failures. Native
database round-trip acceptance and the explicitly enabled >3 GiB/RSS/crash
acceptance are separate integration gates owned by the application.
