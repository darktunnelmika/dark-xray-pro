module github.com/darktunnelmika/dark-xray-pro

go 1.27.3

require (
	filippo.io/age v1.3.2
	github.com/9seconds/mtg/v2 v2.3.0
	github.com/amnezia-vpn/amneziawg-go v1.0.4
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/xtls/xray-core v1.260327.1-0.20261109052709-676eaf66f8f0
	golang.org/x/crypto v0.58.0
	golang.org/x/sys v0.49.0
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	gvisor.dev/gvisor v0.0.0-20260122175437-89a5d21be8f0
	modernc.org/sqlite v1.48.2
)

// Pinned, unmodified MIT source; provenance is in third_party/go-qrcode/UPSTREAM.json.
replace github.com/skip2/go-qrcode => ./third_party/go-qrcode

require (
	filippo.io/hpke v0.4.0 // indirect
	github.com/OneOfOne/xxhash v1.2.8 // indirect
	github.com/apernet/quic-go v0.61.1-0.20260806010916-184d081eef3e // indirect
	github.com/babolivier/go-doh-client v0.0.0-20201028162107-a76cff4cb8b6 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/juju/ratelimit v1.0.2 // indirect
	github.com/klauspost/compress v1.20.1 // indirect
	github.com/klauspost/cpuid/v2 v2.4.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/molecule-man/go-brrr v1.2.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/panjf2000/ants/v2 v2.12.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pires/go-proxyproto v0.16.0 // indirect
	github.com/refraction-networking/utls v1.8.3-0.20261108164504-a13c59ffe98b // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rs/zerolog v1.35.0 // indirect
	github.com/tevino/abool v1.2.0 // indirect
	github.com/txthinking/runnergroup v0.0.0-20250224021307-5864ffeb65ae // indirect
	github.com/txthinking/socks5 v0.0.0-20251011041537-5c31f201a10e // indirect
	github.com/tylertreat/BoomFilters v0.0.0-20251117164519-53813c36cc1b // indirect
	github.com/xtls/reality v0.0.0-20261108101329-4bb46b2afd8b // indirect
	github.com/yl2chen/cidranger v1.0.2 // indirect
	github.com/yuin/gopher-lua v1.1.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/crypto/x509roots/fallback v0.0.0-20261020153306-9f2b28d4331a // indirect
	golang.org/x/exp v0.0.0-20251023183803-a4bb9ffd2546 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	layeh.com/gopher-luar v1.0.11 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
