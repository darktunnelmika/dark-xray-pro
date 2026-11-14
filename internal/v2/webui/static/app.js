// Dark VPN owner panel (v2). No framework, no build step.
const I18N = {
  en: {
    dashboard: "Dashboard", clients: "Clients", inbounds: "Inbounds", nodes: "Nodes", routing: "Routing", settings: "Settings",
    login: "Sign in", username: "Username", password: "Password", badLogin: "Invalid username or password",
    nodesOnline: "Nodes online", activeClients: "Active clients", onlineUsers: "Online now", trafficToday: "Traffic today", trafficTotal: "Total traffic",
    add: "Add", edit: "Edit", delete: "Delete", save: "Save", cancel: "Cancel", close: "Close", copy: "Copy", copied: "Copied",
    search: "Search", name: "Name", volumeGB: "Volume (GB, 0 = unlimited)", days: "Days (0 = no expiry)", firstUse: "Start on first connection",
    maxIPs: "Max IPs (0 = no limit)", enabled: "Enabled", assignInbounds: "Inbounds", unlimited: "Unlimited", noExpiry: "No expiry",
    notStarted: "Starts on first use", daysLeft: "{n} days left", expired: "Expired", disabled: "Disabled", exhausted: "Volume used up",
    subscription: "Subscription", subHint: "Import this link or scan the QR code in v2rayNG, Hiddify or Streisand.",
    confirmDelete: "Delete {x}? Usage history is kept.", node: "Node", preset: "Preset", tag: "Tag", port: "Port", sni: "REALITY target (SNI)", findSni: "Find SNI", updateNode: "Update agent", xrayVer: "Xray version", xrayPick: "Install Xray on {x}", xrayCur: "installed", xrayPre: "pre-release", xrayAsk: "Install Xray {v} on {x}? Connections drop for a few seconds; the previous version is kept.", xrayInstalling: "Downloading and installing Xray {v} on {x}…", xrayDone: "Xray on {x}: {v}", xrayHint: "Official XTLS builds, checked against the release checksum. Pick an older version to roll back.", freeCert: "Free certificate", acmeTitle: "Free certificates (Let's Encrypt)", acmeSettings: "CA settings", acmeEmail: "Email for expiry notices (optional)", acmeCA: "Certificate authority", acmeStaging: "Let's Encrypt staging (test, untrusted)", cfToken: "Cloudflare API token (Zone · DNS · Edit)", cfSaved: "saved — leave empty to keep, - to remove", acmeDomains: "Domains (one per line, *.example.com for wildcard)", acmeMethod: "Prove the domain via", acmeDNS: "Cloudflare DNS (wildcards, no ports)", acmeHTTP: "HTTP on the node (port 80 must be free)", acmeNodeAuto: "auto: the node the domain points to", acmeHint: "The certificate is stored under this id and renewed automatically 30 days before expiry; inbounds using it pick up each renewal.", st_pending: "waiting", st_issuing: "issuing…", st_ok: "issued", st_failed: "failed", renew: "Renew now", acmeEmpty: "No free certificates yet.", updateAvail: "update available", updateAsk: "Update the agent on {x}? Connections drop for a few seconds while it restarts.", updating: "Updating {x}…", updated: "{x} updated", updateFail: "Update failed: {x}", sniScan: "Scan", sniScanning: "Scanning from {x}… {s}s", sniNeighbors: "Also scan neighbours in the node's datacenter (/24)",
    sniCustom: "Your own domains (one per line, empty = built-in list)", sniHint: "Probes each domain from the node itself: TLS 1.3, HTTP/2, a valid certificate and handshake time. The best REALITY targets are fast, TLS 1.3 + h2, and ideally in the same datacenter.",
    sniUse: "Use", sniFailed: "{x} unusable (no TLS 1.3/h2, bad certificate or unreachable)", sniExpired: "The node did not answer. Is it online and updated?", neighbor: "neighbour", latency: "Handshake", score: "Score",
    path: "Path / service name (optional)", remark: "Remark", directAddr: "Public address", tunnelAddr: "Tunnel entry (optional, address:port)",
    addNode: "Add node", nodeId: "Node ID (e.g. de-1)", nodeToken: "Node token — shown once, keep it safe", online: "Online", offline: "Offline",
    restarts: "Xray starts", spooled: "Waiting batches", language: "Language", theme: "Theme", system: "System", light: "Light", dark: "Dark",
    logout: "Sign out", empty: "Nothing here yet.", saved: "Saved", failed: "Failed: {e}", clientsCount: "{n} clients", lastError: "Last error",
    reps: "Resellers", changePassword: "Change password", oldPassword: "Current password", newPassword: "New password (10+ chars)",
    credit: "Credit", volumeCredit: "Volume credit", units: "Unlimited units", addCredit: "Add credit", unitsHint: "1 unit = 1 user × 30 days",
    plan: "Plan", planVolume: "Volume", planUnlimited: "Unlimited", repId: "Reseller ID", loginUser: "Login username", loginPass: "Login password",
    installCmd: "Run this on the new server as root (link works once, for 1 hour):", note: "Note", reinstall: "New install link",
    rename: "Rename", groups: "Groups", members: "Members", bulkRenew: "Bulk renew", addDays: "Add days", addGB: "Add GB",
    selected: "{n} selected", changed: "{n} clients updated", all: "All", filterOnline: "Online", filterExpiring: "Expiring (≤3 days)",
    filterExpired: "Expired / used up", filterDisabled: "Disabled", group: "Group", contact: "Contact / note", transfer: "Move to reseller",
    ownerSelf: "Owner (me)", confirmNodeDelete: "Delete node {x} and its inbounds? Clients and usage stay.", selectAll: "Select all",
    backup: "Backup", backupHint: "Download a full snapshot (nodes, inbounds and keys, clients, resellers, credit, usage). The HUB also keeps 14 daily snapshots.", download: "Download backup",
    finance: "Finance", activity: "Activity", policy: "Limits", usageChart: "Daily usage", payment: "Record payment", amount: "Amount",
    maxClients: "Max clients (0 = no cap)", maxDays: "Longest term, days (0 = no cap)", maxClientGB: "Largest client volume, GB (0 = no cap)",
    allowUnlimited: "May sell unlimited plans", canDelete: "May delete clients", refundUnused: "Return unused volume on delete",
    allowedInbounds: "Allowed inbounds (none ticked = all)", priceGB: "Price per GB", priceUnit: "Price per unlimited unit",
    period: "Period", last30: "Last 30 days", last7: "Last 7 days", allTime: "All time", sold: "Sold", refunded: "Returned", granted: "Credit given",
    paid: "Paid", revenue: "Revenue", debt: "Balance due", balance: "Remaining credit", refundDone: "Returned {v} to your credit",
    kind_adjust: "Credit change", kind_charge: "Sale", kind_refund: "Return", kind_delete: "Deleted", kind_payment: "Payment",
    advanced: "Advanced editor", template: "Xray inbound (JSON, without clients)", metaJson: "Subscription settings (JSON)", invalidJson: "Invalid JSON in {x}",
    enable: "Enable", disable: "Disable", advancedHint: "Clients are injected by the HUB. Nodes pick up changes within a few seconds without dropping other inbounds.",
    swap: "Dark Swap", swapHint: "Relay this inbound through another node: the relay listens on its own port and forwards TCP to this inbound's Direct address. Users and accounting stay on the inbound's node. Point your entry server (e.g. in Iran) at relay address:relay port.",
    relayNode: "Relay node", relayPort: "Relay port", entryAddr: "Entry address (in the link)", entryPort: "Entry port", forwardsTo: "forwards to",
    traffic14: "Traffic, last 14 days", topToday: "Top users today", recentEvents: "Recent events", noEvents: "No blocks — every limit is quiet.",
    certs: "TLS certificates", addCert: "Add certificate", certPem: "Certificate (PEM, full chain)", keyPem: "Private key (PEM)",
    certFor: "Certificate (TLS presets)", noCert: "— none —", usedBy: "used by", certHint: "Paste the full chain and key (e.g. from certbot or Cloudflare origin). Uploading again with the same ID renews it on every inbound and node.",
    sellers: "Sub-sellers", transferCredit: "Transfer credit", subOf: "under", kind_transfer: "Transfer",
    sellersHint: "Open sellers under you and give them part of your credit. Their limits can only be tighter than yours.",
    transferHint: "Moves credit from your balance to this seller. Negative numbers take unused credit back.",
    shop: "Shop", products: "Products", orders: "Orders", newOrder: "New order", product: "Product", price: "Price", buyer: "Buyer (name, @telegram, phone)",
    renewClient: "Renew an existing client (optional)", fulfill: "Deliver", cancelOrder: "Cancel", status_pending: "Pending", status_fulfilled: "Delivered",
    status_cancelled: "Cancelled", status_draft: "Draft", status_published: "Published", status_archived: "Archived", statusLabel: "Status", seller: "Seller",
    delivered: "Delivered to {x}", manageProducts: "May create own shop products", shopHint: "Orders are recorded here; delivering one creates (or renews) the client in one step. A reseller's orders are paid from that reseller's credit.",
    newClient: "New client", firstUseShort: "starts on first use",
    telegram: "Telegram", telegramHint: "Connect your Telegram to get your order cards with Deliver/Cancel, your credit and your clients in the bot.",
    connectTelegram: "Connect Telegram", openBot: "Open the bot", codeExpires: "The link works once, for 15 minutes.",
    stepProfile: "Profile", stepNodes: "Nodes & routes", stepSwap: "Dark Swap", stepReview: "Review", next: "Next", back: "Back",
    newInbound: "New inbound", editInbound: "Nodes & routes", nodesRoutes: "Nodes & routes", pickNode: "Select at least one node.",
    clientTg: "Telegram", clientTgHint: "The customer's numeric Telegram id: the bot sends expiry and volume notices there and shows the service under \"My services\". Empty removes the link.", canCreate: "May create clients", canRenew: "May renew (add days or volume)", canEdit: "May change client settings (IP limit, on/off)", priceStars: "Price in Telegram Stars (0 = not sold for Stars)", priceStarsHint: "With a Stars price the bot and the store sell it for Telegram Stars: paid in Telegram and delivered at once, no approval. Customers can also pay with wallet credit.", tcTitle: "Telegram store: trial and referrals", tcTrial: "Free trial (one per Telegram account)", tcTrialOn: "Offer a free trial", tcGB: "GB (0 = unlimited)", tcHours: "Hours", tcRef: "Invite friends (wallet credit in Stars)", tcRefOn: "Reward customers who invite buyers", tcPercent: "% of the Stars price", tcFixed: "Fixed Stars per order", tcFirst: "Only the invited customer's first purchase", tcMax: "Most Stars per order (0 = no cap)", tcWallet: "Wallet of a customer", tcChat: "Telegram user id", tcDelta: "Add (+) or remove (−) Stars", tcBalance: "Balance: {n}⭐", tcLookup: "Show", tcApply: "Apply", migTitle: "Move from the old panel", migHint: "Brings resellers and sub-sellers with their credit, clients (same UUID, plan, IP limit, expiry, usage), groups, panel logins (old passwords keep working) and active subscription links (old links answer with the new subscription). The old panel is only read, never changed. Preview first: nothing is written until you press Import.", migSrc: "Source", migServer: "The old panel on this server", migUpload: "Upload its database file (business.sqlite)", migPath: "Path", migInbounds: "Give moved clients these inbounds", migArchived: "Also move archived clients (disabled)", migPreview: "Preview", migImport: "Import", migAsk: "Import into this panel now? Existing names are skipped, never overwritten.", migWorking: "Reading the old panel…", migDone: "Imported.", migWould: "Preview — nothing written yet:", migGot: "Done:", mig_reps: "resellers", mig_sellers: "sub-sellers", mig_clients: "clients", mig_disabled: "of them disabled", mig_usage: "with usage", mig_groups: "groups", mig_logins: "logins", mig_links: "old links kept", migSkipped: "Skipped", migNotes: "Notes", trustTitle: "IP limit exemptions", trustHint: "Addresses never counted toward a client's IP limit — an office or school NAT, a CDN or relay exit, a monitoring probe. One IP or network (CIDR) per line.", bulkCreate: "Create many", bcHint: "Creates numbered clients of one plan in one go, e.g. shop-001 … shop-050. If any name already exists nothing is created.", bcPrefix: "Name prefix", bcStart: "First number", bcCount: "How many", bcPad: "Digits", bcSuffix: "Suffix", bcVol: "Volume (GB, 0 = unlimited)", bcDays: "Days (0 = no expiry)", bcFirstUse: "Days start at the first connection", bcHold: "…or anyway after this many days unused (0 = wait)", bcIPs: "Devices (IP limit, 0 = any)", bcGroup: "Add to group", bcNone: "none", bcNote: "Note", bcPreview: "Names: {a} … {b}", bcDone: "{n} clients created", bcCSV: "Download names and links (CSV)", onHold: "On hold", onHoldHint: "Unused first-connection clients start anyway after this many days (0 = wait for the first connection).", onHoldUntil: "starts by {d} if unused", miniTitle: "Telegram store (Mini App)", miniHint: "Customers open the store from the bot: their services with usage, QR and links, the plans, and their orders. Telegram needs an HTTPS address with a real certificate — by default the public subscription address + /app/.", miniOn: "Show the store in the bot", miniURL: "Store address (empty = automatic)", wgSubnet: "Tunnel subnet (WireGuard)", phTitle: "Panel address (HTTPS)", phHint: "Serve the panel on its own port with a real certificate for your domain. Issue or upload a certificate that covers the domain (Inbounds → Certificates; Cloudflare DNS works without opening port 80), point the domain at this server, then save and restart.", phDomain: "Domain", phPort: "Port (0 = off)", phCert: "Certificate: {x} · {d} days left", phNoCert: "No certificate covers this domain yet — the panel answers with its self-signed one until there is.", phRunning: "Listening now on port {p}", phNotRunning: "Not listening yet", phRestart: "Save and restart the HUB", phRestartAsk: "Restart the HUB to apply? The panel and subscriptions are away for a few seconds; nodes keep running.", phOpen: "Open", whTitle: "Webhook", whHint: "Send what happens in the panel to another system as signed JSON (X-Dark-Signature: sha256=HMAC of the body). Items wait and are resent while the receiver is down. A Discord or Slack webhook address works directly — it gets readable messages.", whOn: "Send events", whURL: "Receiver address", whSecret: "Signing secret", whSecretSaved: "saved — empty keeps it, - removes it", whKinds: "What to send", wh_client: "Client events (expired, out of volume, too many IPs, resets, next plan, refused devices)", wh_change: "Changes in the panel (who changed what)", wh_node: "Node offline / back online", whTest: "Send a test", whTestOK: "The receiver answered OK", whLastOK: "Last delivered", whLastErr: "Last error", restoreTitle: "Restore a backup", restoreHint: "Upload a .sqlite.gz or encrypted .dkb backup. You see what is in it before anything changes; the current database is kept on the server.", restoreFile: "Backup file", restorePass: "Password (encrypted backups only)", restoreCheck: "Check backup", restoreChecking: "Checking…", restoreFound: "{c} clients · {n} nodes · {i} inbounds · {l} logins", restoreApply: "Restore and restart", restoreAsk: "Replace everything in the panel with this backup? The HUB restarts; nodes reconnect on their own. Sign in with a login from the backup.", restoreWait: "Restoring — the panel comes back in a few seconds…", xrayLogs: "Xray log", logsLoading: "Asking {x} for its log…", logsEmpty: "Nothing logged yet.", logsErrors: "Warnings and errors only", logsGrep: "Search (IP, email, domain…)", logsRefresh: "Refresh", restartXray: "Restart Xray", restartAsk: "Restart Xray on {x}? Connections drop for a second.", restarting: "Restarting Xray on {x}…", restarted: "Xray on {x} restarted", logsOld: "This node's agent is too old for logs — update the agent first.", keyDays: "Expires after (days, 0 = never)", keyUntil: "until", keysTitle: "API keys", keysHint: "For scripts, bots and other panels. Send it as Authorization: Bearer <key>. An admin key can change things, a viewer key only reads; staff, keys, backups and finance stay closed. Every change is in the change log.", keyName: "Name", addKey: "New key", keyOnce: "Copy this key now — it is not shown again.", keyDelAsk: "Delete the key {x}? Anything using it stops working.", keyNone: "No API keys yet.", apiRef: "API reference", acc_public: "public", acc_viewer: "read", acc_admin: "admin", acc_owner: "owner", acc_rep: "reseller", you: "you", staff: "Team", staffTitle: "Panel logins", staffHint: "Admins manage clients, inbounds, nodes and routing; viewers can only look. Staff, the change log, backups and finance stay with owners.", role: "Role", role_owner: "Owner", role_admin: "Admin", role_viewer: "Viewer", addLogin: "Add login", newPassOpt: "New password (empty = keep)", lastSeen: "Last change", tfaOn: "2FA on", tfaReset: "Reset 2FA", tfaResetAsk: "Turn off two-step login for {x}? They can sign in with the password alone until they set it up again.", delLoginAsk: "Delete the login {x}? Their sessions end now.", auditTitle: "Change log", auditHint: "Every change made in the panel or through the API, and every sign-in. Request contents are never stored.", auditFilter: "Filter by login, client or path", auditMore: "Older", act_login: "signed in", act_loginFail: "wrong password", act_denied: "refused", tfaTitle: "Two-step login", tfaHint: "Sign-in also asks for a 6-digit code from an authenticator app (Google Authenticator, Aegis, 2FAS…).", tfaStart: "Set up", tfaScan: "Scan this in the app, or enter the key by hand, then type the code it shows.", tfaKey: "Key", tfaCode: "6-digit code", tfaEnable: "Turn on", tfaEnabled: "Two-step login is on.", tfaDisable: "Turn off", tfaOff: "Two-step login is off.", tfaNeed: "Enter the code from your authenticator app.", fbHint: "One TLS port for everything: VLESS Vision answers directly, and VLESS, VMess and Trojan over WebSocket are passed on by path. Customers get all four in their subscription; traffic is counted under this inbound.", badTag: "Tag: lowercase letters, digits, dot, dash or underscore.", needCert: "TLS presets need a certificate (add one under TLS certificates).",
    profileHint: "One profile — protocol, transport, security and keys — shared by every node you place it on. Clients assigned to it work on all of them.",
    nodesHint: "Pick the nodes. Each gets its own listen IP, a Direct port and, if you want, a separate Tunnel port so their traffic is counted apart.",
    hostsHint: "Hosts are the addresses clients receive. Add as many as you like per path; ⚙ opens per-host SNI, Host, Path, ALPN, fingerprint and format filters.",
    direct: "Direct", tunnel: "Tunnel", route: "Path", address: "Address", listenIP: "Listen IP", pathOverride: "Path / service", hideIn: "Hide in:",
    destNode: "Destination node", targetAddr: "Relay target (optional)", targetAuto: "auto: node's public IP", noHosts: "no hosts",
    todayTasks: "Needs renewal soon", todayHint: "Ending within 3 days or 90% of volume used — renew them before they stop.",
    exportImport: "Clients export / import", exportHint: "JSON with each client's UUID, secret, subscription token, limits and usage — imported clients keep working with their old links. Import needs the same inbound tags.",
    exportClients: "Export clients", importClients: "Import clients", imported: "{n} clients imported",
    selfSigned: "Self-signed (issued by the HUB)",
    used: "Used", of: "of", last30Traffic: "Last 30 days", timeLeft: "Time left", details: "Details", firstSeen: "First connection", lastSeen: "Last seen",
    when: "When", who: "By", what: "What", more: "Load more", totalLast30: "{v} in the last 30 days",
    health: "Health (24h)", cpu: "CPU", ram: "RAM", disk: "Disk", net: "Network", uptime: "Uptime", inRate: "in", outRate: "out", noData: "No samples yet",
  },
  fa: {
    dashboard: "داشبورد", clients: "کلاینت‌ها", inbounds: "اینباندها", nodes: "نودها", routing: "مسیریابی", settings: "تنظیمات",
    login: "ورود", username: "نام کاربری", password: "رمز عبور", badLogin: "نام کاربری یا رمز اشتباه است",
    nodesOnline: "نود آنلاین", activeClients: "کلاینت فعال", onlineUsers: "آنلاین الان", trafficToday: "ترافیک امروز", trafficTotal: "کل ترافیک",
    add: "افزودن", edit: "ویرایش", delete: "حذف", save: "ذخیره", cancel: "انصراف", close: "بستن", copy: "کپی", copied: "کپی شد",
    search: "جستجو", name: "نام", volumeGB: "حجم (گیگ، ۰ = نامحدود)", days: "روز (۰ = بدون انقضا)", firstUse: "شروع از اولین اتصال",
    maxIPs: "حداکثر IP (۰ = بدون محدودیت)", enabled: "فعال", assignInbounds: "اینباندها", unlimited: "نامحدود", noExpiry: "بدون انقضا",
    notStarted: "از اولین اتصال شروع می‌شود", daysLeft: "{n} روز مانده", expired: "منقضی", disabled: "غیرفعال", exhausted: "حجم تمام شد",
    subscription: "لینک ساب", subHint: "این لینک را در v2rayNG، Hiddify یا Streisand وارد کنید یا QR را اسکن کنید.",
    confirmDelete: "{x} حذف شود؟ سابقهٔ مصرف حفظ می‌شود.", node: "نود", preset: "نوع", tag: "شناسه", port: "پورت", sni: "مقصد REALITY (SNI)", findSni: "یافتن SNI", updateNode: "به‌روزرسانی نود", xrayVer: "نسخهٔ Xray", xrayPick: "نصب Xray روی {x}", xrayCur: "نصب‌شده", xrayPre: "پیش‌انتشار", xrayAsk: "Xray نسخهٔ {v} روی {x} نصب شود؟ اتصال‌ها چند ثانیه قطع می‌شوند؛ نسخهٔ قبلی نگه داشته می‌شود.", xrayInstalling: "در حال دانلود و نصب Xray {v} روی {x}…", xrayDone: "Xray روی {x}: {v}", xrayHint: "نسخه‌های رسمی XTLS، با چک‌سام خود انتشار سنجیده می‌شوند. برای برگشت، نسخهٔ قدیمی‌تر را انتخاب کنید.", freeCert: "گواهی رایگان", acmeTitle: "گواهی رایگان (Let's Encrypt)", acmeSettings: "تنظیمات صادرکننده", acmeEmail: "ایمیل برای هشدار انقضا (اختیاری)", acmeCA: "صادرکننده", acmeStaging: "Let's Encrypt آزمایشی (فقط تست، نامعتبر)", cfToken: "توکن API کلودفلر (Zone · DNS · Edit)", cfSaved: "ذخیره شده — خالی بگذارید تا بماند، - برای حذف", acmeDomains: "دامنه‌ها (هر خط یکی؛ برای وایلدکارت *.example.com)", acmeMethod: "روش تأیید دامنه", acmeDNS: "DNS کلودفلر (وایلدکارت، بدون پورت)", acmeHTTP: "HTTP روی نود (پورت ۸۰ باید آزاد باشد)", acmeNodeAuto: "خودکار: نودی که دامنه به آن اشاره می‌کند", acmeHint: "گواهی با همین شناسه ذخیره و ۳۰ روز مانده به انقضا خودکار تمدید می‌شود؛ اینباندهایی که از آن استفاده می‌کنند هر تمدید را خودشان می‌گیرند.", st_pending: "در انتظار", st_issuing: "در حال صدور…", st_ok: "صادر شد", st_failed: "ناموفق", renew: "تمدید همین حالا", acmeEmpty: "هنوز گواهی رایگانی نساخته‌اید.", updateAvail: "نسخهٔ جدید آماده است", updateAsk: "نرم‌افزار نود {x} به‌روز شود؟ هنگام ری‌استارت، اتصال‌ها چند ثانیه قطع می‌شوند.", updating: "در حال به‌روزرسانی {x}…", updated: "{x} به‌روز شد", updateFail: "به‌روزرسانی ناموفق: {x}", sniScan: "اسکن", sniScanning: "در حال اسکن از {x}… {s} ثانیه", sniNeighbors: "سایت‌های همسایه در دیتاسنتر نود (/24) هم بررسی شوند",
    sniCustom: "دامنه‌های دلخواه (هر خط یکی، خالی = فهرست آماده)", sniHint: "هر دامنه از خود نود سنجیده می‌شود: TLS 1.3، HTTP/2، گواهی معتبر و زمان دست‌دهی. بهترین مقصد REALITY سریع است، TLS 1.3 و h2 دارد و ترجیحاً در همان دیتاسنتر است.",
    sniUse: "انتخاب", sniFailed: "{x} مورد قابل استفاده نیست (بدون TLS 1.3/h2، گواهی نامعتبر یا در دسترس نبود)", sniExpired: "نود جواب نداد. آنلاین و به‌روز است؟", neighbor: "همسایه", latency: "دست‌دهی", score: "امتیاز",
    path: "مسیر / نام سرویس (اختیاری)", remark: "نام نمایشی", directAddr: "آدرس عمومی", tunnelAddr: "ورودی تونل (اختیاری، آدرس:پورت)",
    addNode: "افزودن نود", nodeId: "شناسه نود (مثلاً de-1)", nodeToken: "توکن نود — فقط یک بار نشان داده می‌شود", online: "آنلاین", offline: "آفلاین",
    restarts: "دفعات اجرای Xray", spooled: "بستهٔ در انتظار", language: "زبان", theme: "پوسته", system: "سیستم", light: "روشن", dark: "تیره",
    logout: "خروج", empty: "هنوز چیزی نیست.", saved: "ذخیره شد", failed: "خطا: {e}", clientsCount: "{n} کلاینت", lastError: "آخرین خطا",
    reps: "نماینده‌ها", changePassword: "تغییر رمز", oldPassword: "رمز فعلی", newPassword: "رمز جدید (حداقل ۱۰ کاراکتر)",
    credit: "اعتبار", volumeCredit: "اعتبار حجمی", units: "واحد نامحدود", addCredit: "افزودن اعتبار", unitsHint: "هر واحد = ۱ کاربر × ۳۰ روز",
    plan: "نوع پلن", planVolume: "حجمی", planUnlimited: "نامحدود", repId: "شناسه نماینده", loginUser: "نام کاربری ورود", loginPass: "رمز ورود",
    installCmd: "این دستور را روی سرور جدید با root اجرا کنید (یک‌بار مصرف، ۱ ساعت):", note: "توضیح", reinstall: "لینک نصب جدید",
    rename: "تغییر نام", groups: "گروه‌ها", members: "اعضا", bulkRenew: "تمدید گروهی", addDays: "افزودن روز", addGB: "افزودن گیگ",
    selected: "{n} انتخاب شده", changed: "{n} کلاینت به‌روز شد", all: "همه", filterOnline: "آنلاین", filterExpiring: "رو به انقضا (≤۳ روز)",
    filterExpired: "منقضی / حجم تمام", filterDisabled: "غیرفعال", group: "گروه", contact: "تماس / یادداشت", transfer: "انتقال به نماینده",
    ownerSelf: "مالک (خودم)", confirmNodeDelete: "نود {x} و اینباندهایش حذف شود؟ کلاینت‌ها و مصرف حفظ می‌شوند.", selectAll: "انتخاب همه",
    backup: "بکاپ", backupHint: "دانلود نسخهٔ کامل (نودها، اینباندها و کلیدها، کلاینت‌ها، نماینده‌ها، اعتبار، مصرف). هاب ۱۴ بکاپ روزانه هم نگه می‌دارد.", download: "دانلود بکاپ",
    finance: "مالی", activity: "فعالیت", policy: "محدودیت‌ها", usageChart: "مصرف روزانه", payment: "ثبت پرداخت", amount: "مبلغ",
    maxClients: "حداکثر کلاینت (۰ = بدون سقف)", maxDays: "بیشترین مدت، روز (۰ = بدون سقف)", maxClientGB: "بیشترین حجم هر کلاینت، گیگ (۰ = بدون سقف)",
    allowUnlimited: "اجازهٔ فروش نامحدود", canDelete: "اجازهٔ حذف کلاینت", refundUnused: "برگشت حجم مصرف‌نشده هنگام حذف",
    allowedInbounds: "اینباندهای مجاز (هیچ‌کدام = همه)", priceGB: "قیمت هر گیگ", priceUnit: "قیمت هر واحد نامحدود",
    period: "بازه", last30: "۳۰ روز اخیر", last7: "۷ روز اخیر", allTime: "همهٔ زمان‌ها", sold: "فروش", refunded: "برگشتی", granted: "اعتبار داده‌شده",
    paid: "پرداختی", revenue: "درآمد", debt: "بدهی", balance: "اعتبار باقی‌مانده", refundDone: "{v} به اعتبار شما برگشت",
    kind_adjust: "تغییر اعتبار", kind_charge: "فروش", kind_refund: "برگشت", kind_delete: "حذف", kind_payment: "پرداخت",
    advanced: "ویرایشگر پیشرفته", template: "اینباند Xray (JSON، بدون کلاینت)", metaJson: "تنظیمات ساب (JSON)", invalidJson: "JSON نامعتبر در {x}",
    enable: "فعال کردن", disable: "غیرفعال کردن", advancedHint: "کلاینت‌ها را هاب اضافه می‌کند. نودها تغییر را ظرف چند ثانیه می‌گیرند و بقیهٔ اینباندها قطع نمی‌شوند.",
    swap: "Dark Swap", swapHint: "این اینباند را از یک نود دیگر رله کن: نود واسط روی پورت خودش گوش می‌دهد و TCP را به آدرس Direct این اینباند می‌فرستد. کاربر و حسابداری روی نود اصلی می‌ماند. سرور ورودی (مثلاً ایران) را به آدرس:پورت واسط بفرست.",
    relayNode: "نود واسط", relayPort: "پورت واسط", entryAddr: "آدرس ورودی (داخل لینک)", entryPort: "پورت ورودی", forwardsTo: "ارسال به",
    traffic14: "ترافیک ۱۴ روز اخیر", topToday: "پرمصرف‌های امروز", recentEvents: "رویدادهای اخیر", noEvents: "قطعی نداشته‌ایم؛ همهٔ محدودیت‌ها آرام‌اند.",
    certs: "گواهی‌های TLS", addCert: "افزودن گواهی", certPem: "گواهی (PEM، زنجیرهٔ کامل)", keyPem: "کلید خصوصی (PEM)",
    certFor: "گواهی (برای الگوهای TLS)", noCert: "— هیچ —", usedBy: "استفاده در", certHint: "زنجیرهٔ کامل و کلید را وارد کنید (مثلاً از certbot یا Cloudflare Origin). آپلود دوباره با همان شناسه، گواهی را روی همهٔ اینباندها و نودها تمدید می‌کند.",
    sellers: "زیرفروشنده‌ها", transferCredit: "انتقال اعتبار", subOf: "زیرمجموعهٔ", kind_transfer: "انتقال",
    sellersHint: "برای خودتان زیرفروشنده بسازید و بخشی از اعتبارتان را به او بدهید. محدودیت‌های او فقط می‌تواند سخت‌تر از شما باشد.",
    transferHint: "اعتبار را از موجودی شما به این فروشنده منتقل می‌کند. عدد منفی اعتبار مصرف‌نشده را برمی‌گرداند.",
    shop: "فروشگاه", products: "محصولات", orders: "سفارش‌ها", newOrder: "سفارش جدید", product: "محصول", price: "قیمت", buyer: "خریدار (نام، آیدی تلگرام، تلفن)",
    renewClient: "تمدید کلاینت موجود (اختیاری)", fulfill: "تحویل", cancelOrder: "لغو", status_pending: "در انتظار", status_fulfilled: "تحویل شد",
    status_cancelled: "لغو شد", status_draft: "پیش‌نویس", status_published: "منتشر", status_archived: "بایگانی", statusLabel: "وضعیت", seller: "فروشنده",
    delivered: "تحویل شد: {x}", manageProducts: "اجازهٔ ساخت محصول فروشگاه", shopHint: "سفارش‌ها اینجا ثبت می‌شوند؛ با «تحویل» کلاینت یک‌جا ساخته (یا تمدید) می‌شود. سفارش‌های نماینده از اعتبار همان نماینده پرداخت می‌شود.",
    newClient: "کلاینت جدید", firstUseShort: "شروع از اولین اتصال",
    telegram: "تلگرام", telegramHint: "تلگرامتان را وصل کنید تا سفارش‌ها با دکمهٔ تحویل/لغو، اعتبار و کلاینت‌هایتان در ربات بیاید.",
    connectTelegram: "اتصال تلگرام", openBot: "باز کردن ربات", codeExpires: "این لینک یک‌بار مصرف است و ۱۵ دقیقه اعتبار دارد.",
    stepProfile: "پروفایل", stepNodes: "نودها و مسیرها", stepSwap: "دارک سواپ", stepReview: "بازبینی", next: "بعدی", back: "قبلی",
    newInbound: "اینباند جدید", editInbound: "نودها و مسیرها", nodesRoutes: "نودها و مسیرها", pickNode: "حداقل یک نود انتخاب کنید.",
    clientTg: "تلگرام", clientTgHint: "شناسهٔ عددی تلگرام مشتری: ربات هشدار انقضا و حجم را آنجا می‌فرستد و سرویس را در «سرویس‌های من» نشان می‌دهد. خالی یعنی حذف اتصال.", canCreate: "اجازهٔ ساخت کاربر", canRenew: "اجازهٔ تمدید (افزودن روز یا حجم)", canEdit: "اجازهٔ تغییر تنظیمات کاربر (محدودیت IP، روشن/خاموش)", priceStars: "قیمت به استارز تلگرام (۰ = با استارز فروخته نمی‌شود)", priceStarsHint: "با قیمت استارز، ربات و فروشگاه این محصول را با استارز تلگرام می‌فروشند: پرداخت داخل تلگرام و تحویل فوری، بدون نیاز به تأیید. مشتری می‌تواند با اعتبار کیف پول هم بپردازد.", tcTitle: "فروشگاه تلگرام: تست رایگان و دعوت", tcTrial: "تست رایگان (یک بار برای هر حساب تلگرام)", tcTrialOn: "ارائهٔ تست رایگان", tcGB: "گیگ (۰ = نامحدود)", tcHours: "ساعت", tcRef: "دعوت دوستان (اعتبار کیف پول به استارز)", tcRefOn: "پاداش به مشتری‌ای که خریدار دعوت کند", tcPercent: "درصد از قیمت استارز", tcFixed: "استارز ثابت برای هر سفارش", tcFirst: "فقط اولین خرید دوست دعوت‌شده", tcMax: "سقف استارز هر سفارش (۰ = بدون سقف)", tcWallet: "کیف پول یک مشتری", tcChat: "شناسهٔ عددی تلگرام", tcDelta: "افزودن (+) یا کم کردن (−) استارز", tcBalance: "موجودی: {n}⭐", tcLookup: "نمایش", tcApply: "اعمال", migTitle: "انتقال از پنل قدیمی", migHint: "نماینده‌ها و زیرفروشنده‌ها با اعتبارشان، کاربرها (همان UUID، پلن، محدودیت IP، انقضا و مصرف)، گروه‌ها، ورودهای پنل (رمزهای قبلی کار می‌کنند) و لینک‌های اشتراک فعال (لینک قدیمی اشتراک جدید را برمی‌گرداند) منتقل می‌شوند. پنل قدیمی فقط خوانده می‌شود و تغییری نمی‌کند. اول پیش‌نمایش بگیرید: تا «انتقال» را نزنید چیزی نوشته نمی‌شود.", migSrc: "منبع", migServer: "پنل قدیمی روی همین سرور", migUpload: "بارگذاری فایل دیتابیس آن (business.sqlite)", migPath: "مسیر", migInbounds: "این اینباندها به کاربرهای منتقل‌شده داده شود", migArchived: "کاربرهای بایگانی‌شده هم منتقل شوند (غیرفعال)", migPreview: "پیش‌نمایش", migImport: "انتقال", migAsk: "همین حالا به این پنل منتقل شود؟ نام‌های موجود رد می‌شوند و هرگز بازنویسی نمی‌شوند.", migWorking: "در حال خواندن پنل قدیمی…", migDone: "منتقل شد.", migWould: "پیش‌نمایش — هنوز چیزی نوشته نشده:", migGot: "انجام شد:", mig_reps: "نماینده", mig_sellers: "زیرفروشنده", mig_clients: "کاربر", mig_disabled: "از آن‌ها غیرفعال", mig_usage: "با سابقهٔ مصرف", mig_groups: "گروه", mig_logins: "ورود پنل", mig_links: "لینک قدیمی حفظ‌شده", migSkipped: "رد شده", migNotes: "توضیحات", trustTitle: "استثناهای محدودیت IP", trustHint: "آدرس‌هایی که در محدودیت IP کاربر شمرده نمی‌شوند — NAT دفتر یا مدرسه، خروجی CDN یا ریلی، سرویس مانیتورینگ. هر خط یک IP یا شبکه (CIDR).", bulkCreate: "ساخت گروهی", bcHint: "کاربرهای شماره‌دار با یک پلن را یک‌جا می‌سازد، مثل shop-001 تا shop-050. اگر یکی از نام‌ها از قبل باشد هیچ کاربری ساخته نمی‌شود.", bcPrefix: "پیشوند نام", bcStart: "شماره شروع", bcCount: "تعداد", bcPad: "تعداد رقم", bcSuffix: "پسوند", bcVol: "حجم (گیگ، ۰ = نامحدود)", bcDays: "روز (۰ = بدون انقضا)", bcFirstUse: "روزها از اولین اتصال شروع شود", bcHold: "…یا اگر استفاده نشد بعد از این تعداد روز (۰ = صبر کن)", bcIPs: "دستگاه (محدودیت IP، ۰ = آزاد)", bcGroup: "افزودن به گروه", bcNone: "هیچ", bcNote: "یادداشت", bcPreview: "نام‌ها: {a} … {b}", bcDone: "{n} کاربر ساخته شد", bcCSV: "دانلود نام‌ها و لینک‌ها (CSV)", onHold: "در انتظار شروع", onHoldHint: "کاربرهای «شروع از اولین اتصال» اگر استفاده نکنند بعد از این تعداد روز خودکار شروع می‌شوند (۰ = تا اولین اتصال صبر کن).", onHoldUntil: "اگر استفاده نشود تا {d} شروع می‌شود", miniTitle: "فروشگاه تلگرام (Mini App)", miniHint: "مشتری فروشگاه را از داخل ربات باز می‌کند: سرویس‌هایش با مصرف، QR و لینک، پلن‌ها و سفارش‌هایش. تلگرام آدرس HTTPS با گواهی معتبر می‌خواهد — پیش‌فرض آدرس عمومی اشتراک + /app/ است.", miniOn: "نمایش فروشگاه در ربات", miniURL: "آدرس فروشگاه (خالی = خودکار)", wgSubnet: "زیرشبکهٔ تونل (WireGuard)", phTitle: "آدرس پنل (HTTPS)", phHint: "پنل را روی پورت جدا با گواهی واقعی برای دامنهٔ خودتان بالا بیاورید. گواهی‌ای که دامنه را پوشش دهد بگیرید یا بارگذاری کنید (اینباندها ← گواهی‌ها؛ DNS کلودفلر بدون باز کردن پورت ۸۰ کار می‌کند)، دامنه را به این سرور وصل کنید، بعد ذخیره و ری‌استارت کنید.", phDomain: "دامنه", phPort: "پورت (۰ = خاموش)", phCert: "گواهی: {x} · {d} روز مانده", phNoCert: "هنوز گواهی‌ای برای این دامنه نیست — تا وقتی نباشد پنل با گواهی خودامضا جواب می‌دهد.", phRunning: "الان روی پورت {p} در حال کار", phNotRunning: "هنوز فعال نیست", phRestart: "ذخیره و ری‌استارت هاب", phRestartAsk: "برای اعمال، هاب ری‌استارت شود؟ پنل و اشتراک‌ها چند ثانیه در دسترس نیستند؛ نودها کار می‌کنند.", phOpen: "باز کردن", whTitle: "وب‌هوک", whHint: "اتفاق‌های پنل را به صورت JSON امضاشده به سیستم دیگری بفرستید (X-Dark-Signature: sha256=HMAC بدنه). وقتی گیرنده در دسترس نباشد، موارد صبر می‌کنند و دوباره فرستاده می‌شوند. آدرس وب‌هوک دیسکورد یا اسلک هم مستقیم کار می‌کند و پیام خوانا می‌گیرد.", whOn: "ارسال رویدادها", whURL: "آدرس گیرنده", whSecret: "کلید امضا", whSecretSaved: "ذخیره شده — خالی یعنی بدون تغییر، - یعنی حذف", whKinds: "چه چیزهایی فرستاده شود", wh_client: "رویدادهای کاربر (انقضا، اتمام حجم، IP زیاد، ریست، برنامهٔ بعدی، دستگاه ردشده)", wh_change: "تغییرات پنل (چه کسی چه چیزی را عوض کرد)", wh_node: "قطع و وصل شدن نودها", whTest: "ارسال آزمایشی", whTestOK: "گیرنده جواب درست داد", whLastOK: "آخرین ارسال موفق", whLastErr: "آخرین خطا", restoreTitle: "بازگردانی بکاپ", restoreHint: "فایل .sqlite.gz یا بکاپ رمزدار .dkb را بارگذاری کنید. قبل از هر تغییری محتوایش را می‌بینید؛ دیتابیس فعلی روی سرور نگه داشته می‌شود.", restoreFile: "فایل بکاپ", restorePass: "رمز (فقط برای بکاپ رمزدار)", restoreCheck: "بررسی بکاپ", restoreChecking: "در حال بررسی…", restoreFound: "{c} کاربر · {n} نود · {i} اینباند · {l} ورود", restoreApply: "بازگردانی و ری‌استارت", restoreAsk: "همه چیز پنل با این بکاپ جایگزین شود؟ هاب ری‌استارت می‌شود و نودها خودشان دوباره وصل می‌شوند. با یکی از ورودهای داخل بکاپ وارد شوید.", restoreWait: "در حال بازگردانی — پنل تا چند ثانیه دیگر برمی‌گردد…", xrayLogs: "لاگ Xray", logsLoading: "در حال گرفتن لاگ از {x}…", logsEmpty: "هنوز چیزی ثبت نشده.", logsErrors: "فقط هشدارها و خطاها", logsGrep: "جستجو (IP، ایمیل، دامنه…)", logsRefresh: "تازه کردن", restartXray: "ری‌استارت Xray", restartAsk: "Xray روی {x} ری‌استارت شود؟ اتصال‌ها یک لحظه قطع می‌شوند.", restarting: "در حال ری‌استارت Xray روی {x}…", restarted: "Xray روی {x} ری‌استارت شد", logsOld: "عامل این نود برای لاگ قدیمی است — اول عامل را به‌روز کنید.", keyDays: "انقضا بعد از (روز، ۰ = هرگز)", keyUntil: "تا", keysTitle: "کلیدهای API", keysHint: "برای اسکریپت، ربات و پنل‌های دیگر. به شکل Authorization: Bearer <کلید> بفرستید. کلید مدیر می‌تواند تغییر دهد و کلید ناظر فقط می‌خواند؛ تیم، کلیدها، بکاپ و مالی بسته می‌ماند. هر تغییر در گزارش تغییرات ثبت می‌شود.", keyName: "نام", addKey: "کلید جدید", keyOnce: "همین حالا این کلید را کپی کنید — دوباره نشان داده نمی‌شود.", keyDelAsk: "کلید {x} حذف شود؟ هر چیزی که از آن استفاده می‌کند از کار می‌افتد.", keyNone: "هنوز کلید API ندارید.", apiRef: "مرجع API", acc_public: "عمومی", acc_viewer: "خواندن", acc_admin: "مدیر", acc_owner: "مالک", acc_rep: "نماینده", you: "شما", staff: "تیم", staffTitle: "ورودهای پنل", staffHint: "مدیر کمکی کاربران، اینباندها، نودها و مسیریابی را مدیریت می‌کند؛ ناظر فقط می‌بیند. تیم، گزارش تغییرات، بکاپ و مالی فقط دست مالک است.", role: "نقش", role_owner: "مالک", role_admin: "مدیر کمکی", role_viewer: "ناظر", addLogin: "افزودن ورود", newPassOpt: "رمز جدید (خالی = بدون تغییر)", lastSeen: "آخرین تغییر", tfaOn: "دومرحله‌ای فعال", tfaReset: "حذف دومرحله‌ای", tfaResetAsk: "ورود دومرحله‌ای {x} خاموش شود؟ تا دوباره راه‌اندازی کند فقط با رمز وارد می‌شود.", delLoginAsk: "ورود {x} حذف شود؟ نشست‌هایش همین حالا بسته می‌شود.", auditTitle: "گزارش تغییرات", auditHint: "هر تغییری که در پنل یا با API انجام شود و هر ورود. محتوای درخواست‌ها هرگز ذخیره نمی‌شود.", auditFilter: "فیلتر با نام ورود، کاربر یا مسیر", auditMore: "قدیمی‌تر", act_login: "وارد شد", act_loginFail: "رمز اشتباه", act_denied: "رد شد", tfaTitle: "ورود دومرحله‌ای", tfaHint: "هنگام ورود یک کد ۶ رقمی از برنامهٔ احراز هویت (Google Authenticator، Aegis، 2FAS و…) هم پرسیده می‌شود.", tfaStart: "راه‌اندازی", tfaScan: "این را در برنامه اسکن کنید یا کلید را دستی وارد کنید، بعد کدی که نشان می‌دهد را بنویسید.", tfaKey: "کلید", tfaCode: "کد ۶ رقمی", tfaEnable: "فعال کن", tfaEnabled: "ورود دومرحله‌ای فعال شد.", tfaDisable: "خاموش کن", tfaOff: "ورود دومرحله‌ای خاموش شد.", tfaNeed: "کد برنامهٔ احراز هویت را وارد کنید.", fbHint: "یک پورت TLS برای همه: VLESS Vision مستقیم جواب می‌دهد و VLESS، VMess و Trojan روی WebSocket بر اساس مسیر تحویل داده می‌شوند. مشتری هر چهار را در اشتراکش می‌گیرد و ترافیک به حساب همین اینباند نوشته می‌شود.", badTag: "شناسه: حروف کوچک انگلیسی، عدد، نقطه، خط تیره یا زیرخط.", needCert: "الگوهای TLS گواهی لازم دارند (از بخش گواهی‌های TLS اضافه کنید).",
    profileHint: "یک پروفایل — پروتکل، شبکه، امنیت و کلیدها — که روی همهٔ نودهای انتخابی یکسان است. کلاینتِ این اینباند روی همهٔ آن‌ها کار می‌کند.",
    nodesHint: "نودها را انتخاب کنید. هر نود IP گوش‌دادن، پورت Direct و در صورت نیاز پورت جدای Tunnel دارد تا مصرف هر مسیر جدا شمرده شود.",
    hostsHint: "هاست‌ها آدرس‌هایی هستند که در لینک کاربر می‌آیند. برای هر مسیر هر چند تا خواستید اضافه کنید؛ ⚙ تنظیمات SNI، Host، Path، ALPN، Fingerprint و فیلتر فرمت هر هاست را باز می‌کند.",
    direct: "Direct", tunnel: "Tunnel", route: "مسیر", address: "آدرس", listenIP: "IP گوش‌دادن", pathOverride: "Path / سرویس", hideIn: "مخفی در:",
    destNode: "نود مقصد", targetAddr: "مقصد رله (اختیاری)", targetAuto: "خودکار: IP عمومی نود", noHosts: "بدون هاست",
    todayTasks: "کارهای امروز: نیازمند تمدید", todayHint: "تا ۳ روز دیگر تمام می‌شوند یا ۹۰٪ حجمشان مصرف شده — قبل از قطع شدن تمدید کنید.",
    exportImport: "خروجی / ورودی کلاینت‌ها", exportHint: "فایل JSON شامل UUID، رمز، توکن ساب، محدودیت‌ها و مصرف هر کلاینت — کلاینت‌های واردشده با همان لینک قبلی کار می‌کنند. ورودی به همان شناسه‌های اینباند نیاز دارد.",
    exportClients: "خروجی کلاینت‌ها", importClients: "ورود کلاینت‌ها", imported: "{n} کلاینت وارد شد",
    selfSigned: "خودامضا (هاب صادر می‌کند)",
    used: "مصرف", of: "از", last30Traffic: "۳۰ روز اخیر", timeLeft: "زمان باقی‌مانده", details: "جزئیات", firstSeen: "اولین اتصال", lastSeen: "آخرین فعالیت",
    when: "زمان", who: "توسط", what: "شرح", more: "بیشتر", totalLast30: "{v} در ۳۰ روز اخیر",
    health: "سلامت (۲۴ ساعت)", cpu: "پردازنده", ram: "رم", disk: "دیسک", net: "شبکه", uptime: "روشن بودن", inRate: "ورودی", outRate: "خروجی", noData: "هنوز نمونه‌ای نیست",
  },
};
let lang = safeGet("lang") === "fa" ? "fa" : "en";
const t = (k, vars = {}) => (I18N[lang][k] ?? I18N.en[k] ?? k).replace(/\{(\w+)\}/g, (_, v) => vars[v] ?? "");
const fmtNum = (n) => new Intl.NumberFormat(lang === "fa" ? "fa-IR" : "en-US", { maximumFractionDigits: 1 }).format(n);

function safeGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function safeSet(k, v) { try { v == null ? localStorage.removeItem(k) : localStorage.setItem(k, v); } catch {} }

// Tiny element builder: never uses innerHTML, so names from the API can't inject markup.
function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else if (k === "style") el.style.cssText = v; // CSSOM: allowed under a strict style-src CSP
    else el.setAttribute(k, v === true ? "" : v);
  }
  // In Persian a middle dot reads like the digit zero (۰): use the Persian comma.
  const txt = (x) => (lang === "fa" && /[\u0600-\u06ff]/.test(x) ? String(x).replaceAll(" · ", "، ") : String(x));
  for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid instanceof Node ? kid : txt(kid));
  return el;
}

function bytes(n) {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  // Left-to-right isolate: "۳۴٫۸ GB" keeps its order inside Persian text.
  return `\u2066${fmtNum(n)} ${u[i]}\u2069`;
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method, credentials: "same-origin",
    headers: { "Content-Type": "application/json", "X-Dark-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401 && !path.startsWith("/api/v2/auth/login")) { location.hash = "#/login"; throw new Error("unauthorized"); }
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || res.statusText);
  return text ? JSON.parse(text) : null;
}

function toast(msg) {
  const el = document.getElementById("toast");
  el.textContent = msg;
  try { el.showPopover(); } catch {}
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => { try { el.hidePopover(); } catch {} }, 2200);
}

async function copy(text) {
  try { await navigator.clipboard.writeText(text); toast(t("copied")); } catch { prompt(t("copy"), text); }
}

// One shared native <dialog>; content is rebuilt per use.
function openModal(title, body, actions = []) {
  const d = document.getElementById("modal");
  d.replaceChildren(
    h("h2", { id: "modal-title" }, title), body,
    h("form", { method: "dialog", class: "actions" },
      ...actions, h("button", { value: "cancel", autofocus: actions.length === 0 }, actions.length ? t("cancel") : t("close"))),
  );
  d.returnValue = "";
  d.showModal();
  return d;
}

function confirmAction(text, label) {
  return new Promise((resolve) => {
    const d = openModal(text, h("p", { class: "muted" }, ""), [h("button", { value: "ok", class: "danger" }, label || t("delete"))]);
    d.addEventListener("close", () => { resolve(d.returnValue === "ok"); d.returnValue = ""; }, { once: true });
  });
}

// ---------- Views ----------
const allRoutes = { dashboard: viewDashboard, online: viewOnline, clients: viewClients, groups: viewGroups, inbounds: viewInbounds, nodes: viewNodes, routing: viewRouting, reps: viewReps, shop: viewShop, finance: viewFinance, activity: viewActivity, staff: viewStaff, settings: viewSettings };
let me = null;
let routes = allRoutes;
function fullOwner() { return me && me.role === "owner" && !me.level; }
function routesFor(m) {
  if (m && m.role === "owner" && m.level) {
    const r = { ...allRoutes };
    delete r.finance; delete r.staff;
    return r;
  }
  if (m && m.role === "rep") {
    const r = { clients: viewClients, online: viewOnline };
    if (m.can_sell) r.sellers = viewSellers;
    r.shop = viewShop; r.activity = viewActivity; r.settings = viewSettings;
    return r;
  }
  return allRoutes;
}

// Brand mark and icons are built with the DOM (strict CSP, no image files).
const SVGNS = "http://www.w3.org/2000/svg";
function svgEl(n, a) { const x = document.createElementNS(SVGNS, n); for (const [k, v] of Object.entries(a)) x.setAttribute(k, v); return x; }
let markSeq = 0;
function mark() {
  const id = `g${++markSeq}`;
  const svg = svgEl("svg", { viewBox: "0 0 32 32", "aria-hidden": "true" });
  const defs = svgEl("defs", {});
  const g = svgEl("linearGradient", { id, x1: "0", y1: "0", x2: "1", y2: "1" });
  g.append(svgEl("stop", { offset: "0", "stop-color": "#2de2e6" }), svgEl("stop", { offset: "1", "stop-color": "#9b5cff" }));
  defs.append(g);
  svg.append(defs,
    svgEl("path", { d: "M16 2.5 28 9.25v13.5L16 29.5 4 22.75V9.25z", fill: "none", stroke: `url(#${id})`, "stroke-width": 2.2, "stroke-linejoin": "round" }),
    svgEl("circle", { cx: 12.5, cy: 19.5, r: 2.2, fill: `url(#${id})` }),
    svgEl("path", { d: "M12.5 14.5a5 5 0 0 1 5 5", stroke: `url(#${id})`, "stroke-width": 2.2, fill: "none", "stroke-linecap": "round" }),
    svgEl("path", { d: "M12.5 10a9.5 9.5 0 0 1 9.5 9.5", stroke: `url(#${id})`, "stroke-width": 2.2, fill: "none", "stroke-linecap": "round" }));
  return svg;
}
const ICONS = {
  dashboard: "M3 13h8V3H3zM13 21h8V11h-8zM3 21h8v-6H3zM13 3v6h8V3z",
  clients: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75",
  groups: "M12 2 2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5",
  inbounds: "M6 3v6M18 3v6M4 9h16v3a8 8 0 0 1-16 0zM12 20v2",
  nodes: "M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01",
  reps: "M3 7h18v13H3zM8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M3 13h18",
  sellers: "M15 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M8.5 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM20 8v6M23 11h-6",
  shop: "M6 2 3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4zM3 6h18M16 10a4 4 0 0 1-8 0",
  finance: "M3 3v18h18M7 15l4-4 3 3 6-6",
  activity: "M22 12h-4l-3 9L9 3l-3 9H2",
  staff: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10zM9 12l2 2 4-4",
  settings: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  routing: "M6 3v12M18 9v12M6 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM18 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM6 15c0-4 12-2 12-6",
};
function icon(name) {
  const svg = svgEl("svg", { viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": 1.8, "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true" });
  svg.append(svgEl("path", { d: ICONS[name] || ICONS.dashboard }));
  return svg;
}

function shell(active, content) {
  const tabs = Object.keys(routes).map((r) => h("a", { href: `#/${r}`, "aria-current": r === active ? "page" : null }, icon(r), h("span", {}, t(r))));
  return h("div", { class: "shell" },
    h("header", { class: "top" }, h("span", { class: "brand" }, mark(), "Dark VPN"),
      me && me.role === "rep" ? h("span", { class: "badge" }, `${t("volumeCredit")}: ${bytes(me.volume_credit || 0)} · ${t("units")}: ${fmtNum(me.unlimited_units || 0)}`) : null,
      h("span", { class: "muted small" }, me ? me.user : "")),
    h("nav", { class: "tabs", "aria-label": "Main" }, tabs),
    h("main", {}, content));
}

function viewLogin() {
  const err = h("div", { class: "error", role: "alert" });
  const user = h("input", { name: "u", autocomplete: "username", required: true, autofocus: true });
  const pass = h("input", { name: "p", type: "password", autocomplete: "current-password", required: true });
  const code = h("input", { name: "c", inputmode: "numeric", autocomplete: "one-time-code", pattern: "[0-9 ]{6,7}", maxlength: "7", dir: "ltr" });
  const codeRow = h("label", { hidden: true }, h("span", {}, t("tfaCode")), code);
  const btn = h("button", { class: "primary", type: "submit" }, t("login"));
  const form = h("form", { class: "form", onsubmit: async (e) => {
    e.preventDefault(); btn.disabled = true; err.textContent = "";
    try { await api("POST", "/api/v2/auth/login", { Username: user.value, Password: pass.value, Code: code.value }); location.hash = "#/dashboard"; }
    catch (x) {
      if (String(x.message).includes("need_code")) { codeRow.hidden = false; err.textContent = codeRow.dataset.asked ? t("badLogin") : t("tfaNeed"); codeRow.dataset.asked = "1"; code.value = ""; code.focus(); }
      else err.textContent = t("badLogin");
    } finally { btn.disabled = false; }
  } }, h("label", {}, h("span", {}, t("username")), user), h("label", {}, h("span", {}, t("password")), pass), codeRow, err, btn);
  return h("div", { class: "login" }, h("div", { class: "card" }, h("h1", {}, mark(), "Dark VPN"), form));
}

// ---------- HUB health, online now, traffic per node ----------
const S3 = {
  fa: { hubTitle: "سرور هاب", cpu: "پردازنده", ram: "حافظه", disk: "دیسک", db: "دیتابیس", backups: "بکاپ‌ها ({n})", version: "نسخه", up: "روشن از",
    diskWarn: "دیسک سرور هاب {p}٪ پر است. اگر پر شود هاب نمی‌تواند مصرف کاربرها و تغییرات را ذخیره کند؛ فایل‌های اضافهٔ سرور را پاک کنید. هر روز یک بار در تلگرام هم یادآوری می‌شود.",
    ramWarn: "حافظهٔ سرور هاب {p}٪ پر است.",
    online: "آنلاین", onlineHint: "کاربرهایی که همین حالا وصل‌اند (چند ثانیه یک بار تازه می‌شود). روی نام بزنید تا صفحهٔ کاربر باز شود.", onlineNone: "الان کسی وصل نیست.",
    onSearch: "جستجوی نام یا IP", onNode: "نود", onIPs: "IPها", onToday: "امروز روی این نود", onSeen: "آخرین گزارش", onTotal: "{n} کاربر روی {m} نود",
    ntTitle: "ترافیک هر نود", ntHint: "مجموع مصرف کاربرها روی هر نود در هر روز.", ntDays: "{n} روز", updAll: "به‌روزرسانی همهٔ عامل‌ها ({n})",
    updAllAsk: "عامل {n} نود به آخرین نسخه به‌روز شود؟ هر نود چند ثانیه Xray را دوباره بالا می‌آورد.", updStarted: "به‌روزرسانی {n} نود شروع شد" },
  en: { hubTitle: "HUB server", cpu: "CPU", ram: "Memory", disk: "Disk", db: "Database", backups: "Backups ({n})", version: "Version", up: "Up for",
    diskWarn: "The HUB's disk is {p}% full. When it fills up the HUB cannot save usage or changes — delete unneeded files on the server. A reminder also goes to Telegram once a day.",
    ramWarn: "The HUB's memory is {p}% used.",
    online: "Online", onlineHint: "Clients connected right now (refreshes every few seconds). Tap a name to open the client.", onlineNone: "Nobody is connected right now.",
    onSearch: "Search name or IP", onNode: "Node", onIPs: "IPs", onToday: "Today on this node", onSeen: "Last report", onTotal: "{n} clients on {m} nodes",
    ntTitle: "Traffic per node", ntHint: "Client traffic summed per node and day.", ntDays: "{n} days", updAll: "Update all agents ({n})",
    updAllAsk: "Update the agent on {n} nodes to the latest version? Each node restarts Xray for a few seconds.", updStarted: "Update started on {n} nodes" },
};
const s3 = (k, vars) => { let s = (S3[lang] || S3.en)[k] ?? S3.en[k] ?? k; for (const [a, b] of Object.entries(vars || {})) s = s.replaceAll(`{${a}}`, b); return s; };
I18N.en.online = S3.en.online; I18N.fa.online = S3.fa.online;
ICONS.online = "M5 12.55a11 11 0 0 1 14.08 0M1.42 9a16 16 0 0 1 21.16 0M8.53 16.11a6 6 0 0 1 6.95 0M12 20h.01";

function systemWarnings(si) {
  return (si.warnings || []).map((w) => {
    const [k, p] = w.split(":");
    return h("div", { class: "banner bad", role: "alert" }, s3(k === "disk" ? "diskWarn" : "ramWarn", { p: fmtNum(+p) }));
  });
}

function hubHealthCard(si) {
  const pct = (a, b) => (b ? (100 * a) / b : 0);
  return h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, s3("hubTitle")), h("span", { class: "badge spacer", dir: "ltr" }, `${si.version}${si.commit ? ` · ${si.commit}` : ""}`)),
    h("div", { class: "meters" },
      meter(`${s3("cpu")} (${fmtNum(si.cores)})`, si.cpu_percent, ltr(`${fmtNum(si.cpu_percent)}% · load ${fmtNum(si.load1)}`)),
      meter(s3("ram"), pct(si.ram_used, si.ram_total), ltr(`${bytes(si.ram_used)} / ${bytes(si.ram_total)}`)),
      meter(s3("disk"), pct(si.disk_used, si.disk_total), ltr(`${bytes(si.disk_used)} / ${bytes(si.disk_total)}`))),
    h("div", { class: "row small muted" },
      h("span", {}, `${s3("db")}: ${bytes(si.db_size)}`), h("span", {}, `${s3("backups", { n: fmtNum(si.backups) })}: ${bytes(si.backups_size)}`),
      h("span", { class: "spacer" }, `${s3("up")} ${dur(si.uptime_sec)}`)));
}

async function viewOnline() {
  const box = h("div", { class: "list" });
  const sum = h("span", { class: "badge spacer" });
  const q = h("input", { type: "search", placeholder: s3("onSearch") });
  let rows = [];
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { timeStyle: "medium" }).format(new Date(u * 1000));
  const draw = () => {
    const f = q.value.trim().toLowerCase();
    const list = rows.filter((o) => !f || o.email.toLowerCase().includes(f) || o.ips.some((ip) => ip.includes(f)));
    const people = new Set(rows.map((o) => o.email)), nodes = new Set(rows.map((o) => o.node_id));
    sum.textContent = s3("onTotal", { n: fmtNum(people.size), m: fmtNum(nodes.size) });
    fill(box, list.length ? h("div", { class: "table-wrap card" }, h("table", { class: "dtable" },
      h("thead", {}, h("tr", {}, h("th", {}, t("clients")), h("th", {}, s3("onNode")), h("th", {}, s3("onIPs")), h("th", {}, s3("onToday")), h("th", {}, s3("onSeen")))),
      h("tbody", {}, list.map((o) => h("tr", {},
        h("td", {}, h("span", { class: "dot on" }), " ", h("a", { href: `#/client?e=${encodeURIComponent(o.email)}` }, o.email)),
        h("td", { "data-label": s3("onNode") }, h("span", { class: "badge" }, o.node_id)),
        h("td", { "data-label": s3("onIPs"), class: "small" }, h("span", { dir: "ltr" }, o.ips.length ? o.ips.join("  ") : "—")),
        h("td", { "data-label": s3("onToday") }, bytes(o.today)), h("td", { "data-label": s3("onSeen"), class: "small muted" }, fmtTime(o.seen))))))) : h("p", { class: "muted" }, rows.length ? t("empty") : s3("onlineNone")));
  };
  q.addEventListener("input", draw);
  const seq = routeSeq;
  const load = async () => {
    if (seq !== routeSeq) return;
    try { rows = await api("GET", "/api/v2/admin/online"); draw(); } catch {}
    setTimeout(load, 8000);
  };
  await load();
  return h("div", { class: "form" }, h("div", { class: "row" }, h("h1", {}, s3("online")), sum), h("p", { class: "small muted" }, s3("onlineHint")), q, box);
}

const NODE_COLORS = ["#2de2e6", "#9b5cff", "#3ef0a1", "#ffc857", "#ff4d8d", "#5aa9ff", "#ff8f4d", "#c0f04d"];
function nodeTrafficCard(nodes) {
  const body = h("div", {});
  let days = 14;
  const load = async () => {
    const rows = await api("GET", `/api/v2/admin/nodes/traffic?days=${days}`).catch(() => []);
    const ids = [...new Set([...nodes.map((n) => n.ID), ...rows.map((r) => r.node_id)])];
    const color = (id) => NODE_COLORS[ids.indexOf(id) % NODE_COLORS.length];
    const dayList = [];
    for (let i = days - 1; i >= 0; i--) dayList.push(new Date(Date.now() - i * 86400000).toISOString().slice(0, 10));
    const by = {}; for (const r of rows) (by[r.day] ||= {})[r.node_id] = r.up + r.down;
    const max = Math.max(1, ...dayList.map((d) => Object.values(by[d] || {}).reduce((a, b) => a + b, 0)));
    const W = 1100, H = 180, bw = W / dayList.length;
    const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H + 20}`, class: "bars", role: "img", "aria-label": s3("ntTitle") });
    for (let k = 0; k <= 4; k++) svg.append(svgEl("line", { x1: 0, x2: W, y1: 4 + ((H - 4) * k) / 4, y2: 4 + ((H - 4) * k) / 4, class: "grid-line" }));
    const step = Math.max(1, Math.round(dayList.length / 7));
    dayList.forEach((d, i) => {
      let y = H;
      const parts = [];
      for (const id of ids) {
        const v = (by[d] || {})[id] || 0;
        if (!v) continue;
        const hgt = ((H - 6) * v) / max;
        y -= hgt;
        svg.append(svgEl("rect", { x: i * bw + bw * 0.18, y, width: bw * 0.64, height: Math.max(1, hgt), rx: 3, fill: color(id), opacity: ".85" }));
        parts.push(`${id}: ${bytes(v)}`);
      }
      const hit = svgEl("rect", { x: i * bw, y: 0, width: bw, height: H, class: "bar-hit" });
      const tt = svgEl("title", {}); tt.textContent = `${dayLabel(d, true)}\n${parts.join("\n") || "—"}`; hit.append(tt); svg.append(hit);
      if ((dayList.length - 1 - i) % step === 0) { const lab = svgEl("text", { x: i * bw + bw / 2, y: H + 15, "text-anchor": "middle", class: "bar-label" }); lab.textContent = dayLabel(d); svg.append(lab); }
    });
    const total = (id) => rows.filter((r) => r.node_id === id).reduce((a, r) => a + r.up + r.down, 0);
    fill(body, svg, h("div", { class: "row small" }, ids.map((id) => h("span", { class: "legend-item" }, h("i", { class: "sw", style: `background:${color(id)}` }), `${id} · ${bytes(total(id))}`))));
  };
  const seg = h("div", { class: "row tight spacer" }, [7, 14, 30].map((n) => h("button", { type: "button", class: n === days ? "chip on" : "chip", onclick: (e) => {
    days = n; seg.querySelectorAll("button").forEach((b) => b.classList.toggle("on", b === e.target)); load();
  } }, s3("ntDays", { n: fmtNum(n) }))));
  load();
  return h("div", { class: "card form" }, h("div", { class: "row" }, h("h2", {}, s3("ntTitle")), seg), h("p", { class: "small muted" }, s3("ntHint")), body);
}

function updateAllButton(nodes) {
  const old = nodes.filter((n) => n.update_available && n.online);
  if (!old.length) return null;
  return h("button", { onclick: async () => {
    if (!(await confirmAction(s3("updAllAsk", { n: fmtNum(old.length) }), s3("updAll", { n: fmtNum(old.length) })))) return;
    let ok = 0;
    for (const n of old) { try { await api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/update`); ok++; } catch {} }
    toast(s3("updStarted", { n: fmtNum(ok) })); setTimeout(route, 1500);
  } }, s3("updAll", { n: fmtNum(old.length) }));
}

async function viewDashboard() {
  const [o, nodes, ins] = await Promise.all([api("GET", "/api/v2/admin/overview"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/insights?days=14")]);
  const si = await api("GET", "/api/v2/admin/system").catch(() => null);
  const stat = (k, v) => h("div", { class: "card stat" }, h("div", { class: "v" }, v), h("div", { class: "k" }, t(k)));
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const topMax = Math.max(1, ...ins.top.map((u) => u.bytes));
  return h("div", {},
    h("h1", {}, t("dashboard")),
    si ? systemWarnings(si) : null,
    h("div", { class: "grid" },
      stat("nodesOnline", `${fmtNum(o.NodesOnline)} / ${fmtNum(o.Nodes)}`),
      stat("activeClients", `${fmtNum(o.ClientsActive)} / ${fmtNum(o.Clients)}`),
      stat("onlineUsers", fmtNum(o.OnlineUsers)),
      stat("trafficToday", bytes(o.TrafficToday)),
      stat("trafficTotal", bytes(o.TrafficTotal))),
    h("div", { class: "dash" },
      h("div", { class: "card form wide-col" }, h("div", { class: "row" }, h("h2", {}, t("traffic14")), legend()), barsSvg(ins.daily, 1100)),
      h("div", { class: "card form" }, h("h2", {}, t("topToday")),
        ins.top.length ? h("div", { class: "meters" }, ins.top.map((u) => meter(u.email, (100 * u.bytes) / topMax, bytes(u.bytes), true))) : h("p", { class: "muted small" }, t("noData"))),
      h("div", { class: "card form" }, h("h2", {}, t("recentEvents")),
        ins.events.length ? h("div", { class: "form" }, ins.events.map((e) => h("div", { class: "row small" },
          eventBadge(e.kind), h("span", {}, e.email), h("span", { class: "muted spacer" }, `${e.node_id} · ${fmtTime(e.unix)}`))))
          : h("p", { class: "muted small" }, t("noEvents"))),
      si ? hubHealthCard(si) : null),
    h("div", { class: "list" }, (nodes || []).map((n) => nodeCard(n))));
}

function rate(bps) { return bytes(bps).replace(/\u2069$/, "/s\u2069"); }
function dur(sec) {
  const d = Math.floor(sec / 86400), hh = Math.floor((sec % 86400) / 3600);
  const [ud, uh] = lang === "fa" ? [" روز", " ساعت"] : ["d", "h"];
  return d ? `${fmtNum(d)}${ud} ${fmtNum(hh)}${uh}` : `${fmtNum(hh)}${uh}`;
}
// Chart dates: Persian calendar on the Persian panel ("۰۸/۲۳"), MM-DD otherwise.
function dayLabel(day, full = false) {
  if (lang !== "fa") return full ? day : day.slice(5);
  const d = new Date(day + "T12:00:00Z");
  return new Intl.DateTimeFormat("fa-IR-u-ca-persian", full ? { year: "numeric", month: "2-digit", day: "2-digit" } : { month: "2-digit", day: "2-digit" }).format(d);
}
const ltr = (s) => `\u2066${s}\u2069`;
// Readable event names ("blocked:ip_limit" -> "IP limit") with a colour by type.
function eventBadge(kind) {
  const fa = lang === "fa";
  const names = fa
    ? { ip_limit: "محدودیت IP", volume: "حجم تمام شد", expired: "انقضا", disabled: "غیرفعال", unknown: "کاربر ناشناس", manual: "ریست دستی", "auto:day": "ریست روزانه", "auto:week": "ریست هفتگی", "auto:month": "ریست ماهانه", "next-plan": "برنامهٔ بعدی", "device-refused": "دستگاه اضافه رد شد" }
    : { ip_limit: "IP limit", volume: "Volume used up", expired: "Expired", disabled: "Disabled", unknown: "Unknown user", manual: "Manual reset", "auto:day": "Daily reset", "auto:week": "Weekly reset", "auto:month": "Monthly reset", "next-plan": "Next plan", "device-refused": "Extra device refused" };
  const key = kind.replace(/^(blocked|reset):/, "");
  const cls = kind.startsWith("blocked:") || kind === "device-refused" ? "bad" : kind === "next-plan" ? "good" : "";
  return h("span", { class: `badge ${cls}` }, names[key] || key);
}
// replaceChildren that skips empty slots (null/false), like h() does.
function fill(el, ...kids) { el.replaceChildren(...kids.flat().filter((k) => k != null && k !== false)); return el; }

// Sparkline as inline SVG (built with the DOM, never markup strings).
function spark(values, max) {
  const NS = "http://www.w3.org/2000/svg", W = 300, H = 48;
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`); svg.setAttribute("class", "spark"); svg.setAttribute("preserveAspectRatio", "none");
  if (values.length < 2) return svg;
  const top = max || Math.max(1, ...values);
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * W},${H - 2 - (Math.min(v, top) / top) * (H - 4)}`).join(" ");
  const area = document.createElementNS(NS, "polygon");
  area.setAttribute("points", `0,${H} ${pts} ${W},${H}`); area.setAttribute("class", "spark-area");
  const line = document.createElementNS(NS, "polyline");
  line.setAttribute("points", pts); line.setAttribute("class", "spark-line");
  svg.append(area, line);
  return svg;
}

async function nodeHealth(n) {
  const pts = await api("GET", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/history?hours=24`);
  const block = (label, vals, max, fmt) => h("div", { class: "form" },
    h("div", { class: "row small" }, h("strong", {}, label), h("span", { class: "muted spacer" }, vals.length ? fmt(vals.at(-1)) : "—")), spark(vals, max));
  openModal(`${t("health")} · ${n.Name || n.ID}`, pts.length ? h("div", { class: "form" },
    block(t("cpu"), pts.map((p) => p.cpu), 100, (v) => `${fmtNum(v)}%`),
    block(t("ram"), pts.map((p) => p.ram), 100, (v) => `${fmtNum(v)}%`),
    block(`${t("net")} · ${t("inRate")}`, pts.map((p) => p.rx), 0, rate),
    block(`${t("net")} · ${t("outRate")}`, pts.map((p) => p.tx), 0, rate),
    block(t("onlineUsers"), pts.map((p) => p.online), 0, (v) => fmtNum(v))) : h("p", { class: "muted" }, t("noData")));
}

function nodeCard(n, actions = false) {
  const host = n.Host;
  return h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: `dot ${n.online ? "on" : ""}` }), h("span", { class: "name" }, `${n.Name || n.ID}`),
      h("span", { class: "badge" }, n.ID), n.update_available ? h("span", { class: "badge warn" }, t("updateAvail")) : null,
      n.traffic_percent && n.traffic_percent !== 100 ? h("span", { class: "badge", dir: "ltr", title: lang === "fa" ? "ضریب ترافیک" : "Traffic rate" }, `×${fmtNum(n.traffic_percent / 100)}`) : null, n.xray_version ? h("span", { class: "badge", dir: "ltr" }, `Xray ${n.xray_version}${n.arch ? " · " + n.arch : ""}`) : null, h("span", { class: `badge ${n.online ? "good" : "bad"} spacer` }, t(n.online ? "online" : "offline"))),
    h("div", { class: "muted small" }, `${t("restarts")}: ${fmtNum(n.XrayRestarts)} · ${t("spooled")}: ${fmtNum(n.Spooled)} · ${n.AgentVersion || ""}`),
    host ? h("div", { class: "meters" },
      meter(t("cpu"), host.cpu_percent, `${fmtNum(host.cpu_percent)}%`),
      meter(t("ram"), host.ram_total ? (100 * host.ram_used) / host.ram_total : 0, `\u2066${bytes(host.ram_used)} / ${bytes(host.ram_total)}\u2069`),
      host.disk_total ? meter(t("disk"), (100 * host.disk_used) / host.disk_total, `\u2066${bytes(host.disk_used)} / ${bytes(host.disk_total)}\u2069`) : null,
      h("div", { class: "small muted" }, `↓ ${rate(host.rx_rate)} · ↑ ${rate(host.tx_rate)} · ${t("onlineUsers")}: ${fmtNum(host.online_users)} · ${t("uptime")}: ${dur(host.uptime_sec)}`)) : null,
    n.LastError ? h("div", { class: "small", style: "color:var(--bad)" }, `${t("lastError")}: ${n.LastError}`) : null,
    actions ? h("div", { class: "row" },
      n.update_available ? h("button", { class: "primary", onclick: () => updateNode(n) }, t("updateNode")) : null,
      h("button", { onclick: () => nodeHealth(n) }, t("health")),
      h("button", { onclick: () => nodeLogs(n) }, t("xrayLogs")),
      h("button", { class: "ghost", onclick: () => restartXray(n) }, t("restartXray")),
      h("button", { class: "ghost", onclick: () => xrayPicker(n) }, t("xrayVer")),
      h("button", { class: "ghost", onclick: () => renameNode(n) }, t("edit")),
      h("button", { class: "ghost", onclick: () => {
        const f = sniFinder({ nodes: [n], node: n.ID, onPick: (d) => copy(d) });
        const d = openModal(`${t("findSni")} · ${n.Name || n.ID}`, f);
        d.addEventListener("close", () => f.cancel(), { once: true });
      } }, t("findSni")),
      h("button", { class: "ghost", onclick: async () => {
        try { const r = await guarded(() => api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/install`));
          openModal(t("reinstall"), h("div", { class: "form" }, h("p", { class: "small" }, t("installCmd")), h("code", {}, r.install), h("button", { type: "button", onclick: () => copy(r.install) }, t("copy"))));
        } catch {}
      } }, t("reinstall")),
      h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmNodeDelete", { x: n.ID }))) { await guarded(() => api("DELETE", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}`)); route(); }
      } }, t("delete"))) : null);
}

function nodeLogs(n) {
  const name = n.Name || n.ID;
  const pre = h("pre", { class: "logview", dir: "ltr", tabindex: "0" }, t("logsLoading", { x: name }));
  const errs = h("input", { type: "checkbox" });
  const grep = h("input", { type: "search", placeholder: t("logsGrep"), dir: "auto" });
  const btn = h("button", { class: "primary", type: "button" }, t("logsRefresh"));
  let busy = false;
  const load = async () => {
    if (busy) return;
    busy = true; btn.disabled = true; pre.classList.add("loading");
    let task;
    try { task = await api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/logs`, { lines: 200, errors: errs.checked, grep: grep.value.trim() }); }
    catch (e) { pre.textContent = e.message; busy = false; btn.disabled = false; pre.classList.remove("loading"); return; }
    await pollTask(task.id, (r) => {
      busy = false; btn.disabled = false; pre.classList.remove("loading");
      if (r.status !== "done") { pre.textContent = /unknown task/i.test(r.error || "") ? t("logsOld") : (r.error || r.status); return; }
      const lines = (r.result && r.result.lines) || [];
      pre.replaceChildren(...(lines.length ? lines.map((l) => h("span", { class: /\[error\]|failed/i.test(l) ? "bad" : /\[warning\]/i.test(l) ? "warn" : "" }, l + "\n")) : [t("logsEmpty")]));
      pre.scrollTop = pre.scrollHeight;
    });
  };
  btn.addEventListener("click", load);
  errs.addEventListener("change", load);
  grep.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); load(); } });
  openModal(`${t("xrayLogs")} · ${name}`, h("div", { class: "form logmodal" },
    h("div", { class: "row" }, grep, h("label", { class: "check" }, errs, h("span", {}, t("logsErrors"))), btn), pre));
  load();
}

async function restartXray(n) {
  const name = n.Name || n.ID;
  if (!await confirmAction(t("restartAsk", { x: name }), t("restartXray"))) return;
  let task;
  try { task = await guarded(() => api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/restart`)); } catch { return; }
  toast(t("restarting", { x: name }));
  pollTask(task.id, (r) => toast(r.status === "done" ? t("restarted", { x: name }) : t("failed", { e: /unknown task/i.test(r.error || "") ? t("logsOld") : (r.error || r.status) })));
}

async function updateNode(n) {
  const name = n.Name || n.ID;
  const d = openModal(t("updateAsk", { x: name }), h("p", { class: "muted" }, ""), [h("button", { value: "ok", class: "primary" }, t("updateNode"))]);
  const ok = await new Promise((res) => d.addEventListener("close", () => res(d.returnValue === "ok"), { once: true }));
  if (!ok) return;
  let task;
  try { task = await guarded(() => api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/update`)); } catch { return; }
  toast(t("updating", { x: name }));
  for (let i = 0; i < 90; i++) {
    await new Promise((r) => setTimeout(r, 2000));
    let r;
    try { r = await api("GET", `/api/v2/admin/tasks/${task.id}`); } catch { continue; }
    if (r.status === "queued") continue;
    toast(r.status === "done" ? t("updated", { x: name }) : t("updateFail", { x: r.error || r.status }));
    route();
    return;
  }
}

async function pollTask(id, done) {
  for (let i = 0; i < 120; i++) {
    await new Promise((r) => setTimeout(r, 2000));
    let r;
    try { r = await api("GET", `/api/v2/admin/tasks/${id}`); } catch { continue; }
    if (r.status !== "queued") return done(r);
  }
  done({ status: "expired" });
}

async function xrayPicker(n) {
  const name = n.Name || n.ID;
  const list = h("div", { class: "list" }, h("p", { class: "muted small" }, "…"));
  const d = openModal(t("xrayPick", { x: name }), h("div", { class: "form" }, h("p", { class: "small muted" }, t("xrayHint")), list));
  let rel;
  try { rel = await api("GET", "/api/v2/admin/xray/releases"); } catch (e) { list.replaceChildren(h("p", { class: "error" }, e.message)); return; }
  list.replaceChildren(...rel.map((r) => {
    const cur = n.xray_version && r.tag === "v" + n.xray_version;
    return h("div", { class: "row card item" },
      h("span", { class: "name", dir: "ltr" }, r.tag), h("span", { class: "small muted" }, r.published),
      r.prerelease ? h("span", { class: "badge warn" }, t("xrayPre")) : null, cur ? h("span", { class: "badge good" }, t("xrayCur")) : null,
      h("button", { class: cur ? "ghost spacer" : "primary spacer", disabled: cur, onclick: async () => {
        d.close();
        if (!(await new Promise((res) => { const c = openModal(t("xrayAsk", { v: r.tag, x: name }), h("p", {}, ""), [h("button", { value: "ok", class: "primary" }, t("save"))]); c.addEventListener("close", () => res(c.returnValue === "ok"), { once: true }); }))) return;
        let task;
        try { task = await guarded(() => api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/xray`, { version: r.tag })); } catch { return; }
        toast(t("xrayInstalling", { v: r.tag, x: name }));
        pollTask(task.id, (res) => { toast(res.status === "done" ? t("xrayDone", { x: name, v: res.result?.to || r.tag }) : t("updateFail", { x: res.error || res.status })); route(); });
      } }, r.tag));
  }));
}

function meter(label, pct, text, neutral = false) {
  const p = Math.max(0, Math.min(100, pct || 0));
  return h("div", { class: "meter" }, h("div", { class: "row small" }, h("span", {}, label), h("span", { class: "muted spacer" }, text)),
    h("div", { class: "bar" }, h("i", { class: neutral ? "" : p > 90 ? "bad" : p > 75 ? "warn" : "", style: `inline-size:${p}%` })));
}

function renameNode(n) {
  const fa = lang === "fa";
  const name = h("input", { value: n.Name || "", autofocus: true });
  const pct = h("input", { type: "number", min: "10", max: "1000", step: "5", value: String(n.traffic_percent || 100), dir: "ltr" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!pct.reportValidity()) return;
    try { await guarded(() => api("PUT", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}`, { Name: name.value, traffic_percent: +pct.value })); document.getElementById("modal").close("save"); route(); } catch {}
  } }, t("save"));
  openModal(`${t("edit")} · ${n.ID}`, h("div", { class: "form" }, h("label", {}, h("span", {}, t("name")), name),
    h("label", {}, h("span", {}, fa ? "ضریب ترافیک کاربرها (درصد)" : "Client traffic rate (percent)"), pct,
      h("small", { class: "muted" }, fa ? "مصرف کاربرها روی این نود با این درصد حساب می‌شود: ۱۵۰ یعنی هر ۱ گیگ ۱٫۵ گیگ حساب شود (نود ویژه) و ۵۰ یعنی نصف. آمار سرور واقعی می‌ماند." : "Client usage on this node is billed at this rate: 150 counts 1 GB as 1.5 GB (a premium node), 50 as half. Server totals stay real."))), [save]);
}

function clientStatus(c) {
  const now = Date.now() / 1000;
  if (!c.Enabled) return ["bad", t("disabled")];
  if (c.Remaining === 0) return ["bad", t("exhausted")];
  if (c.EffectiveUntil && c.EffectiveUntil <= now) return ["bad", t("expired")];
  if (!c.EffectiveUntil && c.DurationSeconds) return ["", t("notStarted")];
  if (!c.EffectiveUntil) return ["", t("noExpiry")];
  const d = Math.ceil((c.EffectiveUntil - now) / 86400);
  return [d <= 3 ? "warn" : "good", t("daysLeft", { n: fmtNum(d) })];
}

const CT = {
  fa: { all: "همه", online: "آنلاین", active: "فعال", expiring: "رو به انقضا", bad: "تمام‌شده", disabled: "غیرفعال", user: "کاربر", usage: "مصرف", remain: "باقی‌مانده",
    expiry: "انقضا", seen: "آخرین اتصال", actions: "", perPage: "در هر صفحه", page: "صفحهٔ {p} از {n}", prev: "قبلی", next: "بعدی", none: "کلاینتی با این فیلتر نیست.",
    reset: "صفر کردن مصرف", resetAsk: "مصرف {x} صفر شود؟ مقدار فعلی در سابقه می‌ماند.", resetDone: "مصرف {n} کلاینت صفر شد", enable: "فعال کردن", disable: "غیرفعال کردن",
    del: "حذف", delAsk: "{n} کلاینت حذف شوند؟", done: "{n} مورد انجام شد", never: "هرگز", now: "الان", ago: "{t} پیش", sub: "اشتراک", edit: "ویرایش", chart: "نمودار",
    sel: "{n} انتخاب‌شده", clear: "لغو انتخاب", allFilters: "همهٔ گروه‌ها و اینباندها", sortBy: "مرتب‌سازی" },
  en: { all: "All", online: "Online", active: "Active", expiring: "Expiring", bad: "Ended", disabled: "Disabled", user: "Client", usage: "Usage", remain: "Remaining",
    expiry: "Expiry", seen: "Last seen", actions: "", perPage: "per page", page: "Page {p} of {n}", prev: "Prev", next: "Next", none: "No clients match.",
    reset: "Reset usage", resetAsk: "Reset {x}'s usage? The current total stays in the history.", resetDone: "Usage reset for {n}", enable: "Enable", disable: "Disable",
    del: "Delete", delAsk: "Delete {n} clients?", done: "{n} done", never: "never", now: "now", ago: "{t} ago", sub: "Subscription", edit: "Edit", chart: "Chart",
    sel: "{n} selected", clear: "Clear", allFilters: "All groups and inbounds", sortBy: "Sort" },
};

function relTime(u, L) {
  if (!u) return L.never;
  const s = Date.now() / 1000 - u;
  if (s < 120) return L.now;
  const fa = lang === "fa";
  const [n, unit] = s < 3600 ? [Math.round(s / 60), fa ? "دقیقه" : "min"] : s < 86400 ? [Math.round(s / 3600), fa ? "ساعت" : "h"] : [Math.round(s / 86400), fa ? "روز" : "d"];
  return L.ago.replace("{t}", `${fmtNum(n)} ${unit}`);
}

function clientState(c) {
  const now = Date.now() / 1000;
  if (!c.Enabled) return "disabled";
  if (c.Remaining === 0 || (c.EffectiveUntil && c.EffectiveUntil <= now)) return "bad";
  if (c.EffectiveUntil && c.EffectiveUntil - now <= 3 * 86400) return "expiring";
  return "active";
}

const putToggle = (c, on) => api("PUT", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`, {
  Enabled: on, MaxIPs: c.MaxIPs, VolumeBytes: c.VolumeBytes, ExpiresUnix: c.ExpiresUnix, DurationSeconds: c.DurationSeconds, Inbounds: c.Inbounds || [], Plan: c.Plan || "" });

async function viewClients() {
  const L = CT[lang] || CT.en;
  const owner = me.role === "owner";
  const [list0, inbounds, groups, reps] = await Promise.all([
    api("GET", "/api/v2/admin/clients"),
    owner ? api("GET", "/api/v2/admin/inbounds") : api("GET", "/api/v2/rep/inbounds").catch(() => []),
    owner ? api("GET", "/api/v2/admin/groups") : Promise.resolve([]),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([]),
  ]);
  const list = list0 || [];
  const ctx = { inbounds, groups: groups || [], reps: reps || [] };
  const saved = (() => { try { return JSON.parse(localStorage.getItem("clients-view") || "{}"); } catch { return {}; } })();
  const st = { q: "", state: saved.state || "", extra: "", sort: saved.sort || "created", dir: saved.dir || 1, page: 0, size: saved.size || 25 };
  const remember = () => { try { localStorage.setItem("clients-view", JSON.stringify({ state: st.state, sort: st.sort, dir: st.dir, size: st.size })); } catch {} };
  const selected = new Set();
  const used = (c) => c.Usage.Up + c.Usage.Down;
  const counts = { "": list.length, online: 0, active: 0, expiring: 0, bad: 0, disabled: 0 };
  for (const c of list) { counts[clientState(c)]++; if (c.Online) counts.online++; }
  const keyOf = {
    created: (c, i) => i, email: (c) => c.Email.toLowerCase(), usage: (c) => used(c),
    remain: (c) => (c.Remaining < 0 ? Infinity : c.Remaining), expiry: (c) => c.EffectiveUntil || Infinity, seen: (c) => -(c.Usage.LastSeen || 0),
  };
  const match = (c) => {
    if (st.q && !(c.Email + " " + (c.Note || "")).toLowerCase().includes(st.q.toLowerCase())) return false;
    if (st.state === "online" && !c.Online) return false;
    if (st.state && st.state !== "online" && clientState(c) !== st.state) return false;
    const f = st.extra;
    if (f.startsWith("g:") && !(ctx.groups.find((g) => g.ID === f.slice(2))?.Members || []).includes(c.Email)) return false;
    if (f.startsWith("i:") && !(c.Inbounds || []).includes(f.slice(2))) return false;
    if (f.startsWith("r:") && (c.RepID || "") !== f.slice(2)) return false;
    return true;
  };

  const chips = h("div", { class: "chips", role: "tablist" });
  const q = h("input", { class: "search", type: "search", placeholder: t("search"), "aria-label": t("search") });
  const extra = h("select", { "aria-label": L.allFilters }, h("option", { value: "" }, L.allFilters),
    ctx.groups.map((g) => h("option", { value: `g:${g.ID}` }, `${t("group")}: ${g.Name || g.ID}`)),
    (inbounds || []).map((ib) => h("option", { value: `i:${ib.tag}` }, `${t("inbounds")}: ${ib.tag}`)),
    ctx.reps.map((r) => h("option", { value: `r:${r.ID}` }, `${t("reps")}: ${r.Name || r.ID}`)));
  const table = h("table", { class: "dtable" });
  const pager = h("div", { class: "row pager" });
  const bulkBar = h("div", { class: "row bulkbar", hidden: true });

  const sortHead = (key, label) => {
    const th = h("th", { scope: "col", class: key ? "sortable" : "", "aria-sort": st.sort === key ? (st.dir > 0 ? "ascending" : "descending") : "none" }, label,
      st.sort === key ? h("span", { class: "sortmark" }, st.dir > 0 ? "▲" : "▼") : null);
    if (key) th.addEventListener("click", () => { st.dir = st.sort === key ? -st.dir : 1; st.sort = key; remember(); draw(); });
    return th;
  };

  function rowActions(c) {
    const menu = h("details", { class: "rowmenu" }, h("summary", { "aria-label": t("edit") }, "⋯"),
      h("div", { class: "menu" },
        h("button", { onclick: () => showSub(c) }, L.sub),
        h("button", { onclick: () => clientForm(c, inbounds, ctx) }, L.edit),
        h("button", { onclick: () => usageChart(c) }, L.chart),
        owner ? h("button", { onclick: async () => { if (await confirmText(L.resetAsk.replace("{x}", c.Email))) { await guarded(() => api("POST", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/reset`)); toast(L.resetDone.replace("{n}", "1")); route(); } } }, L.reset) : null,
        h("button", { onclick: async () => { await guarded(() => putToggle(c, !c.Enabled)); route(); } }, c.Enabled ? L.disable : L.enable),
        me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "danger", onclick: async () => {
          if (!(await confirmAction(t("confirmDelete", { x: c.Email })))) return;
          await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`)); route();
        } }, L.del)));
    menu.addEventListener("toggle", () => { if (menu.open) document.querySelectorAll("details.rowmenu[open]").forEach((d) => { if (d !== menu) d.open = false; }); });
    return menu;
  }

  function row(c) {
    const u = used(c);
    const pct = c.VolumeBytes > 0 ? Math.min(100, (u / c.VolumeBytes) * 100) : 0;
    const [cls, status] = clientStatus(c);
    const cb = h("input", { type: "checkbox", "aria-label": c.Email, checked: selected.has(c.Email) });
    cb.addEventListener("change", () => { cb.checked ? selected.add(c.Email) : selected.delete(c.Email); syncBulk(); });
    return h("tr", { class: c.Enabled ? "" : "muted-row" },
      h("td", { class: "c-sel" }, cb),
      h("td", { class: "c-user", "data-label": L.user },
        h("div", { class: "row tight" }, h("span", { class: `dot ${c.Online ? "on" : ""}`, title: c.Online ? t("online") : t("offline") }),
          h("a", { class: "name", href: `#/client?e=${encodeURIComponent(c.Email)}` }, c.Email)),
        c.Note ? h("div", { class: "small muted clip" }, c.Note) : null),
      h("td", { class: "c-usage", "data-label": L.usage },
        h("div", { class: "small mono" }, `${bytes(u)} / ${c.VolumeBytes < 0 ? "∞" : bytes(c.VolumeBytes)}`),
        c.VolumeBytes > 0 ? h("div", { class: "bar slim" }, h("i", { class: pct > 90 ? "bad" : pct > 75 ? "warn" : "", style: `inline-size:${pct}%` })) : null),
      h("td", { class: "c-remain mono small", "data-label": L.remain }, c.Remaining < 0 ? "∞" : bytes(c.Remaining)),
      h("td", { class: "c-exp", "data-label": L.expiry }, h("span", { class: `badge ${cls}` }, status),
        c.ResetPeriod ? h("span", { class: "badge", title: "reset" }, "↻ " + c.ResetPeriod) : null),
      h("td", { class: "c-seen small muted", "data-label": L.seen }, c.Online ? h("span", { class: "good-text" }, L.now) : relTime(c.Usage.LastSeen, L)),
      h("td", { class: "c-act" }, rowActions(c)));
  }

  function syncBulk() {
    bulkBar.hidden = selected.size === 0;
    if (bulkBar.hidden) return;
    const emails = [...selected];
    const pick = list.filter((c) => selected.has(c.Email));
    const run = async (fn) => { let n = 0; for (const c of pick) { try { await fn(c); n++; } catch {} } toast(L.done.replace("{n}", fmtNum(n))); selected.clear(); route(); };
    fill(bulkBar,
      h("b", {}, L.sel.replace("{n}", fmtNum(selected.size))),
      owner ? h("button", { onclick: () => bulkForm(emails) }, t("bulkRenew")) : null,
      owner ? h("button", { onclick: () => bulkSettingsForm(emails, inbounds, ctx.reps) }, lang === "fa" ? "تغییر تنظیمات" : "Change settings") : null,
      owner ? h("button", { onclick: async () => { if (await confirmText(L.resetAsk.replace("{x}", `${fmtNum(emails.length)}`))) run((c) => api("POST", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/reset`)); } }, L.reset) : null,
      h("button", { class: "ghost", onclick: () => run((c) => putToggle(c, true)) }, L.enable),
      h("button", { class: "ghost", onclick: () => run((c) => putToggle(c, false)) }, L.disable),
      me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "ghost danger", onclick: async () => {
        if (await confirmAction(L.delAsk.replace("{n}", fmtNum(emails.length)))) run((c) => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`));
      } }, L.del),
      h("button", { class: "ghost spacer", onclick: () => { selected.clear(); draw(); } }, L.clear));
  }

  function draw() {
    chips.replaceChildren(...["", "online", "active", "expiring", "bad", "disabled"].map((k) => {
      const b = h("button", { role: "tab", "aria-selected": st.state === k ? "true" : "false", class: `chip ${st.state === k ? "on" : ""} ${k}` },
        k ? L[k] : L.all, h("span", { class: "count" }, fmtNum(counts[k])));
      b.addEventListener("click", () => { st.state = k; st.page = 0; remember(); draw(); });
      return b;
    }));
    const idx = new Map(list.map((c, i) => [c.Email, i]));
    const rows = list.filter(match).sort((a, b) => {
      const ka = keyOf[st.sort](a, idx.get(a.Email)), kb = keyOf[st.sort](b, idx.get(b.Email));
      return (ka < kb ? -1 : ka > kb ? 1 : 0) * st.dir;
    });
    const pages = Math.max(1, Math.ceil(rows.length / st.size));
    st.page = Math.min(st.page, pages - 1);
    const view = rows.slice(st.page * st.size, (st.page + 1) * st.size);
    const all = h("input", { type: "checkbox", "aria-label": L.all, checked: view.length > 0 && view.every((c) => selected.has(c.Email)) });
    all.addEventListener("change", () => { for (const c of view) all.checked ? selected.add(c.Email) : selected.delete(c.Email); draw(); });
    table.replaceChildren(
      h("thead", {}, h("tr", {}, h("th", { class: "c-sel" }, all), sortHead("email", L.user), sortHead("usage", L.usage), sortHead("remain", L.remain),
        sortHead("expiry", L.expiry), sortHead("seen", L.seen), h("th", {}, ""))),
      h("tbody", {}, view.length ? view.map(row) : h("tr", {}, h("td", { colspan: 7, class: "muted empty" }, L.none))));
    const size = h("select", { "aria-label": L.perPage }, [25, 50, 100, 250].map((n) => h("option", { value: n, selected: n === st.size }, `${n} ${L.perPage}`)));
    size.addEventListener("change", () => { st.size = +size.value; st.page = 0; remember(); draw(); });
    pager.replaceChildren(
      h("span", { class: "small muted" }, `${fmtNum(rows.length)} / ${fmtNum(list.length)}`), h("span", { class: "spacer" }), size,
      h("button", { class: "ghost", disabled: st.page === 0, onclick: () => { st.page--; draw(); } }, L.prev),
      h("span", { class: "small" }, L.page.replace("{p}", fmtNum(st.page + 1)).replace("{n}", fmtNum(pages))),
      h("button", { class: "ghost", disabled: st.page >= pages - 1, onclick: () => { st.page++; draw(); } }, L.next));
    syncBulk();
  }
  let timer = 0;
  q.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => { st.q = q.value.trim(); st.page = 0; draw(); }, 150); });
  extra.addEventListener("change", () => { st.extra = extra.value; st.page = 0; draw(); });
  draw();

  const nowS = Date.now() / 1000;
  const attention = list.filter((c) => c.Enabled && ((c.EffectiveUntil && c.EffectiveUntil > nowS && c.EffectiveUntil - nowS <= 3 * 86400) ||
    (c.VolumeBytes > 0 && used(c) / c.VolumeBytes >= 0.9))).slice(0, 8);
  const todo = attention.length ? h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, t("todayTasks")), h("span", { class: "badge warn" }, fmtNum(attention.length))),
    h("p", { class: "small muted" }, t("todayHint")),
    h("div", { class: "row" }, attention.map((c) => {
      const [cls, s] = clientStatus(c);
      const pct = c.VolumeBytes > 0 ? Math.round((used(c) / c.VolumeBytes) * 100) : 0;
      return h("a", { class: "btn", href: `#/client?e=${encodeURIComponent(c.Email)}` }, c.Email, " ", h("span", { class: `badge ${cls}` }, pct >= 90 ? `${fmtNum(pct)}%` : s));
    }))) : null;
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("clients")), h("span", { class: "muted small" }, t("clientsCount", { n: fmtNum(list.length) })),
      owner ? h("button", { class: "ghost spacer", onclick: () => templatesModal(inbounds) }, lang === "fa" ? "قالب‌ها" : "Templates") : h("span", { class: "spacer" }),
      owner ? h("button", { onclick: () => bulkCreateForm(inbounds) }, t("bulkCreate")) : null,
      me.role === "rep" && me.policy && me.policy.DenyCreate ? null : h("button", { class: "primary", onclick: () => clientForm(null, inbounds) }, t("add"))),
    todo, chips,
    h("div", { class: "row toolbar" }, q, extra),
    bulkBar,
    h("div", { class: "card tablecard" }, table), pager);
}

async function bulkCreateForm(inbounds) {
  let groups = [];
  try { groups = await api("GET", "/api/v2/admin/groups") || []; } catch {}
  const num = (v, min = 0) => h("input", { type: "number", min: String(min), value: String(v), dir: "ltr" });
  const prefix = h("input", { value: "user-", dir: "ltr", required: true }), suffix = h("input", { value: "", dir: "ltr" });
  const start = num(1), count = num(10, 1), pad = num(3), vol = num(30), days = num(30), hold = num(0), ips = num(2);
  const firstUse = h("input", { type: "checkbox", checked: true });
  const note = h("input", {});
  const group = h("select", {}, h("option", { value: "" }, t("bcNone")), groups.map((g) => h("option", { value: g.ID }, g.Name || g.ID)));
  const ins = h("div", { class: "chips" }, (inbounds || []).map((i) => h("label", { class: "check" }, h("input", { type: "checkbox", value: i.tag, checked: true }), h("span", {}, i.tag))));
  const prev = h("div", { class: "small muted", dir: "auto" });
  const name = (n) => prefix.value + String(n).padStart(+pad.value || 0, "0") + suffix.value;
  const upd = () => { const a = +start.value || 0; fill(prev, t("bcPreview", { a: `\u2066${name(a)}\u2069`, b: `\u2066${name(a + Math.max(1, +count.value || 1) - 1)}\u2069` })); };
  for (const x of [prefix, suffix, start, count, pad]) x.addEventListener("input", upd);
  upd();
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = ""; save.disabled = true;
    const body = { prefix: prefix.value.trim(), suffix: suffix.value.trim(), start: +start.value || 0, count: +count.value || 0, pad: +pad.value || 0,
      volume_bytes: (+vol.value || 0) > 0 ? Math.round(+vol.value * 2 ** 30) : -1, days: +days.value || 0, first_use: firstUse.checked, on_hold_days: +hold.value || 0,
      max_ips: +ips.value || 0, inbounds: [...ins.querySelectorAll("input:checked")].map((x) => x.value), group: group.value, note: note.value.trim() };
    try {
      const r = await api("POST", "/api/v2/admin/clients/bulk-create", body);
      const list = await api("GET", "/api/v2/admin/clients");
      const made = new Set(r.emails);
      const rows = [["name", "subscription"], ...list.filter((c) => made.has(c.Email)).map((c) => [c.Email, c.SubURL || ""])];
      const csv = rows.map((x) => x.map((v) => `"${String(v).replaceAll('"', '""')}"`).join(",")).join("\n");
      const url = URL.createObjectURL(new Blob(["\ufeff" + csv], { type: "text/csv" }));
      document.getElementById("modal").close();
      openModal(t("bcDone", { n: fmtNum(r.emails.length) }), h("div", { class: "form" },
        h("a", { class: "btn primary", href: url, download: `${body.prefix || "clients"}${body.start}.csv` }, t("bcCSV"))));
      route();
    } catch (x) { err.textContent = x.message; save.disabled = false; }
  } }, t("bulkCreate"));
  openModal(t("bulkCreate"), h("div", { class: "form" }, h("p", { class: "small muted" }, t("bcHint")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("bcPrefix")), prefix), h("label", {}, h("span", {}, t("bcSuffix")), suffix)),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("bcStart")), start), h("label", {}, h("span", {}, t("bcCount")), count)),
    h("label", {}, h("span", {}, t("bcPad")), pad), prev,
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("bcVol")), vol), h("label", {}, h("span", {}, t("bcDays")), days)),
    h("label", { class: "check" }, firstUse, h("span", {}, t("bcFirstUse"))), h("label", {}, h("span", {}, t("bcHold")), hold),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("bcIPs")), ips), h("label", {}, h("span", {}, t("bcGroup")), group)),
    ins, h("label", {}, h("span", {}, t("bcNote")), note), err), [save]);
}

async function templatesModal(inbounds) {
  const fa = lang === "fa";
  const L = fa ? { title: "قالب‌های کاربر", hint: "هنگام ساخت کلاینت جدید، «از روی قالب» همهٔ فیلدها را پر می‌کند.", id: "شناسه", name: "نام", gb: "حجم (گیگ، ۰ = نامحدود)", days: "روز",
      first: "زمان از اولین اتصال", ips: "حداکثر IP", dev: "حداکثر دستگاه", reset: "ریست دوره‌ای", prefix: "پیشوند نام", add: "افزودن قالب", del: "حذف", off: "خاموش", day: "روزانه", week: "هفتگی", month: "ماهانه", none: "هنوز قالبی نیست." }
    : { title: "Client templates", hint: "When adding a client, “From template” fills every field.", id: "ID", name: "Name", gb: "Volume (GB, 0 = unlimited)", days: "Days",
      first: "Time from first connection", ips: "Max IPs", dev: "Max devices", reset: "Periodic reset", prefix: "Name prefix", add: "Add template", del: "Delete", off: "Off", day: "Daily", week: "Weekly", month: "Monthly", none: "No templates yet." };
  const list = h("div", { class: "list" });
  const f = { id: h("input", { pattern: "[a-z0-9][a-z0-9._-]{0,62}", placeholder: "plan-30", dir: "ltr" }), name: h("input", { placeholder: "30 GB · 30 days" }),
    gb: h("input", { type: "number", min: "0", step: "0.5", value: "30" }), days: h("input", { type: "number", min: "0", value: "30" }),
    first: h("input", { type: "checkbox", checked: true }), ips: h("input", { type: "number", min: "0", value: "2" }), dev: h("input", { type: "number", min: "0", value: "0" }),
    reset: h("select", {}, [["", L.off], ["day", L.day], ["week", L.week], ["month", L.month]].map(([v, l]) => h("option", { value: v }, l))), prefix: h("input", { dir: "ltr" }) };
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag }), h("span", { dir: "ltr" }, ib.tag)));
  const err = h("div", { class: "error small" });
  async function load() {
    const tl = await api("GET", "/api/v2/admin/templates").catch(() => []);
    list.replaceChildren(...(tl.length ? tl.map((tp) => h("div", { class: "row card item" }, h("span", { class: "name" }, tp.name),
      h("span", { class: "badge" }, `${tp.gb ? ltr(fmtNum(tp.gb) + " GB") : "∞"} · ${fmtNum(tp.days)} ${lang === "fa" ? "روز" : "d"}`), tp.reset_period ? h("span", { class: "badge" }, "↻ " + tp.reset_period) : null,
      h("button", { class: "ghost danger spacer", onclick: async () => { await guarded(() => api("DELETE", `/api/v2/admin/templates/${encodeURIComponent(tp.id)}`)); load(); } }, L.del))) : [h("p", { class: "muted small" }, L.none)]));
  }
  const add = h("button", { type: "button", class: "primary", onclick: async () => {
    err.textContent = "";
    if (!f.id.value || !f.id.reportValidity() || !f.name.value) { err.textContent = `${L.id} / ${L.name}`; return; }
    try {
      await guarded(() => api("PUT", `/api/v2/admin/templates/${encodeURIComponent(f.id.value)}`, { name: f.name.value, gb: +f.gb.value || 0, days: +f.days.value || 0, first_use: f.first.checked,
        max_ips: +f.ips.value || 0, hwid_limit: +f.dev.value || 0, reset_period: f.reset.value, prefix: f.prefix.value.trim(),
        inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value) }));
      f.id.value = ""; f.name.value = ""; load();
    } catch (x) { err.textContent = x.message; }
  } }, L.add);
  const lab = (l, el) => h("label", {}, h("span", {}, l), el);
  openModal(L.title, h("div", { class: "form" }, h("p", { class: "small muted" }, L.hint), list,
    h("div", { class: "two" }, lab(L.id, f.id), lab(L.name, f.name)), h("div", { class: "two" }, lab(L.gb, f.gb), lab(L.days, f.days)),
    h("label", { class: "check" }, f.first, h("span", {}, L.first)),
    h("div", { class: "two" }, lab(L.ips, f.ips), lab(L.dev, f.dev)), h("div", { class: "two" }, lab(L.reset, f.reset), lab(L.prefix, f.prefix)),
    boxes.length ? h("div", { class: "row" }, boxes) : null, err, add));
  load();
}

function confirmText(text) {
  return new Promise((resolve) => {
    const d = openModal(text, h("p", { class: "muted" }, ""), [h("button", { value: "ok", class: "primary" }, t("save"))]);
    d.addEventListener("close", () => { resolve(d.returnValue === "ok"); d.returnValue = ""; }, { once: true });
  });
}

function bulkForm(emails) {
  const days = h("input", { type: "number", min: "0", value: "30" });
  const gb = h("input", { type: "number", min: "0", step: "0.5", value: "0" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      const r = await api("POST", "/api/v2/admin/clients/bulk", { Emails: emails, AddDays: +days.value || 0, AddGB: +gb.value || 0 });
      toast(t("changed", { n: fmtNum(r.changed) })); document.getElementById("modal").close("save"); route();
    } catch (x) { toast(t("failed", { e: x.message })); }
  } }, t("save"));
  openModal(`${t("bulkRenew")} · ${t("selected", { n: fmtNum(emails.length) })}`, h("div", { class: "form two" },
    h("label", {}, h("span", {}, t("addDays")), days), h("label", {}, h("span", {}, t("addGB")), gb)), [save]);
}

async function viewGroups() {
  const [groups, clients] = await Promise.all([api("GET", "/api/v2/admin/groups"), api("GET", "/api/v2/admin/clients")]);
  const cards = (groups || []).map((g) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, g.Name || g.ID), h("span", { class: "badge" }, g.ID),
      h("span", { class: "badge spacer" }, t("clientsCount", { n: fmtNum(g.Members.length) }))),
    h("div", { class: "small muted" }, g.Members.join(", ")),
    h("div", { class: "row" },
      h("button", { onclick: () => bulkGroup(g) }, t("bulkRenew")),
      h("button", { class: "ghost", onclick: () => groupForm(g, clients) }, t("edit")),
      h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: g.Name || g.ID }))) { await guarded(() => api("DELETE", `/api/v2/admin/groups/${encodeURIComponent(g.ID)}`)); route(); }
      } }, t("delete")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("groups")), h("button", { class: "primary spacer", onclick: () => groupForm(null, clients) }, t("add"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

function bulkGroup(g) {
  const days = h("input", { type: "number", min: "0", value: "30" });
  const gb = h("input", { type: "number", min: "0", step: "0.5", value: "0" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try { const r = await api("POST", "/api/v2/admin/clients/bulk", { Group: g.ID, AddDays: +days.value || 0, AddGB: +gb.value || 0 });
      toast(t("changed", { n: fmtNum(r.changed) })); document.getElementById("modal").close("save"); route(); } catch (x) { toast(t("failed", { e: x.message })); }
  } }, t("save"));
  openModal(`${t("bulkRenew")} · ${g.Name || g.ID}`, h("div", { class: "form two" },
    h("label", {}, h("span", {}, t("addDays")), days), h("label", {}, h("span", {}, t("addGB")), gb)), [save]);
}

function groupForm(g, clients) {
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: g ? g.ID : "", disabled: !!g, autofocus: !g });
  const name = h("input", { value: g ? g.Name : "" });
  const boxes = (clients || []).map((c) => h("label", { class: "check" },
    h("input", { type: "checkbox", value: c.Email, checked: !!g && g.Members.includes(c.Email) }), c.Email));
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    const members = boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value);
    try { await guarded(() => api("PUT", `/api/v2/admin/groups/${encodeURIComponent(id.value)}`, { Name: name.value, Members: members }));
      document.getElementById("modal").close("save"); route(); } catch {}
  } }, t("save"));
  openModal(g ? `${t("edit")} · ${g.Name || g.ID}` : `${t("add")} · ${t("groups")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("members")), boxes)), [save]);
}

function clientCard(c, ctx, selected, onSelect) {
  const inbounds = ctx.inbounds;
  const used = c.Usage.Up + c.Usage.Down;
  const pct = c.VolumeBytes > 0 ? Math.min(100, (used / c.VolumeBytes) * 100) : 0;
  const [cls, status] = clientStatus(c);
  return h("div", { class: "card item" },
    h("div", { class: "head" },
      selected ? h("input", { type: "checkbox", "aria-label": c.Email, checked: selected.has(c.Email), style: "inline-size:auto",
        onchange: (e) => { e.target.checked ? selected.add(c.Email) : selected.delete(c.Email); onSelect(); } }) : null,
      h("span", { class: `dot ${c.Online ? "on" : ""}`, title: c.Online ? t("online") : t("offline") }),
      h("a", { class: "name", href: `#/client?e=${encodeURIComponent(c.Email)}` }, c.Email), h("span", { class: `badge ${cls}` }, status)),
    h("div", { class: "row small muted" },
      h("span", {}, `${bytes(used)} / ${c.VolumeBytes < 0 ? t("unlimited") : bytes(c.VolumeBytes)}`),
      c.MaxIPs ? h("span", {}, `· IP ≤ ${fmtNum(c.MaxIPs)}`) : null,
      h("span", {}, `· ${(c.Inbounds || []).join(", ")}`)),
    c.Note ? h("div", { class: "small muted" }, c.Note) : null,
    c.VolumeBytes > 0 ? h("div", { class: "bar" }, h("i", { class: pct > 90 ? "bad" : pct > 75 ? "warn" : "", style: `inline-size:${pct}%` })) : null,
    h("div", { class: "row" },
      h("button", { onclick: () => showSub(c) }, t("subscription")),
      h("button", { class: "ghost", onclick: () => clientForm(c, inbounds, ctx) }, t("edit")),
      h("button", { class: "ghost", onclick: () => usageChart(c) }, t("usageChart")),
      me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "ghost danger spacer", onclick: async () => {
        if (!(await confirmAction(t("confirmDelete", { x: c.Email })))) return;
        const r = await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`));
        if (r && (r.refund_bytes || r.refund_units)) toast(t("refundDone", { v: [r.refund_bytes ? bytes(r.refund_bytes) : "", r.refund_units ? `${fmtNum(r.refund_units)} ${t("units")}` : ""].filter(Boolean).join(" + ") }));
        route();
      } }, t("delete"))));
}

// Client detail page: #/client?e=<name>
async function viewClient() {
  const email = new URLSearchParams(location.hash.split("?")[1] || "").get("e") || "";
  const owner = me.role === "owner";
  const [list, inbounds, days, reps] = await Promise.all([
    api("GET", "/api/v2/admin/clients"),
    owner ? api("GET", "/api/v2/admin/inbounds") : api("GET", "/api/v2/rep/inbounds").catch(() => []),
    api("GET", `/api/v2/admin/clients/${encodeURIComponent(email)}/daily?days=30`).catch(() => []),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([])]);
  const [ips, resets] = await Promise.all([
    api("GET", `/api/v2/admin/clients/${encodeURIComponent(email)}/ips`).catch(() => []),
    api("GET", `/api/v2/admin/clients/${encodeURIComponent(email)}/resets`).catch(() => [])]);
  const c = (list || []).find((x) => x.Email === email);
  if (!c) return h("div", {}, h("h1", {}, t("clients")), h("p", { class: "muted" }, t("empty")));
  const ctx = { inbounds, groups: [], reps: reps || [] };
  const [cls, status] = clientStatus(c);
  const used = c.Usage.Up + c.Usage.Down;
  const now = Date.now() / 1000;
  const left = c.EffectiveUntil ? Math.max(0, Math.ceil((c.EffectiveUntil - now) / 86400)) : null;
  const stat = (k, v, sub) => h("div", { class: "card stat" }, h("div", { class: "v" }, v), h("div", { class: "k" }, k), sub ? h("div", { class: "small muted" }, sub) : null);
  const url = c.SubURL || "";
  const fmt = (label, u) => h("div", { class: "row" }, h("span", { class: "badge" }, label), h("code", { style: "flex:1" }, u), h("button", { type: "button", class: "ghost", onclick: () => copy(u) }, t("copy")));
  const sum30 = days.reduce((a, d) => a + d.Up + d.Down, 0);
  return h("div", {},
    h("div", { class: "row" }, h("a", { class: "btn ghost", href: "#/clients" }, "‹"), h("span", { class: `dot ${c.Online ? "on" : ""}` }), h("h1", {}, c.Email),
      h("span", { class: `badge ${cls}` }, status),
      h("span", { class: "spacer" }),
      h("button", { onclick: () => clientForm(c, inbounds, ctx) }, t("edit")),
      me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "ghost danger", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: c.Email }))) { await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`)); location.hash = "#/clients"; }
      } }, t("delete"))),
    h("div", { class: "grid" },
      stat(t("used"), bytes(used), c.VolumeBytes > 0 ? `${t("of")} ${bytes(c.VolumeBytes)} · ${fmtNum(Math.min(100, (used / c.VolumeBytes) * 100))}%` : t("unlimited")),
      stat(t("timeLeft"), left == null ? (c.DurationSeconds ? t("notStarted") : t("noExpiry")) : t("daysLeft", { n: fmtNum(left) })),
      stat(t("maxIPs").split(" (")[0], c.MaxIPs ? fmtNum(c.MaxIPs) : "∞"),
      stat(t("last30Traffic"), bytes(sum30))),
    h("div", { class: "dash" },
      usageCard(c.Email, days),
      h("div", { class: "card form" }, h("h2", {}, t("subscription")),
        h("img", { class: "qr", alt: "QR", src: `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/qr.png` }),
        fmt("Auto", url), fmt("sing-box", `${url}?format=singbox`), fmt("Clash", `${url}?format=clash`), fmt("Xray JSON", `${url}?format=xray`)),
      clientConfigsCard(c),
      owner ? rotateCard(c) : null,
      h("div", { class: "card form" }, h("h2", {}, t("details")),
        h("div", { class: "row" }, h("span", { class: "muted" }, t("assignInbounds")), ...(c.Inbounds || []).map((x) => h("span", { class: "badge" }, x))),
        c.RepID ? h("div", { class: "row" }, h("span", { class: "muted" }, t("seller")), h("span", { class: "badge" }, c.RepID)) : null,
        c.Plan ? h("div", { class: "row" }, h("span", { class: "muted" }, t("plan")), h("span", {}, c.Plan === "unlimited" ? t("planUnlimited") : t("planVolume"))) : null,
        c.Note ? h("div", { class: "row" }, h("span", { class: "muted" }, t("note")), h("span", {}, c.Note)) : null,
        c.Usage.FirstSeen ? h("div", { class: "row small" }, h("span", { class: "muted" }, t("firstSeen")), h("span", {}, new Date(c.Usage.FirstSeen * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB"))) : null,
        c.Usage.LastSeen ? h("div", { class: "row small" }, h("span", { class: "muted" }, t("lastSeen")), h("span", {}, new Date(c.Usage.LastSeen * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB"))) : null),
      clientIPsCard(c, ips || []),
      clientResetCard(c, resets || [], owner),
      owner ? nextPlanCard(c) : null,
      owner ? telegramCard(c) : null,
      devicesCard(c)));
}

const CC = {
  fa: { configs: "کانفیگ‌ها", configsHint: "هر کانفیگ جدا، برای برنامه‌هایی که اشتراک نمی‌گیرند. QR را با برنامه اسکن کنید.", noConfigs: "این کاربر هنوز اینباند فعالی ندارد.",
    rotate: "امنیت لینک و کلید", rotateHint: "اگر لینک یا کانفیگ این کاربر به دست کس دیگری افتاده، عوضش کنید. مصرف، پلن و تنظیمات دست نمی‌خورد.",
    rotLink: "لینک اشتراک تازه", rotLinkAsk: "لینک اشتراک {x} عوض شود؟ لینک قبلی دیگر جواب نمی‌دهد و باید لینک تازه را به مشتری بدهید.",
    rotCreds: "UUID و رمز تازه", rotCredsAsk: "UUID و رمز {x} عوض شود؟ همهٔ کانفیگ‌های فعلی‌اش تا وقتی اشتراک را به‌روز نکند قطع می‌شوند.", rotDone: "عوض شد", change: "عوض کن" },
  en: { configs: "Configs", configsHint: "Each config on its own, for apps that don't take subscriptions. Scan the QR with the app.", noConfigs: "This client has no active inbound yet.",
    rotate: "Link and key security", rotateHint: "If this client's link or configs reached someone else, replace them. Usage, plan and settings stay as they are.",
    rotLink: "New subscription link", rotLinkAsk: "Replace the subscription link of {x}? The old link stops answering; give the customer the new one.",
    rotCreds: "New UUID and password", rotCredsAsk: "Replace the UUID and password of {x}? Every current config stops working until the subscription is refreshed.", rotDone: "Replaced", change: "Replace" },
};
const cc = (k, vars) => { let s = (CC[lang] || CC.en)[k] ?? CC.en[k] ?? k; for (const [a, b] of Object.entries(vars || {})) s = s.replaceAll(`{${a}}`, b); return s; };

function clientConfigsCard(c) {
  const list = h("div", { class: "cfglist" }, h("p", { class: "muted small" }, "…"));
  api("GET", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/links`).then((links) => {
    if (!links?.length) { list.replaceChildren(h("p", { class: "muted small" }, cc("noConfigs"))); return; }
    list.replaceChildren(...links.map((l) => h("div", { class: "cfg" },
      h("span", { class: `badge proto-${l.kind}` }, l.kind === "hysteria2" ? "hy2" : l.kind),
      h("span", { class: "cfgname", dir: "auto" }, l.name || "—"),
      h("span", { class: "spacer" }),
      h("button", { type: "button", class: "ghost", "aria-label": "QR", onclick: () => showQR(l.name || c.Email, l.link) }, "QR"),
      h("button", { type: "button", class: "ghost", onclick: () => copy(l.link) }, t("copy")))));
  }).catch((x) => list.replaceChildren(h("p", { class: "error" }, x.message)));
  return h("div", { class: "card form" }, h("h2", {}, cc("configs")), h("p", { class: "small muted" }, cc("configsHint")), list);
}

function rotateCard(c) {
  const run = async (body, ask) => {
    if (!(await confirmAction(cc(ask, { x: c.Email }), cc("change")))) return;
    try { await api("POST", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/rotate`, body); toast(cc("rotDone")); route(); } catch (x) { toast(t("failed", { e: x.message })); }
  };
  return h("div", { class: "card form" }, h("h2", {}, cc("rotate")), h("p", { class: "small muted" }, cc("rotateHint")),
    h("div", { class: "row" },
      h("button", { type: "button", onclick: () => run({ link: true }, "rotLinkAsk") }, cc("rotLink")),
      h("button", { type: "button", class: "ghost danger", onclick: () => run({ credentials: true }, "rotCredsAsk") }, cc("rotCreds"))));
}

function telegramCard(c) {
  const id = h("input", { inputmode: "numeric", dir: "ltr", placeholder: "123456789" });
  const out = h("div", { class: "small muted" });
  api("GET", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/telegram`).then((r) => { id.value = (r.chats || [])[0] || ""; }).catch(() => {});
  return h("div", { class: "card form" }, h("h2", {}, t("clientTg")), h("p", { class: "small muted" }, t("clientTgHint")),
    h("div", { class: "row" }, id, h("button", { onclick: async () => {
      try { await guarded(() => api("PUT", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/telegram`, { chat: parseInt(id.value, 10) || 0 })); } catch {}
    } }, t("save"))), out);
}

const CX = {
  fa: { range: "بازه", d7: "۷ روز", d30: "۳۰ روز", d90: "۹۰ روز", byNode: "به تفکیک نود", noData: "در این بازه مصرفی نیست.",
    np: "برنامهٔ بعدی", npHint: "وقتی حجم یا زمان فعلی تمام شود، مصرف صفر و این برنامه خودکار شروع می‌شود.", npNone: "برنامهٔ بعدی تنظیم نشده.", gb: "حجم (گیگ، ۰ = نامحدود)", days: "روز (۰ = بدون انقضا)",
    firstUse: "شروع زمان از اولین اتصال", setNp: "ذخیرهٔ برنامهٔ بعدی", clearNp: "حذف برنامهٔ بعدی", npSet: "{g} · {d} روز",
    dev: "دستگاه‌ها", devHint: "برنامه‌هایی مثل Happ، v2rayN و Hiddify هنگام گرفتن اشتراک شناسهٔ دستگاه می‌فرستند. با محدودیت، دستگاه اضافه اشتراک نمی‌گیرد.", devLimit: "حداکثر دستگاه (۰ = بدون محدودیت)",
    noDev: "هنوز دستگاهی ثبت نشده.", rmAll: "حذف همهٔ دستگاه‌ها", rm: "حذف", saved: "ذخیره شد", unl: "نامحدود" },
  en: { range: "Range", d7: "7 days", d30: "30 days", d90: "90 days", byNode: "By node", noData: "No usage in this range.",
    np: "Next plan", npHint: "When the current volume or time runs out, usage resets and this plan starts automatically.", npNone: "No next plan set.", gb: "Volume (GB, 0 = unlimited)", days: "Days (0 = no expiry)",
    firstUse: "Time starts at first connection", setNp: "Save next plan", clearNp: "Remove next plan", npSet: "{g} · {d} days",
    dev: "Devices", devHint: "Apps such as Happ, v2rayN and Hiddify send a device ID when fetching the subscription. With a limit, an extra device gets no subscription.", devLimit: "Max devices (0 = no limit)",
    noDev: "No devices yet.", rmAll: "Remove all devices", rm: "Remove", saved: "Saved", unl: "Unlimited" },
};

function usageCard(email, days0) {
  const L = CX[lang] || CX.en;
  const card = h("div", { class: "card form wide-col" });
  let range = 30;
  async function draw(days) {
    const [nodes] = await Promise.all([api("GET", `/api/v2/admin/clients/${encodeURIComponent(email)}/nodes?days=${range}`).catch(() => [])]);
    const chips = h("div", { class: "chips small-chips" }, [[7, L.d7], [30, L.d30], [90, L.d90]].map(([n, l]) => {
      const b = h("button", { class: `chip ${range === n ? "on" : ""}` }, l);
      b.addEventListener("click", async () => { range = n; const d = await api("GET", `/api/v2/admin/clients/${encodeURIComponent(email)}/daily?days=${n}`).catch(() => []); draw(d); });
      return b;
    }));
    const total = (nodes || []).reduce((a, n) => a + n.up + n.down, 0);
    fill(card,
      h("div", { class: "row" }, h("h2", {}, t("usageChart")), legend(), h("span", { class: "spacer" }), chips),
      barsSvg(days, 1100),
      h("div", { class: "small muted" }, L.byNode),
      total ? h("div", { class: "nodebars" }, nodes.map((n) => {
        const v = n.up + n.down, pct = (v / total) * 100;
        return h("div", { class: "nodebar" }, h("span", { class: "name" }, n.node_id), h("div", { class: "bar slim" }, h("i", { style: `inline-size:${pct}%` })),
          h("span", { class: "mono small" }, `${bytes(v)} · ${fmtNum(Math.round(pct))}%`));
      })) : h("p", { class: "muted small" }, L.noData));
  }
  draw(days0);
  return card;
}

function nextPlanCard(c) {
  const L = CX[lang] || CX.en;
  const p = c.next_plan;
  const gb = h("input", { type: "number", min: "0", step: "0.5", value: p ? p.gb : 30 });
  const days = h("input", { type: "number", min: "0", value: p ? p.days : 30 });
  const first = h("input", { type: "checkbox", checked: p ? p.first_use : true });
  const err = h("div", { class: "error small" });
  const save = async (plan) => {
    err.textContent = "";
    try { await guarded(() => api("PUT", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/next-plan`, { plan })); toast(L.saved); route(); } catch (x) { err.textContent = x.message; }
  };
  return h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, L.np), p ? h("span", { class: "badge good" }, L.npSet.replace("{g}", p.gb ? ltr(`${fmtNum(p.gb)} GB`) : L.unl).replace("{d}", fmtNum(p.days))) : null),
    h("p", { class: "small muted" }, p ? L.npHint : L.npNone + " " + L.npHint),
    h("div", { class: "two" }, h("label", {}, h("span", {}, L.gb), gb), h("label", {}, h("span", {}, L.days), days)),
    h("label", { class: "check" }, first, h("span", {}, L.firstUse)), err,
    h("div", { class: "row" }, h("button", { class: "primary", onclick: () => save({ gb: +gb.value || 0, days: +days.value || 0, first_use: first.checked }) }, L.setNp),
      p ? h("button", { class: "ghost danger spacer", onclick: () => save(null) }, L.clearNp) : null));
}

function devicesCard(c) {
  const L = CX[lang] || CX.en;
  const card = h("div", { class: "card form" });
  const limit = h("input", { type: "number", min: "0", max: "100", value: c.hwid_limit || 0 });
  const saveLimit = h("button", { class: "ghost", onclick: async () => { await guarded(() => api("PUT", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/hwid-limit`, { limit: +limit.value || 0 })); toast(L.saved); } }, t("save"));
  const list = h("div", {});
  const del = async (hwid) => { await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/devices/${encodeURIComponent(hwid)}`)); load(); };
  async function load() {
    const devs = await api("GET", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/devices`).catch(() => []);
    fill(list, devs.length ? h("table", { class: "dtable compact" }, h("tbody", {}, devs.map((d) => h("tr", {},
      h("td", {}, h("div", {}, [d.os, d.model].filter(Boolean).join(" · ") || "?"), h("div", { class: "small muted clip", dir: "ltr" }, d.agent || d.hwid)),
      h("td", { class: "small muted" }, fmtDT(d.last)),
      h("td", { class: "c-act" }, h("button", { class: "ghost danger", onclick: () => del(d.hwid) }, L.rm)))))) : h("p", { class: "muted small" }, L.noDev),
      devs.length > 1 ? h("button", { class: "ghost danger", onclick: () => del("*") }, L.rmAll) : null);
  }
  card.append(h("div", { class: "row" }, h("h2", {}, L.dev), c.hwid_limit ? h("span", { class: "badge spacer" }, `≤ ${fmtNum(c.hwid_limit)}`) : null),
    h("p", { class: "small muted" }, L.devHint),
    h("div", { class: "row" }, h("label", { style: "flex:1" }, h("span", {}, L.devLimit), limit), saveLimit), list);
  load();
  return card;
}

const CD = {
  fa: { ips: "IPهای متصل", ipsHint: "آدرس‌هایی که نودها در ۳۰ روز اخیر برای این کلاینت دیده‌اند.", noIPs: "هنوز IPی دیده نشده.", onlineNow: "الان", reset: "صفر کردن مصرف", resetAsk: "مصرف {x} صفر شود؟ مقدار فعلی در سابقه می‌ماند.",
    period: "ریست دوره‌ای", off: "خاموش", day: "روزانه", week: "هفتگی", month: "ماهانه", next: "ریست بعدی", history: "سابقهٔ ریست", resetDone: "مصرف صفر شد", noHistory: "هنوز ریستی نشده.", saved: "ذخیره شد" },
  en: { ips: "Connected IPs", ipsHint: "Addresses nodes saw for this client in the last 30 days.", noIPs: "No addresses seen yet.", onlineNow: "now", reset: "Reset usage", resetAsk: "Reset {x}'s usage? The current total stays in the history.",
    period: "Periodic reset", off: "Off", day: "Daily", week: "Weekly", month: "Monthly", next: "Next reset", history: "Reset history", resetDone: "Usage reset", noHistory: "No resets yet.", saved: "Saved" },
};
const fmtDT = (u) => new Date(u * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" });

function clientIPsCard(c, ips) {
  const L = CD[lang] || CD.en;
  const online = ips.filter((x) => x.online).length;
  return h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, L.ips), online ? h("span", { class: "badge good" }, `${fmtNum(online)} ${L.onlineNow}`) : null,
      c.MaxIPs ? h("span", { class: "badge spacer" }, `≤ ${fmtNum(c.MaxIPs)}`) : null),
    h("p", { class: "small muted" }, L.ipsHint),
    ips.length ? h("table", { class: "dtable compact" }, h("tbody", {}, ips.slice(0, 30).map((x) => h("tr", {},
      h("td", {}, h("span", { class: `dot ${x.online ? "on" : ""}` }), " ", h("span", { class: "mono", dir: "ltr" }, x.ip)),
      h("td", { class: "small muted" }, x.node_id),
      h("td", { class: "small muted" }, x.online ? L.onlineNow : fmtDT(x.last)))))) : h("p", { class: "muted small" }, L.noIPs));
}

function clientResetCard(c, resets, owner) {
  const L = CD[lang] || CD.en;
  const period = h("select", { disabled: !owner }, [["", L.off], ["day", L.day], ["week", L.week], ["month", L.month]].map(([v, l]) => h("option", { value: v, selected: (c.reset_period || "") === v }, l)));
  period.addEventListener("change", async () => { try { await guarded(() => api("PUT", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/reset-period`, { period: period.value })); toast(L.saved); route(); } catch {} });
  return h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, L.reset),
      owner ? h("button", { class: "spacer", onclick: async () => {
        if (await confirmText(L.resetAsk.replace("{x}", c.Email))) { await guarded(() => api("POST", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/reset`)); toast(L.resetDone); route(); }
      } }, L.reset) : null),
    h("label", {}, h("span", {}, L.period), period),
    owner && c.DurationSeconds > 0 && !c.ExpiresUnix && !(c.Usage && c.Usage.FirstSeen) ? onHoldField(c) : null,
    c.next_reset ? h("div", { class: "row small" }, h("span", { class: "muted" }, L.next), h("span", {}, fmtDT(c.next_reset))) : null,
    h("div", { class: "small muted" }, L.history),
    resets.length ? h("table", { class: "dtable compact" }, h("tbody", {}, resets.slice(0, 10).map((r) => h("tr", {},
      h("td", { class: "small" }, fmtDT(r.unix)), h("td", { class: "mono small" }, bytes(r.up + r.down)),
      h("td", { class: "small muted" }, r.reason.startsWith("auto:") ? `↻ ${L[r.reason.slice(5)] || r.reason}` : r.reason))))) : h("p", { class: "muted small" }, L.noHistory));
}

function onHoldField(c) {
  const days = h("input", { type: "number", min: "0", max: "365", dir: "ltr", value: c.start_by ? String(Math.max(0, Math.ceil((c.start_by - Date.now() / 1000) / 86400))) : "0" });
  days.addEventListener("change", async () => { try { await guarded(() => api("PUT", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/on-hold`, { Days: +days.value || 0 })); route(); } catch {} });
  return h("div", { class: "form" }, h("label", {}, h("span", {}, t("onHold")), days),
    h("div", { class: "small muted" }, c.start_by ? t("onHoldUntil", { d: fmtDT(c.start_by) }) : t("onHoldHint")));
}

function showSub(c) {
  const url = c.SubURL || "";
  openModal(`${t("subscription")} · ${c.Email}`, h("div", { class: "form" },
    h("p", { class: "muted small" }, t("subHint")),
    h("img", { class: "qr", alt: "QR", src: `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/qr.png` }),
    h("code", {}, url),
    h("button", { type: "button", onclick: () => copy(url) }, t("copy"))));
}

async function guarded(fn) {
  try { const r = await fn(); toast(t("saved")); return r; } catch (e) { toast(t("failed", { e: e.message })); throw e; }
}

// One change for many clients: inbounds added or removed, IP limit, reseller.
function bulkSettingsForm(emails, inbounds, reps) {
  const fa = lang === "fa";
  const list = (inbounds || []).filter((ib) => !["socks", "http", "dokodemo-door", "tunnel"].includes(ib.protocol));
  const box = (name) => h("div", { class: "chips" }, list.map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, name }), h("span", {}, ib.tag))));
  const add = box("add"), rem = box("rem");
  const ips = h("input", { type: "number", min: "0", max: "1000", placeholder: fa ? "بدون تغییر" : "unchanged", dir: "ltr" });
  const rep = h("select", {}, h("option", { value: "-" }, fa ? "بدون تغییر" : "unchanged"), h("option", { value: "" }, fa ? "مالک (بدون نماینده)" : "Owner (no reseller)"),
    (reps || []).map((r) => h("option", { value: r.ID }, r.Name || r.ID)));
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    const body = { Emails: emails, add_inbounds: [...add.querySelectorAll("input:checked")].map((x) => x.value), remove_inbounds: [...rem.querySelectorAll("input:checked")].map((x) => x.value) };
    if (ips.value !== "") body.max_ips = +ips.value;
    if (rep.value !== "-") body.rep_id = rep.value;
    try { const r = await api("POST", "/api/v2/admin/clients/bulk", body); toast((fa ? "{n} کاربر تغییر کرد" : "{n} clients changed").replace("{n}", fmtNum(r.changed))); document.getElementById("modal").close("save"); route(); }
    catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(fa ? `تغییر تنظیمات ${fmtNum(emails.length)} کاربر` : `Change ${fmtNum(emails.length)} clients`, h("div", { class: "form" },
    h("div", { class: "small muted" }, fa ? "این اینباندها اضافه شود" : "Add these inbounds"), add,
    h("div", { class: "small muted" }, fa ? "این اینباندها برداشته شود" : "Remove these inbounds"), rem,
    h("div", { class: "two" }, h("label", {}, h("span", {}, fa ? "محدودیت IP (۰ = آزاد)" : "IP limit (0 = any)"), ips), h("label", {}, h("span", {}, fa ? "نماینده" : "Reseller"), rep)),
    h("p", { class: "small muted" }, fa ? "انتقال به نماینده اعتبار جابه‌جا نمی‌کند؛ اگر لازم است اعتبار را دستی تنظیم کنید." : "Moving to a reseller does not move credit; adjust credit by hand if needed."), err), [save]);
}

function clientForm(c, inbounds, ctx = { reps: [] }) {
  inbounds = (inbounds || []).filter((ib) => !["socks", "http", "dokodemo-door", "tunnel"].includes(ib.protocol));
  const isNew = !c;
  const v = c || { Email: "", Enabled: true, VolumeBytes: -1, ExpiresUnix: 0, DurationSeconds: 30 * 86400, MaxIPs: 2, Inbounds: [] };
  const daysLeft = v.ExpiresUnix ? Math.max(0, Math.round((v.ExpiresUnix - Date.now() / 1000) / 86400)) : Math.round((v.DurationSeconds || 0) / 86400);
  const name = h("input", { required: true, pattern: "[A-Za-z0-9][A-Za-z0-9._@+-]{0,127}", value: v.Email, disabled: !isNew, autofocus: isNew });
  const vol = h("input", { type: "number", min: "0", step: "0.5", value: v.VolumeBytes < 0 ? 0 : +(v.VolumeBytes / 2 ** 30).toFixed(2) });
  const days = h("input", { type: "number", min: "0", value: daysLeft });
  const first = h("input", { type: "checkbox", checked: isNew ? true : !v.ExpiresUnix && v.DurationSeconds > 0 });
  const ips = h("input", { type: "number", min: "0", max: "1000", value: v.MaxIPs });
  const enabled = h("input", { type: "checkbox", checked: v.Enabled });
  const note = h("input", { value: v.Note || "", maxlength: "2000" });
  const owner = me.role === "owner";
  const repSel = owner && !isNew && ctx.reps.length ? h("select", {}, h("option", { value: "" }, t("ownerSelf")),
    ctx.reps.map((r) => h("option", { value: r.ID, selected: v.RepID === r.ID }, r.Name || r.ID))) : null;
  const plan = h("select", {}, h("option", { value: "volume", selected: v.Plan !== "unlimited" }, t("planVolume")),
    me.role === "rep" && me.policy && !me.policy.AllowUnlimited ? null : h("option", { value: "unlimited", selected: v.Plan === "unlimited" }, t("planUnlimited")));
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" },
    h("input", { type: "checkbox", value: ib.tag, checked: isNew ? (inbounds.length === 1) : (v.Inbounds || []).includes(ib.tag) }),
    `${ib.tag} · ${ib.node_id} · ${ib.network}/${ib.security}`));
  const err = h("div", { class: "error" });
  let tplExtra = null; // reset period and device limit from a template
  const tplSel = isNew ? h("select", {}, h("option", { value: "" }, "—")) : null;
  if (tplSel) {
    api("GET", "/api/v2/admin/templates").then((list) => {
      for (const tp of list || []) tplSel.append(h("option", { value: tp.id }, tp.name));
      tplSel.addEventListener("change", () => {
        const tp = (list || []).find((x) => x.id === tplSel.value);
        if (!tp) { tplExtra = null; return; }
        vol.value = tp.gb || 0; days.value = tp.days || 0; first.checked = !!tp.first_use; ips.value = tp.max_ips || 0;
        for (const b of boxes) { const i = b.querySelector("input"); i.checked = (tp.inbounds || []).includes(i.value); }
        if (tp.prefix && !name.value) name.value = tp.prefix;
        tplExtra = { reset_period: tp.reset_period || "", hwid_limit: tp.hwid_limit || 0 };
      });
    }).catch(() => {});
  }
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!name.reportValidity()) return;
    const d = +days.value || 0;
    const body = {
      Enabled: enabled.checked, MaxIPs: +ips.value || 0,
      VolumeBytes: +vol.value > 0 ? Math.round(+vol.value * 2 ** 30) : -1,
      ExpiresUnix: d && !first.checked ? Math.floor(Date.now() / 1000) + d * 86400 : 0,
      DurationSeconds: d && first.checked ? d * 86400 : 0,
      Inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value),
      Plan: plan.value,
    };
    if (plan.value === "unlimited") body.VolumeBytes = -1;
    if (!isNew && !first.checked && v.ExpiresUnix && d === daysLeft) body.ExpiresUnix = v.ExpiresUnix;
    try {
      const em = encodeURIComponent(name.value);
      await api("PUT", `/api/v2/admin/clients/${em}`, body);
      if ((note.value || "") !== (v.Note || "")) await api("PUT", `/api/v2/admin/clients/${em}/note`, { Note: note.value });
      if (repSel && repSel.value !== (v.RepID || "")) await api("POST", `/api/v2/admin/clients/${em}/transfer`, { RepID: repSel.value });
      if (tplExtra?.reset_period && owner) await api("PUT", `/api/v2/admin/clients/${em}/reset-period`, { period: tplExtra.reset_period });
      if (tplExtra?.hwid_limit) await api("PUT", `/api/v2/admin/clients/${em}/hwid-limit`, { limit: tplExtra.hwid_limit });
      toast(t("saved")); document.getElementById("modal").close("save"); route();
    }
    catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(isNew ? t("add") : `${t("edit")} · ${v.Email}`, h("div", { class: "form" },
    tplSel ? h("label", {}, h("span", {}, lang === "fa" ? "از روی قالب" : "From template"), tplSel) : null,
    h("label", {}, h("span", {}, t("name")), name),
    me.role === "rep" ? h("label", {}, h("span", {}, t("plan")), plan, h("span", { class: "small" }, t("unitsHint"))) : null,
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("volumeGB")), vol), h("label", {}, h("span", {}, t("days")), days)),
    h("label", { class: "check" }, first, t("firstUse")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("maxIPs")), ips), h("label", { class: "check" }, enabled, t("enabled"))),
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("assignInbounds")), boxes),
    h("label", {}, h("span", {}, t("contact")), note),
    repSel ? h("label", {}, h("span", {}, t("transfer")), repSel) : null,
    err), [save]);
}

async function viewInbounds() {
  const [list, nodes, presets, certs] = await Promise.all([api("GET", "/api/v2/admin/inbounds"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/presets"), api("GET", "/api/v2/admin/certs")]);
  const acme = await api("GET", "/api/v2/admin/acme").catch(() => ({}));
  acme.settings ||= {}; acme.certs ||= [];
  const cards = (list || []).map((ib) => {
    let meta = {};
    try { meta = typeof ib.meta === "string" ? JSON.parse(ib.meta) : ib.meta || {}; } catch {}
    return h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: `dot ${ib.enabled ? "on" : ""}` }), h("span", { class: "name" }, ib.tag), h("span", { class: "badge" }, `${ib.protocol} · ${ib.network} · ${ib.security}`),
        ib.enabled ? null : h("span", { class: "badge bad" }, t("disabled")),
        h("span", { class: "spacer" })),
      h("div", { class: "row" }, (ib.nodes || []).map((n) => h("span", { class: "badge" }, n))),
      ["socks", "http", "dokodemo-door", "tunnel"].includes(ib.protocol)
        ? h("div", { class: "muted small" }, `${t("trafficToday")}: ${bytes(ib.traffic_today || 0)} · ${t("trafficTotal")}: ${bytes(ib.traffic_total || 0)}`)
        : h("div", { class: "muted small" }, `${t("clientsCount", { n: fmtNum(ib.clients) })} · ${t("trafficToday")}: ${bytes(ib.traffic_today || 0)} · ${t("trafficTotal")}: ${bytes(ib.traffic_total || 0)}`),
      plainLine(ib, meta, nodes),
      h("div", { class: "row" },
        h("button", { class: "primary", onclick: () => inboundStudio(ib.tag, { nodes, presets, certs }) }, t("nodesRoutes")),
        h("button", { onclick: async () => { await guarded(() => api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}/enabled`, { Enabled: !ib.enabled })); route(); } }, t(ib.enabled ? "disable" : "enable")),
        h("button", { onclick: () => inboundEditor(ib.tag, nodes, certs) }, ie("title")),
        h("button", { class: "ghost", onclick: () => cloneInbound(ib) }, ie("clone")),
        h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: ib.tag }))) { await guarded(() => api("DELETE", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}`)); route(); }
      } }, t("delete"))));
  });
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("inbounds")), h("button", { class: "primary spacer", onclick: () => inboundStudio(null, { nodes, presets, certs }) }, t("newInbound"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))),
    h("div", { class: "row section" }, h("h2", {}, t("certs")), h("button", { class: "spacer", onclick: () => certForm() }, t("addCert"))),
    h("div", { class: "list" }, certs.length ? certs.map((c) => h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: "name" }, c.id), h("span", { class: "badge" }, c.domains.join(", ")),
        h("span", { class: `badge spacer ${c.days_left < 7 ? "bad" : c.days_left < 21 ? "warn" : "good"}` }, t("daysLeft", { n: fmtNum(c.days_left) }))),
      c.used_by.length ? h("div", { class: "small muted" }, `${t("usedBy")}: ${c.used_by.join(", ")}`) : null,
      h("div", { class: "row" }, h("button", { class: "ghost", onclick: () => certForm(c.id) }, t("edit")),
        h("button", { class: "ghost danger spacer", onclick: async () => {
          if (await confirmAction(t("confirmDelete", { x: c.id }))) { await guarded(() => api("DELETE", `/api/v2/admin/certs/${encodeURIComponent(c.id)}`)); route(); }
        } }, t("delete"))))) : h("p", { class: "muted small" }, t("empty"))),
    h("div", { class: "row section" }, h("h2", {}, t("acmeTitle")),
      h("button", { class: "ghost spacer", onclick: () => acmeSettingsForm(acme.settings) }, t("acmeSettings")),
      h("button", { class: "primary", onclick: () => acmeForm(nodes, acme.settings) }, t("freeCert"))),
    h("div", { class: "list" }, acme.certs.length ? acme.certs.map((c) => h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: "name" }, c.id), h("span", { class: "badge", dir: "ltr" }, c.domains.join(", ")),
        h("span", { class: `badge ${c.status === "ok" ? "good" : c.status === "failed" ? "bad" : "warn"}` }, t(`st_${c.status}`)),
        c.not_after ? h("span", { class: `badge spacer ${c.days_left < 7 ? "bad" : c.days_left < 21 ? "warn" : "good"}` }, t("daysLeft", { n: fmtNum(c.days_left) })) : null),
      h("div", { class: "small muted" }, `${c.method === "dns_cf" ? t("acmeDNS") : `${t("acmeHTTP")} · ${c.node_id}`}`),
      c.last_error ? h("div", { class: "small", style: "color:var(--bad)" }, c.last_error) : null,
      h("div", { class: "row" },
        h("button", { class: "ghost", disabled: c.status === "issuing", onclick: async () => { await guarded(() => api("POST", `/api/v2/admin/acme/certs/${encodeURIComponent(c.id)}/renew`)); setTimeout(route, 800); } }, t("renew")),
        h("button", { class: "ghost danger spacer", onclick: async () => {
          if (await confirmAction(t("confirmDelete", { x: c.id }))) { await guarded(() => api("DELETE", `/api/v2/admin/acme/certs/${encodeURIComponent(c.id)}`)); route(); }
        } }, t("delete"))))) : h("p", { class: "muted small" }, t("acmeEmpty"))));
  if (acme.certs.some((c) => c.status === "issuing" || c.status === "pending")) {
    const seq = routeSeq;
    setTimeout(() => { if (seq === routeSeq) route(); }, 4000);
  }
}

// SOCKS / HTTP proxy link, or the port-forward target, on the inbound card.
function plainLine(ib, meta, nodes) {
  if (meta.target) return h("div", { class: "row small" }, h("span", { class: "badge" }, lang === "fa" ? "انتقال پورت" : "Port forward"), h("code", { dir: "ltr" }, `:${ib.port} → ${meta.target}`));
  if (!meta.proxy_user) return null;
  const node = (nodes || []).find((n) => n.ID === (ib.nodes || [])[0]);
  const host = node?.PublicIP || "SERVER-IP";
  const link = `${ib.protocol}://${encodeURIComponent(meta.proxy_user)}:${encodeURIComponent(meta.proxy_pass)}@${host.includes(":") ? `[${host}]` : host}:${ib.port}`;
  return h("div", { class: "row small" }, h("code", { dir: "ltr", style: "flex:1;overflow:hidden;text-overflow:ellipsis" }, link),
    h("button", { type: "button", class: "ghost", onclick: () => copy(link) }, t("copy")), h("button", { type: "button", class: "ghost", onclick: () => showQR(ib.tag, link) }, "QR"));
}

// Test one outbound from a node with a throwaway Xray (agent v2.5.0+).
function outboundTester(tag, nodes) {
  const fa = lang === "fa";
  const sel = h("select", { "aria-label": fa ? "نود" : "Node", style: "max-inline-size:220px;flex:none" }, nodes.map((n) => h("option", { value: n.ID }, `${n.Name || n.ID}${n.online ? "" : fa ? " (خاموش)" : " (offline)"}`)));
  const out = h("span", { class: "small muted" }, fa ? "با تنظیمات ذخیره‌شده روی نود تست می‌شود" : "Tests the saved settings on the node");
  const btn = h("button", { type: "button", class: "ghost", onclick: async () => {
    btn.disabled = true; fill(out, h("span", { class: "muted" }, fa ? "در حال تست…" : "Testing…"));
    try {
      const { id } = await api("POST", "/api/v2/admin/routing/test", { node: sel.value, tag });
      for (let i = 0; i < 40; i++) {
        await new Promise((r) => setTimeout(r, 1500));
        const tk = await api("GET", `/api/v2/admin/tasks/${id}`);
        if (tk.status === "queued") continue;
        if (tk.status === "failed" || tk.status === "expired") {
          const old = /unsupported task/.test(tk.error || "");
          fill(out, h("span", { class: "badge bad" }, old ? (fa ? "عامل این نود قدیمی است؛ اول به‌روز کنید" : "This node's agent is too old — update it first") : (tk.error || tk.status)));
          return;
        }
        const r = tk.result || {};
        fill(out, r.ok ? h("span", { class: `badge ${r.ms < 400 ? "good" : r.ms < 1200 ? "warn" : "bad"}` }, `${fmtNum(r.ms)} ms`) : h("span", { class: "badge bad" }, fa ? "کار نمی‌کند" : "Not working"),
          r.ip ? h("span", { class: "small", dir: "ltr" }, ` ${fa ? "IP خروجی" : "exit IP"}: ${r.ip}`) : null,
          r.error ? h("span", { class: "small muted", dir: "ltr" }, ` ${r.error}`) : null);
        return;
      }
      fill(out, h("span", { class: "badge warn" }, fa ? "نود جواب نداد" : "The node did not answer"));
    } catch (x) { fill(out, h("span", { class: "badge bad" }, x.message)); } finally { btn.disabled = false; }
  } }, fa ? "تست اتصال" : "Test");
  return nodes.length ? h("div", { class: "row small" }, sel, btn, out) : null;
}

function acmeSettingsForm(cur = {}) {
  const LE = "https://acme-v02.api.letsencrypt.org/directory", STG = "https://acme-staging-v02.api.letsencrypt.org/directory";
  const email = h("input", { type: "email", value: cur.email || "", dir: "ltr", autofocus: true });
  const ca = h("select", {}, [[LE, "Let's Encrypt"], [STG, t("acmeStaging")]].map(([v, l]) => h("option", { value: v, selected: (cur.directory || LE) === v }, l)));
  const tok = h("input", { type: "password", dir: "ltr", autocomplete: "off", placeholder: cur.cf_set ? t("cfSaved") : "" });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    try {
      await guarded(() => api("PUT", "/api/v2/admin/acme/settings", { email: email.value, directory: ca.value, cf_token: tok.value }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(t("acmeSettings"), h("div", { class: "form" },
    h("label", {}, h("span", {}, t("acmeEmail")), email), h("label", {}, h("span", {}, t("acmeCA")), ca),
    h("label", {}, h("span", {}, t("cfToken")), tok), err), [save]);
}

function acmeForm(nodes, settings = {}) {
  const idf = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", placeholder: "vpn-cert", autofocus: true, dir: "ltr" });
  const doms = h("textarea", { rows: 3, required: true, dir: "ltr", placeholder: "vpn.example.com" });
  const method = h("select", {}, [["http_node", t("acmeHTTP")], ["dns_cf", t("acmeDNS")]].map(([v, l]) => h("option", { value: v, selected: v === (settings.cf_set ? "dns_cf" : "http_node") }, l)));
  const node = h("select", {}, h("option", { value: "" }, t("acmeNodeAuto")), nodes.map((n) => h("option", { value: n.ID }, n.Name || n.ID)));
  const nodeRow = h("label", {}, h("span", {}, t("node")), node);
  const sync = () => { nodeRow.hidden = method.value !== "http_node"; };
  method.addEventListener("change", sync); sync();
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [idf, doms]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/acme/certs/${encodeURIComponent(idf.value)}`, { domains: doms.value.split(/\s+/).filter(Boolean), method: method.value, node_id: method.value === "http_node" ? node.value : "" }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("freeCert"));
  openModal(t("freeCert"), h("div", { class: "form" },
    h("p", { class: "small muted" }, t("acmeHint")),
    h("label", {}, h("span", {}, t("tag")), idf), h("label", {}, h("span", {}, t("acmeDomains")), doms),
    h("label", {}, h("span", {}, t("acmeMethod")), method), nodeRow, err), [save]);
}

// ---------- Routing and outbounds ----------
const RT = {
  fa: { title: "مسیریابی و خروجی‌ها", save: "ذخیره و اعمال روی نودها", saved: "ذخیره شد؛ نودها در همگام‌سازی بعدی می‌گیرند", safe: "پیش‌فرض‌های امن",
    private: "بستن شبکهٔ داخلی و IP خود نود", torrent: "بلاک تورنت (جلوگیری از شکایت کپی‌رایت)", routeOnly: "sniffing فقط برای مسیریابی (پیشنهادی)",
    ads: "بلاک تبلیغات", clientIran: "در کانفیگ برنامه‌ها (sing-box، Clash) سایت‌ها و IPهای ایران مستقیم بروند", iran: "ترافیک ایران روی سرور", iranNone: "کاری نکن", iranBlock: "بلاک (کلاینت خودش مستقیم برود)", iranVia: "از خروجی", ds: "حل نام دامنه در خروجی مستقیم",
    dns: "سرورهای DNS (هر خط یکی، خالی = DNS سیستم نود)", outbounds: "خروجی‌ها", addWarp: "+ WARP رایگان", addCustom: "+ خروجی دلخواه (JSON)", tag: "شناسه",
    allNodes: "همهٔ نودها", nodes: "نودها", balancers: "بالانسرها", addBal: "+ بالانسر", strategy: "روش", rules: "قانون‌ها", addRule: "+ قانون",
    rulesHint: "به ترتیب بررسی می‌شوند؛ اولین قانونی که بخورد اجرا می‌شود. قانون‌های شما بعد از پیش‌فرض‌های امن و قبل از تبلیغات و ایران هستند.",
    tpl: "الگوهای آماده", tplAI: "هوش مصنوعی از WARP", tplMail: "بلاک پورت ایمیل (اسپم)", tplSpeed: "سایت‌های تست سرعت مستقیم",
    domains: "دامنه‌ها (domain:، full:، keyword:، regexp:، geosite:)", ips: "IP / CIDR / geoip:", ports: "پورت (مثل 25,465 یا 1000-2000)", protocols: "پروتکل",
    network: "شبکه", users: "کاربرها (ایمیل)", inbounds: "اینباندها", to: "خروجی", remark: "توضیح", enabled: "فعال", del: "حذف", up: "بالا", down: "پایین",
    geo: "فایل‌های geo", geoUpdate: "به‌روزرسانی الان", geoMissing: "این نودها هنوز فایل geo ندارند؛ قانون‌های geoip/geosite فعلاً روی آن‌ها اعمال نمی‌شود:",
    geoNeed: "برای تبلیغات، ایران و قانون‌های geo لازم است. هفتگی خودکار به‌روز می‌شود.", any: "همه", none: "هیچ", edit: "ویرایش", warpOk: "اکانت WARP ساخته شد", empty: "چیزی نیست",
    unsaved: "تغییرات ذخیره نشده", adv: "کانفیگ پیشرفتهٔ Xray", advHint: "بخش‌های اضافه که روی کانفیگ همهٔ نودها اضافه می‌شوند: log، policy، dns، observatory، fakedns، reverse، metrics. قبل از ذخیره با خود Xray چک می‌شود.", advSave: "ذخیرهٔ کانفیگ پیشرفته", view: "کانفیگ نهایی نود", show: "نمایش" },
  en: { title: "Routing & outbounds", save: "Save and apply to nodes", saved: "Saved; nodes pick it up on their next sync", safe: "Safe defaults",
    private: "Block private networks and the node's own IP", torrent: "Block BitTorrent (avoids copyright complaints)", routeOnly: "Sniffing for routing only (recommended)",
    ads: "Block ads", clientIran: "App profiles (sing-box, Clash) send Iranian sites and IPs direct", iran: "Iranian traffic on the server", iranNone: "Do nothing", iranBlock: "Block (clients go direct themselves)", iranVia: "Via outbound", ds: "Domain resolution on the direct outbound",
    dns: "DNS servers (one per line, empty = node's system DNS)", outbounds: "Outbounds", addWarp: "+ Free WARP", addCustom: "+ Custom outbound (JSON)", tag: "Tag",
    allNodes: "All nodes", nodes: "Nodes", balancers: "Balancers", addBal: "+ Balancer", strategy: "Strategy", rules: "Rules", addRule: "+ Rule",
    rulesHint: "Checked in order; the first match wins. Your rules run after the safe defaults and before ads and Iran.",
    tpl: "Templates", tplAI: "AI sites via WARP", tplMail: "Block mail ports (spam)", tplSpeed: "Speed-test sites direct",
    domains: "Domains (domain:, full:, keyword:, regexp:, geosite:)", ips: "IP / CIDR / geoip:", ports: "Ports (e.g. 25,465 or 1000-2000)", protocols: "Protocol",
    network: "Network", users: "Users (email)", inbounds: "Inbounds", to: "Outbound", remark: "Remark", enabled: "Enabled", del: "Delete", up: "Up", down: "Down",
    geo: "Geo files", geoUpdate: "Update now", geoMissing: "These nodes have no geo files yet; geoip/geosite rules are not applied on them:",
    geoNeed: "Needed for ads, Iran and geo rules. Updated weekly.", any: "any", none: "none", edit: "Edit", warpOk: "WARP account created", empty: "Nothing yet",
    unsaved: "Unsaved changes", adv: "Advanced Xray config", advHint: "Extra sections merged into every node's config: log, policy, dns, observatory, fakedns, reverse, metrics. Checked with Xray before it is saved.", advSave: "Save advanced config", view: "Node's final config", show: "Show" },
};

async function viewRouting() {
  const L = RT[lang] || RT.en;
  const [data, nodes, inbounds, overlay, traffic] = await Promise.all([api("GET", "/api/v2/admin/routing"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/inbounds"), api("GET", "/api/v2/admin/xray/overlay"),
    api("GET", "/api/v2/admin/routing/traffic?days=30").catch(() => [])]);
  const usage = Object.fromEntries((traffic || []).map((x) => [x.tag, x]));
  const usageBadge = (tag) => usage[tag] ? h("span", { class: "badge" }, lang === "fa" ? `امروز ${bytes(usage[tag].today)}، ۳۰ روز ${bytes(usage[tag].total)}` : `today ${bytes(usage[tag].today)} · 30 days ${bytes(usage[tag].total)}`) : null;
  const r = JSON.parse(JSON.stringify(data.routing));
  r.outbounds ||= []; r.balancers ||= []; r.rules ||= []; r.dns ||= [];
  const root = h("div", {});
  const dirty = h("span", { class: "badge warn", hidden: true }, L.unsaved);
  const err = h("div", { class: "error", role: "alert" });
  const touch = () => { dirty.hidden = false; };
  const lines = (v) => v.split(/\n+/).map((x) => x.trim()).filter(Boolean);
  const chk = (obj, key, label) => { const i = h("input", { type: "checkbox", checked: !!obj[key] }); i.addEventListener("change", () => { obj[key] = i.checked; touch(); }); return h("label", { class: "check" }, i, h("span", {}, label)); };
  const txt = (obj, key, attrs = {}) => { const i = h("input", { value: obj[key] || "", ...attrs }); i.addEventListener("input", () => { obj[key] = i.value.trim(); touch(); }); return i; };
  const area = (obj, key, attrs = {}) => { const i = h("textarea", { rows: 2, dir: "ltr", ...attrs }); i.value = (obj[key] || []).join("\n"); i.addEventListener("input", () => { obj[key] = lines(i.value); touch(); }); return i; };
  const sel = (obj, key, opts, onChange) => { const s = h("select", {}, opts.map(([v, l]) => h("option", { value: v, selected: (obj[key] || "") === v }, l))); s.addEventListener("change", () => { obj[key] = s.value; touch(); onChange?.(); }); return s; };
  const field = (label, el) => h("label", {}, h("span", {}, label), el);
  const nodePick = (obj) => {
    const all = h("label", { class: "check" }, h("input", { type: "checkbox", checked: !(obj.nodes || []).length }), h("span", {}, L.allNodes));
    const box = h("div", { class: "row" }, all, nodes.map((n) => {
      const i = h("input", { type: "checkbox", checked: (obj.nodes || []).includes(n.ID) });
      i.addEventListener("change", () => { const set = new Set(obj.nodes || []); i.checked ? set.add(n.ID) : set.delete(n.ID); obj.nodes = [...set]; all.firstChild.checked = !obj.nodes.length; touch(); });
      return h("label", { class: "check" }, i, h("span", {}, n.Name || n.ID));
    }));
    all.firstChild.addEventListener("change", () => { if (all.firstChild.checked) { obj.nodes = []; draw(); touch(); } });
    return box;
  };
  const targets = () => [["direct", "direct"], ["block", "block"], ...r.outbounds.map((o) => [o.tag, o.tag]), ...r.balancers.map((b) => [b.tag, `⚖ ${b.tag}`])];
  const uniq = (base) => { let tag = base, i = 2; const used = new Set(targets().map((x) => x[0])); while (used.has(tag)) tag = `${base}${i++}`; return tag; };

  function outboundCard(o, i) {
    const cfg = h("textarea", { class: "code", rows: 6, dir: "ltr", spellcheck: "false" });
    cfg.value = JSON.stringify(o.config, null, 2);
    const bad = h("div", { class: "error small" });
    cfg.addEventListener("input", () => { try { o.config = JSON.parse(cfg.value); bad.textContent = ""; touch(); } catch (e) { bad.textContent = e.message; } });
    const det = h("details", {}, h("summary", { class: "small muted" }, L.edit + " JSON"), cfg, bad);
    return h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: "name", dir: "ltr" }, o.tag), h("span", { class: "badge" }, o.kind === "warp" ? "WARP" : o.config?.protocol || "?"), usageBadge(o.tag),
        h("button", { class: "ghost danger spacer", onclick: () => { r.outbounds.splice(i, 1); touch(); draw(); } }, L.del)),
      h("div", { class: "two" }, field(L.tag, txt(o, "tag", { dir: "ltr" })), field(L.remark, txt(o, "remark"))),
      field(L.nodes, nodePick(o)), outboundTester(o.tag, nodes.filter((n) => !(o.nodes || []).length || o.nodes.includes(n.ID))), det);
  }

  function balancerCard(b, i) {
    const picks = h("div", { class: "row" }, r.outbounds.map((o) => {
      const c = h("input", { type: "checkbox", checked: b.outbounds.includes(o.tag) });
      c.addEventListener("change", () => { b.outbounds = c.checked ? [...b.outbounds, o.tag] : b.outbounds.filter((x) => x !== o.tag); touch(); });
      return h("label", { class: "check" }, c, h("span", { dir: "ltr" }, o.tag));
    }));
    return h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: "name", dir: "ltr" }, `⚖ ${b.tag}`), h("button", { class: "ghost danger spacer", onclick: () => { r.balancers.splice(i, 1); touch(); draw(); } }, L.del)),
      h("div", { class: "two" }, field(L.tag, txt(b, "tag", { dir: "ltr" })), field(L.strategy, sel(b, "strategy", [["", "random"], ["roundRobin", "roundRobin"], ["leastPing", "leastPing"], ["leastLoad", "leastLoad"]]))),
      field(L.outbounds, picks));
  }

  function ruleCard(ru, i) {
    const protos = h("div", { class: "row" }, ["bittorrent", "http", "tls", "quic"].map((p) => {
      const c = h("input", { type: "checkbox", checked: (ru.protocols || []).includes(p) });
      c.addEventListener("change", () => { const s = new Set(ru.protocols || []); c.checked ? s.add(p) : s.delete(p); ru.protocols = [...s]; touch(); });
      return h("label", { class: "check" }, c, h("span", {}, p));
    }));
    const ibs = h("div", { class: "row" }, inbounds.map((ib) => {
      const c = h("input", { type: "checkbox", checked: (ru.inbounds || []).includes(ib.tag) });
      c.addEventListener("change", () => { const s = new Set(ru.inbounds || []); c.checked ? s.add(ib.tag) : s.delete(ib.tag); ru.inbounds = [...s]; touch(); });
      return h("label", { class: "check" }, c, h("span", { dir: "ltr" }, ib.tag));
    }));
    const move = (d) => { const j = i + d; if (j < 0 || j >= r.rules.length) return; [r.rules[i], r.rules[j]] = [r.rules[j], r.rules[i]]; touch(); draw(); };
    const summary = [ru.domains?.length ? `${ru.domains.length} domain` : "", ru.ips?.length ? `${ru.ips.length} ip` : "", ru.ports ? `port ${ru.ports}` : "", (ru.protocols || []).join(","), ru.users?.length ? `${ru.users.length} user` : ""].filter(Boolean).join(" · ");
    return h("details", { class: `card item rule ${ru.enabled ? "" : "off"}` },
      h("summary", { class: "head" }, h("span", { class: "badge" }, String(i + 1)), h("span", { class: "name" }, ru.remark || summary || "—"),
        h("span", { class: "badge", dir: "ltr" }, `→ ${ru.outbound}`), ru.nodes?.length ? h("span", { class: "badge" }, `${ru.nodes.length} ${L.nodes}`) : null),
      h("div", { class: "form" },
        h("div", { class: "row" }, chk(ru, "enabled", L.enabled), h("button", { type: "button", class: "ghost", onclick: () => move(-1) }, "↑ " + L.up), h("button", { type: "button", class: "ghost", onclick: () => move(1) }, "↓ " + L.down),
          h("button", { type: "button", class: "ghost danger spacer", onclick: () => { r.rules.splice(i, 1); touch(); draw(); } }, L.del)),
        h("div", { class: "two" }, field(L.remark, txt(ru, "remark")), field(L.to, sel(ru, "outbound", targets()))),
        field(L.domains, area(ru, "domains", { placeholder: "domain:example.com\ngeosite:category-ads-all" })),
        h("div", { class: "two" }, field(L.ips, area(ru, "ips", { placeholder: "geoip:ir\n1.2.3.0/24" })), field(L.users, area(ru, "users"))),
        h("div", { class: "two" }, field(L.ports, txt(ru, "ports", { dir: "ltr" })), field(L.network, sel(ru, "network", [["", L.any], ["tcp", "tcp"], ["udp", "udp"], ["tcp,udp", "tcp,udp"]]))),
        field(L.protocols, protos), inbounds.length ? field(L.inbounds, ibs) : null, field(L.nodes, nodePick(ru))));
  }

  const addRule = (x) => { r.rules.push({ enabled: true, outbound: "block", ...x }); touch(); draw(); };
  async function addWarp() {
    try {
      const res = await guarded(() => api("POST", "/api/v2/admin/routing/warp"));
      r.outbounds.push({ tag: uniq("warp"), kind: "warp", remark: "Cloudflare WARP", config: res.config });
      toast(L.warpOk); touch(); draw();
    } catch {}
  }
  function addCustom() {
    r.outbounds.push({ tag: uniq("out"), kind: "custom", config: { protocol: "socks", settings: { servers: [{ address: "203.0.113.1", port: 1080 }] } } });
    touch(); draw();
  }
  async function save() {
    err.textContent = "";
    try {
      await guarded(() => api("PUT", "/api/v2/admin/routing", r));
      dirty.hidden = true; toast(L.saved);
    } catch (x) { err.textContent = x.message; }
  }
  async function geoUpdate(btn) {
    btn.disabled = true;
    try { data.geo = await guarded(() => api("POST", "/api/v2/admin/routing/geo/update")); draw(); } catch {} finally { btn.disabled = false; }
  }

  function draw() {
    const missing = data.status?.nodes_without_geo || [];
    const iranOpts = [["", L.iranNone], ["block", L.iranBlock], ...r.outbounds.map((o) => [o.tag, `${L.iranVia} ${o.tag}`])];
    fill(root,
      h("div", { class: "row sticky-head" }, h("h1", {}, L.title), dirty, h("button", { class: "primary spacer", onclick: save }, L.save)),
      err,
      missing.length ? h("div", { class: "card warnbox small" }, L.geoMissing, " ", h("b", { dir: "ltr" }, missing.join(", "))) : null,
      h("section", { class: "card form" }, h("h2", {}, L.safe),
        chk(r, "block_private", L.private), chk(r, "block_torrent", L.torrent), chk(r, "route_only", L.routeOnly), chk(r, "block_ads", L.ads), chk(r, "client_iran_direct", L.clientIran),
        h("div", { class: "two" }, field(L.iran, sel(r, "iran", iranOpts)),
          field(L.ds, sel(r, "domain_strategy", [["AsIs", "AsIs"], ["UseIPv4", "UseIPv4"], ["UseIPv6", "UseIPv6"], ["UseIP", "UseIP"], ["ForceIPv4", "ForceIPv4"]]))),
        field(L.dns, area(r, "dns", { placeholder: "https://1.1.1.1/dns-query\n8.8.8.8" }))),
      h("div", { class: "row section" }, h("h2", {}, L.outbounds), h("button", { class: "spacer", onclick: addWarp }, L.addWarp), h("button", { class: "ghost", onclick: addCustom }, L.addCustom)),
      h("div", { class: "row small" }, ["direct", "block"].map((tag) => usage[tag] ? h("span", { class: "chip" }, h("b", { dir: "ltr" }, tag), " ", usageBadge(tag)) : null)),
      h("div", { class: "list" }, r.outbounds.length ? r.outbounds.map(outboundCard) : h("p", { class: "muted small" }, L.empty)),
      r.outbounds.length > 1 || r.balancers.length ? h("div", { class: "row section" }, h("h2", {}, L.balancers),
        h("button", { class: "ghost spacer", onclick: () => { r.balancers.push({ tag: uniq("lb"), outbounds: r.outbounds.map((o) => o.tag), strategy: "" }); touch(); draw(); } }, L.addBal)) : null,
      h("div", { class: "list" }, r.balancers.map(balancerCard)),
      h("div", { class: "row section" }, h("h2", {}, L.rules), h("button", { class: "spacer", onclick: () => addRule({ remark: "" }) }, L.addRule)),
      h("p", { class: "small muted" }, L.rulesHint),
      h("div", { class: "row small" }, h("span", { class: "muted" }, L.tpl + ":"),
        h("button", { class: "ghost", onclick: () => addRule({ remark: L.tplMail, ports: "25,465,587", outbound: "block" }) }, L.tplMail),
        h("button", { class: "ghost", onclick: () => addRule({ remark: L.tplAI, domains: ["domain:openai.com", "domain:chatgpt.com", "domain:oaistatic.com", "domain:oaiusercontent.com", "domain:gemini.google.com", "domain:claude.ai", "domain:anthropic.com"], outbound: r.outbounds.find((o) => o.kind === "warp")?.tag || "direct" }) }, L.tplAI),
        h("button", { class: "ghost", onclick: () => addRule({ remark: L.tplSpeed, domains: ["domain:speedtest.net", "domain:fast.com", "domain:ookla.com"], outbound: "direct" }) }, L.tplSpeed)),
      h("div", { class: "list" }, r.rules.length ? r.rules.map(ruleCard) : h("p", { class: "muted small" }, L.empty)),
      h("div", { class: "row section" }, h("h2", {}, L.geo), h("button", { class: "ghost spacer", onclick: (e) => geoUpdate(e.currentTarget) }, L.geoUpdate)),
      h("p", { class: "small muted" }, L.geoNeed),
      h("div", { class: "list" }, (data.geo || []).map((g) => h("div", { class: "card item" },
        h("div", { class: "head" }, h("span", { class: "name", dir: "ltr" }, g.name), h("span", { class: `badge ${g.size ? "good" : "warn"}` }, g.size ? bytes(g.size) : "—"),
          g.updated ? h("span", { class: "badge spacer" }, new Date(g.updated * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB")) : null),
        h("div", { class: "small muted mono", dir: "ltr" }, g.url)))),
      advSection());
  }
  const ovText = h("textarea", { class: "code", rows: 8, dir: "ltr", spellcheck: "false" });
  ovText.value = JSON.stringify(overlay, null, 2);
  function advSection() {
    const ovErr = h("div", { class: "error small" });
    const nodeSel = h("select", {}, nodes.map((n) => h("option", { value: n.ID }, n.Name || n.ID)));
    return h("section", { class: "card form" }, h("h2", {}, L.adv), h("p", { class: "small muted" }, L.advHint), ovText, ovErr,
      h("div", { class: "row" }, h("button", { class: "primary", onclick: async () => {
        ovErr.textContent = "";
        let body;
        try { body = JSON.parse(ovText.value || "{}"); } catch (e) { ovErr.textContent = e.message; return; }
        try { await guarded(() => api("PUT", "/api/v2/admin/xray/overlay", body)); toast(L.saved); } catch (x) { ovErr.textContent = x.message; }
      } }, L.advSave),
        h("span", { class: "spacer" }), h("span", { class: "small muted" }, L.view), nodeSel,
        h("button", { class: "ghost", onclick: async () => {
          try {
            const cfg = await api("GET", `/api/v2/admin/nodes/${encodeURIComponent(nodeSel.value)}/config`);
            const txt = JSON.stringify(cfg, null, 2);
            openModal(`${L.view} · ${nodeSel.value}`, h("div", { class: "form" }, h("pre", { class: "code cfgview", dir: "ltr" }, txt), h("button", { type: "button", onclick: () => copy(txt) }, t("copy"))));
          } catch (e) { toast(e.message); }
        } }, L.show)));
  }
  draw();
  return root;
}

// ---------- REALITY SNI finder: probes candidates from the node itself ----------
function sniFinder({ nodes, node, onPick }) {
  const field = (label, el) => h("label", {}, h("span", {}, label), el);
  const box = h("div", { class: "card snifind" });
  const sel = h("select", {}, (nodes || []).map((n) => h("option", { value: n.ID, selected: n.ID === node }, n.Name || n.ID)));
  const subnet = h("input", { type: "checkbox", checked: true });
  const custom = h("textarea", { rows: 3, placeholder: "www.example.com", dir: "ltr" });
  const status = h("div", { class: "small muted", role: "status" });
  const out = h("div", { class: "sniresults" });
  const go = h("button", { type: "button", class: "primary" }, t("sniScan"));
  let timer = 0;
  const stop = () => { clearInterval(timer); timer = 0; go.disabled = false; };
  go.addEventListener("click", async () => {
    const id = sel.value || node;
    if (!id) return;
    go.disabled = true; out.replaceChildren();
    let task;
    try {
      task = await api("POST", `/api/v2/admin/nodes/${encodeURIComponent(id)}/sniscan`, { subnet: subnet.checked, domains: custom.value.split(/\s+/).filter(Boolean) });
    } catch (e) { status.textContent = e.message; stop(); return; }
    const t0 = Date.now();
    const name = (nodes || []).find((n) => n.ID === id)?.Name || id;
    const tick = async () => {
      status.textContent = t("sniScanning", { x: name, s: Math.round((Date.now() - t0) / 1000) });
      let r;
      try { r = await api("GET", `/api/v2/admin/tasks/${task.id}`); } catch { return; }
      if (r.status === "queued") return;
      stop();
      if (r.status === "expired") { status.textContent = t("sniExpired"); return; }
      if (r.status === "failed") { status.textContent = r.error || "failed"; return; }
      show(r.result || {});
    };
    timer = setInterval(tick, 2000);
    tick();
  });
  function show(res) {
    const all = res.candidates || [];
    const good = all.filter((c) => c.ok && c.tls13 && c.h2);
    status.textContent = `${fmtNum(res.scanned || 0)} · ${(res.took_ms / 1000).toFixed(1)}s` + (res.neighbors ? ` · ${t("neighbor")}: ${fmtNum(res.neighbors)}` : "");
    fill(out,
      good.length ? h("table", {},
        h("thead", {}, h("tr", {}, h("th", {}, "SNI"), h("th", {}, t("score")), h("th", {}, t("latency")), h("th", {}, ""), h("th", {}, ""))),
        h("tbody", {}, good.slice(0, 25).map((c) => h("tr", {},
          h("td", { class: "mono", dir: "ltr" }, c.domain, c.neighbor ? h("span", { class: "badge good" }, t("neighbor")) : null),
          h("td", {}, h("div", { class: "scorebar" }, h("i", { style: `width:${c.score}%` })), h("span", { class: "small" }, String(c.score))),
          h("td", { class: "mono" }, `${c.latency_ms} ms`),
          h("td", { class: "small muted" }, c.issuer || ""),
          h("td", {}, h("button", { type: "button", onclick: () => { onPick(c.domain, c); toast(`SNI → ${c.domain}`); } }, t("sniUse"))))))) : null,
      all.length > good.length ? h("p", { class: "small muted" }, t("sniFailed", { x: fmtNum(all.length - good.length) })) : null,
    );
  }
  box.append(
    h("p", { class: "small muted" }, t("sniHint")),
    nodes && nodes.length > 1 ? field(t("node"), sel) : null,
    h("label", { class: "check" }, subnet, h("span", {}, t("sniNeighbors"))),
    field(t("sniCustom"), custom),
    h("div", { class: "row" }, go, status), out);
  box.cancel = stop;
  return box;
}

// ---------- Inbound studio: one profile, many nodes, Direct/Tunnel/Swap per node ----------
async function inboundStudio(tag, ctx) {
  const isNew = !tag;
  const nodes = ctx.nodes || [];
  const nodeName = (id) => { const n = nodes.find((x) => x.ID === id); return n ? n.Name || n.ID : id; };
  const st = {
    step: 0,
    profile: { tag: "", preset: (ctx.presets || [])[0] || "vless-reality-tcp", sni: "www.speedtest.net", path: "", remark: "Dark VPN", cert: (ctx.certs || [])[0]?.id || "" },
    routes: {}, // nodeID -> {listen_ip, direct_port, tunnel_port, hosts}
    swaps: [], removedSwaps: [],
  };
  const newHost = (path, node) => ({ path, enabled: true, remark: "", address: path === "direct" ? (node?.PublicIP || "") : "", port: 443,
    sni: "", host: "", path_override: "", alpn: "", fingerprint: "", allow_insecure: false, exclude: [], _adv: false });
  const newRoute = (node) => ({ listen_ip: "", direct_port: 443, tunnel_port: 0, hosts: [newHost("direct", node)] });
  if (!isNew) {
    const [routes, swaps] = await Promise.all([api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`), api("GET", `/api/v2/admin/swaps?inbound=${encodeURIComponent(tag)}`)]);
    for (const r of routes) st.routes[r.node_id] = { ...r, hosts: r.hosts.map((x) => ({ ...x, exclude: x.exclude || [], _adv: !!(x.sni || x.host || x.path_override || x.alpn || x.fingerprint || x.allow_insecure || x.exclude?.length) })) };
    st.swaps = swaps.map((w) => ({ ...w }));
    st.step = 1;
  }
  const steps = [["stepProfile"], ["stepNodes"], ["stepSwap"], ["stepReview"]];
  const body = h("div", { class: "form" });
  const err = h("div", { class: "error", role: "alert" });
  const d = openModal(isNew ? t("newInbound") : `${t("editInbound")} · ${tag}`, h("div", {}, h("div", { class: "steps", role: "tablist" }), body, err), [
    h("button", { class: "ghost", type: "button", onclick: (e) => { e.preventDefault(); go(st.step - 1); } }, t("back")),
    h("button", { class: "primary", value: "save", onclick: async (e) => { e.preventDefault(); if (st.step < 3) go(st.step + 1); else await save(); } }, t("next")),
  ]);
  d.classList.add("wide");
  d.addEventListener("close", () => d.classList.remove("wide"), { once: true });
  const stepBar = d.querySelector(".steps");
  const nextBtn = d.querySelector(".actions .primary");
  const backBtn = d.querySelector(".actions .ghost");

  function go(i) {
    if (i < (isNew ? 0 : 1) || i > 3) return;
    if (i > st.step && st.step === 0 && !validProfile()) return;
    if (i > 1 && st.step <= 1 && !Object.keys(st.routes).length) { err.textContent = t("pickNode"); return; }
    err.textContent = ""; st.step = i; draw();
  }
  function validProfile() {
    if (!/^[a-z0-9][a-z0-9._-]{0,62}$/.test(st.profile.tag)) { err.textContent = t("badTag"); return false; }
    if (st.profile.preset.includes("-tls-") && !st.profile.cert) { err.textContent = t("needCert"); return false; }
    return true;
  }
  const field = (label, el) => h("label", {}, h("span", {}, label), el);
  const bind = (obj, key, attrs = {}, num = false) => {
    const el = h("input", { ...attrs, value: obj[key] ?? "" });
    el.addEventListener("input", () => { obj[key] = num ? (+el.value || 0) : el.value; });
    return el;
  };

  function draw() {
    stepBar.replaceChildren(...steps.map(([k], i) => h("button", { type: "button", role: "tab", "aria-current": i === st.step ? "step" : null,
      disabled: isNew ? false : i === 0, onclick: () => go(i) }, `${i + 1}. ${t(k)}`)));
    backBtn.hidden = st.step === (isNew ? 0 : 1);
    nextBtn.textContent = st.step === 3 ? t("save") : t("next");
    body.replaceChildren(...[drawProfile, drawNodes, drawSwaps, drawReview][st.step]());
  }

  function drawProfile() {
    const p = st.profile;
    const preset = h("select", {}, (ctx.presets || []).map((x) => h("option", { value: x, selected: x === p.preset }, x)));
    const certSel = h("select", {}, h("option", { value: "" }, t("selfSigned")), (ctx.certs || []).map((c) => h("option", { value: c.id, selected: c.id === p.cert }, `${c.id} · ${c.domains.join(", ")}`)));
    const certRow = field(t("certFor"), certSel);
    const fbNote = h("p", { class: "small muted", hidden: true }, t("fbHint"));
    const plainNote = h("p", { class: "small muted", hidden: true });
    const sniWrap = h("div", {}), pathLabel = h("span", {}, t("path"));
    const sync = () => {
      fbNote.hidden = p.preset !== "vless-tls-fallback";
      const plain = ["socks", "http", "dokodemo"].includes(p.preset);
      sniWrap.hidden = plain || p.preset === "wireguard";
      pathLabel.textContent = p.preset === "dokodemo" ? (lang === "fa" ? "مقصد (host:port)" : "Target (host:port)") : t("path");
      if (pathInput) pathInput.placeholder = p.preset === "dokodemo" ? "1.2.3.4:443" : p.preset === "wireguard" ? "10.66.0.0/16" : "";
      plainNote.hidden = !plain;
      plainNote.textContent = p.preset === "dokodemo"
        ? (lang === "fa" ? "انتقال پورت (تونل): هر چیزی که به این پورت برسد به مقصد فرستاده می‌شود؛ مثلاً یک ریلی جلوی سرور دیگر. کاربری ندارد و در اشتراک‌ها نمی‌آید؛ ترافیکش در آمار اینباند هست." : "Port forwarding (tunnel): everything arriving on this port goes to the target — e.g. a relay in front of another server. No clients, not in subscriptions; its traffic shows in the inbound's stats.")
        : (lang === "fa" ? "پروکسی ساده با یک نام کاربری و رمز تصادفی (هرگز پروکسی باز نیست). کاربرهای پنل به آن وصل نمی‌شوند و در اشتراک‌ها نمی‌آید؛ لینک و رمز روی کارت اینباند است." : "A plain proxy with one random username and password (never an open proxy). Panel clients don't use it and it is not in subscriptions; the link and password are on the inbound card.");
      certRow.hidden = !(p.preset.includes("-tls-") || p.preset === "hysteria2");
      certSel.options[0].hidden = p.preset !== "hysteria2";
      if (p.preset === "hysteria2" && !(ctx.certs || []).length) p.cert = "";
    };
    const sniInput = bind(p, "sni");
    var pathInput = bind(p, "path");
    const finder = sniFinder({ nodes, node: Object.keys(st.routes)[0] || nodes[0]?.ID, onPick: (d) => { p.sni = d; sniInput.value = d; } });
    finder.hidden = true;
    const findBtn = h("button", { type: "button", class: "ghost" }, t("findSni"));
    findBtn.addEventListener("click", () => { finder.hidden = !finder.hidden; });
    const isReality = () => p.preset.includes("reality");
    sniWrap.append(field(t("sni"), h("div", { class: "row tight" }, sniInput, findBtn)));
    preset.addEventListener("change", () => { p.preset = preset.value; sync(); findBtn.hidden = !isReality(); if (!isReality()) finder.hidden = true; });
    findBtn.hidden = !isReality();
    certSel.addEventListener("change", () => { p.cert = certSel.value; const c = (ctx.certs || []).find((x) => x.id === p.cert); if (c?.domains[0]) { p.sni = c.domains[0]; draw(); } });
    sync();
    return [
      h("p", { class: "small muted" }, t("profileHint")),
      h("div", { class: "two" }, field(t("tag"), bind(p, "tag", { required: true, placeholder: "multi-reality", autofocus: true })), field(t("preset"), preset)),
      certRow, fbNote,
      plainNote,
      h("div", { class: "two" }, h("div", {}, sniWrap), h("label", {}, pathLabel, pathInput)),
      finder,
      field(t("remark"), bind(p, "remark")),
    ];
  }

  function hostRow(r, hst, idx, node) {
    const pathSel = h("select", {}, [["direct", t("direct")], ["tunnel", t("tunnel")]].map(([v, l]) => h("option", { value: v, selected: hst.path === v }, l)));
    pathSel.addEventListener("change", () => { hst.path = pathSel.value; });
    const en = h("input", { type: "checkbox", checked: hst.enabled, "aria-label": t("enabled") });
    en.addEventListener("change", () => { hst.enabled = en.checked; });
    const adv = h("div", { class: "adv", hidden: !hst._adv },
      field("SNI", bind(hst, "sni")), field("Host", bind(hst, "host")), field(t("pathOverride"), bind(hst, "path_override")),
      field("ALPN", bind(hst, "alpn", { placeholder: "h2,http/1.1" })), field("Fingerprint", bind(hst, "fingerprint", { placeholder: "chrome" })),
      h("div", { class: "form" },
        h("label", { class: "check" }, (() => { const c = h("input", { type: "checkbox", checked: hst.allow_insecure }); c.addEventListener("change", () => { hst.allow_insecure = c.checked; }); return c; })(), "allowInsecure"),
        h("div", { class: "row small" }, t("hideIn"), ...["links", "singbox", "clash"].map((f) => h("label", { class: "check" },
          (() => { const c = h("input", { type: "checkbox", checked: hst.exclude.includes(f) }); c.addEventListener("change", () => { hst.exclude = c.checked ? [...hst.exclude, f] : hst.exclude.filter((x) => x !== f); }); return c; })(), f)))));
    return h("div", { class: "hostrow" },
      field(t("route"), pathSel), field(t("address"), bind(hst, "address", { placeholder: "1.2.3.4 / cdn.example.com" })),
      field(t("port"), bind(hst, "port", { type: "number", min: "1", max: "65535" }, true)), field(t("remark"), bind(hst, "remark", { placeholder: node })),
      h("label", { class: "check" }, en, t("enabled")),
      h("div", { class: "row" },
        h("button", { type: "button", class: "ghost", onclick: () => { hst._adv = !hst._adv; adv.hidden = !hst._adv; } }, "⚙"),
        h("button", { type: "button", class: "ghost danger", "aria-label": t("delete"), onclick: () => { r.hosts.splice(idx, 1); draw(); } }, "✕")),
      adv);
  }

  function drawNodes() {
    const pick = h("div", { class: "nodepick" }, nodes.map((n) => {
      const c = h("input", { type: "checkbox", checked: !!st.routes[n.ID] });
      c.addEventListener("change", () => { if (c.checked) st.routes[n.ID] = st.routes[n.ID] || newRoute(n); else delete st.routes[n.ID]; draw(); });
      return h("label", {}, c, h("span", { class: `dot ${n.online ? "on" : ""}` }), `${n.Name || n.ID}`, h("span", { class: "muted small" }, n.ID));
    }));
    const cards = Object.keys(st.routes).sort().map((id) => {
      const r = st.routes[id];
      const node = nodes.find((x) => x.ID === id);
      const tunnelOn = h("input", { type: "checkbox", checked: r.tunnel_port > 0 });
      const tport = bind(r, "tunnel_port", { type: "number", min: "1", max: "65535", disabled: !(r.tunnel_port > 0) }, true);
      tunnelOn.addEventListener("change", () => { r.tunnel_port = tunnelOn.checked ? (r.direct_port === 8443 ? 2083 : 8443) : 0; draw(); });
      return h("div", { class: "route" },
        h("div", { class: "head" }, h("span", { class: `dot ${node?.online ? "on" : ""}` }), h("strong", {}, nodeName(id)), h("span", { class: "badge" }, id),
          node?.PublicIP ? h("span", { class: "badge" }, node.PublicIP) : null,
          !isNew && r.direct_total != null ? h("span", { class: "small muted spacer" }, `${t("direct")} ${bytes(r.direct_total || 0)} · ${t("tunnel")} ${bytes(r.tunnel_total || 0)}`) : null),
        h("div", { class: "three" },
          field(t("listenIP"), bind(r, "listen_ip", { placeholder: "0.0.0.0" })),
          field(`${t("direct")} · ${t("port")}`, bind(r, "direct_port", { type: "number", min: "1", max: "65535", required: true }, true)),
          h("label", { class: "tunnel-label" }, h("span", {}, tunnelOn, `${t("tunnel")} · ${t("port")}`), tport)),
        ...r.hosts.map((x, i) => hostRow(r, x, i, id)),
        h("div", { class: "row" },
          h("button", { type: "button", onclick: () => { r.hosts.push(newHost("direct", node)); draw(); } }, `+ ${t("direct")}`),
          h("button", { type: "button", onclick: () => { r.hosts.push({ ...newHost("tunnel", node), port: r.tunnel_port || r.direct_port }); draw(); } }, `+ ${t("tunnel")}`)));
    });
    return [h("p", { class: "small muted" }, t("nodesHint")), pick, cards.length ? h("p", { class: "small muted" }, t("hostsHint")) : null, ...cards];
  }

  function drawSwaps() {
    const placed = Object.keys(st.routes).sort();
    const rows = st.swaps.map((w, i) => {
      const dest = h("select", {}, placed.map((id) => h("option", { value: id, selected: id === w.dest_node }, nodeName(id))));
      dest.addEventListener("change", () => { w.dest_node = dest.value; });
      const src = h("select", {}, nodes.map((n) => h("option", { value: n.ID, selected: n.ID === w.source_node }, n.Name || n.ID)));
      src.addEventListener("change", () => { w.source_node = src.value; });
      const en = h("input", { type: "checkbox", checked: w.enabled });
      en.addEventListener("change", () => { w.enabled = en.checked; });
      return h("div", { class: "route" },
        h("div", { class: "head" }, h("span", { class: "pathtag swap" }, "Dark Swap"), h("strong", {}, w.name || "DARK SWAP"), w.target ? h("span", { class: "small muted mono" }, `→ ${w.target}`) : null,
          h("button", { type: "button", class: "ghost danger spacer", onclick: () => { if (!w._new) st.removedSwaps.push(w.id); st.swaps.splice(i, 1); draw(); } }, t("delete"))),
        h("div", { class: "three" }, field(t("name"), bind(w, "name")), field(t("destNode"), dest), field(t("relayNode"), src)),
        h("div", { class: "three" }, field(t("relayPort"), bind(w, "source_port", { type: "number", min: "1", max: "65535" }, true)),
          field(t("entryAddr"), bind(w, "entry_address", { placeholder: "ir.example.com" })), field(t("entryPort"), bind(w, "entry_port", { type: "number", min: "1", max: "65535" }, true))),
        h("div", { class: "two" }, field(t("targetAddr"), bind(w, "target_address", { placeholder: t("targetAuto") })), h("label", { class: "check" }, en, t("enabled"))));
    });
    return [h("p", { class: "small muted" }, t("swapHint")), ...rows,
      h("div", { class: "row" }, h("button", { type: "button", disabled: !placed.length || nodes.length < 2, onclick: () => {
        const dest = placed[0];
        const src = nodes.find((n) => n.ID !== dest)?.ID || "";
        st.swaps.push({ _new: true, id: `${st.profile.tag || tag}-swap${st.swaps.length + 1}`, name: "DARK SWAP", dest_node: dest, source_node: src, source_port: 6801, entry_address: "", entry_port: 6801, target_address: "", enabled: true });
        draw();
      } }, `+ ${t("swap")}`))];
  }

  function drawReview() {
    const lines = [];
    for (const id of Object.keys(st.routes).sort()) {
      const r = st.routes[id];
      lines.push(h("div", { class: "line" }, h("strong", {}, nodeName(id)), h("span", { class: "pathtag direct" }, `${t("direct")} :${r.direct_port}`),
        r.tunnel_port ? h("span", { class: "pathtag tunnel" }, `${t("tunnel")} :${r.tunnel_port}`) : null,
        h("span", { class: "small muted" }, r.hosts.filter((x) => x.enabled).map((x) => `${x.address}:${x.port}`).join("  ") || t("noHosts"))));
    }
    for (const w of st.swaps) lines.push(h("div", { class: "line" }, h("span", { class: "pathtag swap" }, "Dark Swap"),
      h("span", { class: "mono small" }, `${w.entry_address || "?"}:${w.entry_port} → ${nodeName(w.source_node)}:${w.source_port} → ${nodeName(w.dest_node)}`)));
    return [isNew ? h("p", {}, h("strong", {}, st.profile.tag), h("span", { class: "muted" }, ` · ${st.profile.preset}`)) : null, h("div", { class: "review" }, lines)];
  }

  async function save() {
    err.textContent = "";
    const routes = Object.entries(st.routes).map(([node_id, r]) => ({ node_id, listen_ip: (r.listen_ip || "").trim(), direct_port: +r.direct_port, tunnel_port: +r.tunnel_port || 0,
      hosts: r.hosts.map(({ _adv, ...x }) => ({ ...x, port: +x.port })) }));
    const theTag = isNew ? st.profile.tag : tag;
    try {
      if (isNew) {
        const p = st.profile;
        await api("POST", "/api/v2/admin/inbounds/compose", { tag: p.tag, preset: p.preset, sni: p.sni.trim(), path: p.path.trim(), remark: p.remark.trim(), cert: p.preset.includes("-tls-") || p.preset === "hysteria2" ? p.cert : "", routes });
      } else {
        await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(theTag)}/routes`, routes);
      }
      for (const id of st.removedSwaps) await api("DELETE", `/api/v2/admin/swaps/${encodeURIComponent(id)}`);
      for (const w of st.swaps) {
        const { _new, target, ...body } = w;
        await api("PUT", `/api/v2/admin/swaps/${encodeURIComponent(w.id)}`, { ...body, inbound_tag: theTag, source_port: +w.source_port, entry_port: +w.entry_port });
      }
      toast(t("saved")); d.close("save"); route();
    } catch (x) { err.textContent = x.message; }
  }
  draw();
}

async function swapDialog(ib, nodes) {
  const list = await api("GET", `/api/v2/admin/swaps?inbound=${encodeURIComponent(ib.tag)}`);
  const others = (nodes || []).filter((n) => n.ID !== ib.node_id);
  const rows = list.map((w) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: `dot ${w.enabled ? "on" : ""}` }), h("span", { class: "name" }, w.name), h("span", { class: "badge" }, w.id)),
    h("div", { class: "small mono" }, `${w.entry_address}:${w.entry_port} → ${w.source_node}:${w.source_port} → ${w.target || "?"}`),
    h("div", { class: "row" },
      h("button", { type: "button", onclick: async () => { await guarded(() => api("PUT", `/api/v2/admin/swaps/${encodeURIComponent(w.id)}`, { ...w, enabled: !w.enabled })); swapDialog(ib, nodes); } }, t(w.enabled ? "disable" : "enable")),
      h("button", { type: "button", class: "ghost danger spacer", onclick: async () => { await guarded(() => api("DELETE", `/api/v2/admin/swaps/${encodeURIComponent(w.id)}`)); swapDialog(ib, nodes); } }, t("delete")))));
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: `${ib.tag}-swap${list.length ? list.length + 1 : ""}` });
  const name = h("input", { value: "DARK SWAP", maxlength: "160" });
  const node = h("select", { required: true }, others.map((n) => h("option", { value: n.ID }, `${n.Name || n.ID} (${n.ID})`)));
  const sport = h("input", { type: "number", min: "1", max: "65535", required: true, value: "6801" });
  const eaddr = h("input", { required: true, placeholder: "ir.example.com" });
  const eport = h("input", { type: "number", min: "1", max: "65535", required: true, value: "6801" });
  const err = h("div", { class: "error" });
  const add = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [id, node, sport, eaddr, eport]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/swaps/${encodeURIComponent(id.value)}`, { name: name.value, inbound_tag: ib.tag, source_node: node.value,
        source_port: +sport.value, entry_address: eaddr.value.trim(), entry_port: +eport.value, enabled: true }));
      swapDialog(ib, nodes);
    } catch (x) { err.textContent = x.message; }
  } }, t("add"));
  openModal(`${t("swap")} · ${ib.tag}`, h("div", { class: "form" },
    h("p", { class: "small muted" }, t("swapHint")),
    rows.length ? h("div", { class: "list" }, rows) : null,
    others.length ? h("div", { class: "form" },
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("relayNode")), node), h("label", {}, h("span", {}, t("relayPort")), sport)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("entryAddr")), eaddr), h("label", {}, h("span", {}, t("entryPort")), eport)), err)
      : h("p", { class: "muted" }, t("empty"))), others.length ? [add] : []);
}

// ---------- Inbound settings editor (form + JSON) ----------
const IE = {
  fa: { title: "تنظیمات", form: "فرم", json: "JSON", general: "عمومی", protocol: "پروتکل", transport: "انتقال (ترنسپورت)", security: "امنیت", sniffing: "Sniffing",
    listen: "IP شنود", port: "پورت", portRouted: "این اینباند روی نود قرار گرفته؛ پورت هر نود را در «نودها و مسیرها» عوض کنید.", remark: "نام در برنامه‌ها", enabled: "فعال",
    flow: "Flow", flowNone: "بدون flow", venc: "رمزنگاری VLESS (Xray 26)", vencOff: "خاموش (none)", vencX: "X25519 (سریع)", vencM: "ML-KEM-768 (پساکوانتومی)",
    vencHint: "لایهٔ رمزنگاری خود VLESS روی هر ترنسپورت. فقط برنامه‌های مبتنی بر Xray 26 (v2rayNG، v2rayN، Happ، Streisand جدید) و Clash Meta جدید پشتیبانی می‌کنند؛ sing-box این اینباند را در اشتراکش نمی‌گیرد.",
    vencNew: "کلید تازه", server: "سمت سرور (decryption)", client: "سمت برنامه (encryption)",
    method: "روش رمزنگاری", ssKey: "کلید سرور", newKey: "ساخت کلید تازه", ssWarn: "با عوض کردن روش یا کلید سرور، همهٔ مشتری‌ها باید اشتراک را به‌روز کنند.",
    network: "شبکه", path: "مسیر (path)", host: "Host", service: "نام سرویس gRPC", multi: "multiMode (چند جریانی، سریع‌تر)", xmode: "حالت XHTTP", padding: "Padding (بایت، مثل 100-1000)",
    proxyProto: "پذیرش PROXY protocol (پشت HAProxy/Nginx یا ریلی)", sec: "امنیت", none: "هیچ", target: "مقصد (target)", sni: "serverNames (هر خط یکی)",
    sids: "shortIds (هر خط یکی؛ اولی در لینک‌ها)", addSid: "+ shortId", priv: "کلید خصوصی", pub: "کلید عمومی (در لینک‌ها)", newKeys: "کلید تازه",
    keysWarn: "با کلید تازه همهٔ کانفیگ‌های قبلی این اینباند از کار می‌افتند تا مشتری اشتراک را به‌روز کند.",
    fp: "Fingerprint (uTLS)", spx: "spiderX", xver: "xver (PROXY به مقصد)", cert: "گواهی", sniName: "serverName (دامنه)", alpn: "ALPN", minVer: "حداقل نسخهٔ TLS",
    insecure: "گواهی خودامضا (برنامه‌ها تأیید CA را رد می‌کنند)", sniffOn: "فعال", dest: "destOverride", routeOnly: "فقط برای مسیریابی (routeOnly)", metaOnly: "metadataOnly",
    realityNeeds: "REALITY فقط با tcp، grpc و xhttp کار می‌کند.", visionNeeds: "Vision فقط با tcp و امنیت REALITY یا TLS کار می‌کند؛ خاموش شد.",
    needCert: "برای TLS یک گواهی انتخاب کنید (اینباندها ← گواهی‌ها).", saved: "ذخیره شد؛ Xray کانفیگ را تأیید کرد و نودها در همگام‌سازی بعدی می‌گیرند.",
    badJson: "JSON نامعتبر", mtu: "MTU", clone: "کپی", cloneTitle: "کپی اینباند {x}", cloneHint: "همان تنظیمات و همان نودها با شناسه و پورت تازه. کلیدهای REALITY و Shadowsocks تازه ساخته می‌شوند؛ تونل‌ها کپی نمی‌شوند.",
    newTag: "شناسهٔ تازه", newPort: "پورت تازه", cloneClients: "به همان کاربرها هم داده شود", cloned: "کپی ساخته شد", jsonHint: "تغییرات این‌جا با فرم یکی است. قبل از ذخیره Xray کانفیگ را بررسی می‌کند و اگر رد کند هیچ تغییری اعمال نمی‌شود." },
  en: { title: "Settings", form: "Form", json: "JSON", general: "General", protocol: "Protocol", transport: "Transport", security: "Security", sniffing: "Sniffing",
    listen: "Listen IP", port: "Port", portRouted: "This inbound is placed on nodes; change each node's port under Nodes & routes.", remark: "Name in apps", enabled: "Enabled",
    flow: "Flow", flowNone: "No flow", venc: "VLESS encryption (Xray 26)", vencOff: "Off (none)", vencX: "X25519 (fast)", vencM: "ML-KEM-768 (post-quantum)",
    vencHint: "VLESS's own encryption layer, over any transport. Only Xray 26 based apps (recent v2rayNG, v2rayN, Happ, Streisand) and recent Clash Meta support it; sing-box subscriptions leave this inbound out.",
    vencNew: "New key", server: "Server side (decryption)", client: "App side (encryption)",
    method: "Method", ssKey: "Server key", newKey: "New key", ssWarn: "Changing the method or server key means every customer must refresh the subscription.",
    network: "Network", path: "Path", host: "Host", service: "gRPC service name", multi: "multiMode (several streams, faster)", xmode: "XHTTP mode", padding: "Padding (bytes, e.g. 100-1000)",
    proxyProto: "Accept PROXY protocol (behind HAProxy/Nginx or a relay)", sec: "Security", none: "none", target: "Target", sni: "serverNames (one per line)",
    sids: "shortIds (one per line; the first goes in links)", addSid: "+ shortId", priv: "Private key", pub: "Public key (in links)", newKeys: "New keys",
    keysWarn: "New keys stop every existing config of this inbound until customers refresh the subscription.",
    fp: "Fingerprint (uTLS)", spx: "spiderX", xver: "xver (PROXY to target)", cert: "Certificate", sniName: "serverName (domain)", alpn: "ALPN", minVer: "Minimum TLS version",
    insecure: "Self-signed certificate (apps skip CA verification)", sniffOn: "Enabled", dest: "destOverride", routeOnly: "For routing only (routeOnly)", metaOnly: "metadataOnly",
    realityNeeds: "REALITY works only with tcp, grpc and xhttp.", visionNeeds: "Vision needs tcp with REALITY or TLS; it was turned off.",
    needCert: "Pick a certificate for TLS (Inbounds → Certificates).", saved: "Saved; Xray accepted the config and nodes pick it up on their next sync.",
    badJson: "Invalid JSON", mtu: "MTU", clone: "Copy", cloneTitle: "Copy inbound {x}", cloneHint: "Same settings and nodes under a new tag and port. REALITY and Shadowsocks keys are made fresh; tunnels are not copied.",
    newTag: "New tag", newPort: "New port", cloneClients: "Give it to the same clients", cloned: "Copy created", jsonHint: "This is the same data as the form. Xray checks the config before it is saved; if Xray refuses it nothing changes." },
};
const ie = (k, vars) => { let s = (IE[lang] || IE.en)[k] ?? IE.en[k] ?? k; for (const [a, b] of Object.entries(vars || {})) s = s.replaceAll(`{${a}}`, b); return s; };

async function qrImage(text) {
  const img = h("img", { class: "qr", alt: "QR" });
  try {
    const res = await fetch("/api/v2/admin/qr.png", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-Dark-Request": "1" }, body: JSON.stringify({ text }) });
    if (!res.ok) throw new Error(await res.text());
    img.src = URL.createObjectURL(await res.blob());
    img.addEventListener("load", () => URL.revokeObjectURL(img.src), { once: true });
  } catch (x) { return h("p", { class: "error" }, x.message); }
  return img;
}

function showQR(title, text) {
  const box = h("div", { class: "qrbox" }, h("p", { class: "muted small" }, "…"));
  openModal(title, h("div", { class: "form" }, box, h("code", { class: "linkline", dir: "ltr" }, text)), [h("button", { type: "button", onclick: (e) => { e.preventDefault(); copy(text); } }, t("copy"))]);
  qrImage(text).then((img) => box.replaceChildren(img));
}

function cloneInbound(ib) {
  const tag = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: `${ib.tag}-2`, dir: "ltr", autofocus: true });
  const port = h("input", { type: "number", min: "1", max: "65535", required: true, value: String((ib.port || 443) + 1), dir: "ltr" });
  const cl = h("input", { type: "checkbox", checked: true });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [tag, port]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}/clone`, { tag: tag.value.trim(), port: +port.value, clients: cl.checked }));
      document.getElementById("modal").close("save"); toast(ie("cloned")); route();
    } catch (x) { err.textContent = x.message; }
  } }, ie("clone"));
  openModal(ie("cloneTitle", { x: ib.tag }), h("div", { class: "form" }, h("p", { class: "small muted" }, ie("cloneHint")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, ie("newTag")), tag), h("label", {}, h("span", {}, ie("newPort")), port)),
    h("label", { class: "check" }, cl, ie("cloneClients")), err), [save]);
}

async function inboundEditor(tag, nodes, certs) {
  const [raw, routes, certList] = await Promise.all([
    api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}`),
    api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`).catch(() => []),
    certs ? Promise.resolve(certs) : api("GET", "/api/v2/admin/certs").catch(() => [])]);
  const parse = (v) => { try { return typeof v === "string" ? JSON.parse(v) : structuredClone(v || {}); } catch { return {}; } };
  let tpl = parse(raw.template), meta = parse(raw.meta);
  let enabled = raw.enabled;
  const routed = (routes || []).length > 0;
  let mode = "form";
  const body = h("div", { class: "form ie" });
  const err = h("div", { class: "error", role: "alert" });
  const tabs = h("div", { class: "steps", role: "tablist" });
  const d = openModal(`${ie("title")} · ${tag}`, h("div", {}, tabs, body, err), [
    h("button", { class: "primary", value: "save", onclick: async (e) => { e.preventDefault(); await save(); } }, t("save"))]);
  d.classList.add("wide");
  d.addEventListener("close", () => d.classList.remove("wide"), { once: true });

  const field = (label, el, hint) => h("label", {}, h("span", {}, label), el, hint ? h("small", { class: "muted" }, hint) : null);
  const obj = (o, k) => (o[k] && typeof o[k] === "object" && !Array.isArray(o[k]) ? o[k] : (o[k] = {}));
  const input = (get, set, attrs = {}) => { const el = h("input", { ...attrs, value: get() ?? "" }); el.addEventListener("input", () => set(attrs.type === "number" ? (el.value === "" ? undefined : +el.value) : el.value)); return el; };
  const select = (opts, get, set, onChange) => {
    const el = h("select", {}, opts.map(([v, l]) => h("option", { value: v, selected: String(get() ?? "") === v }, l)));
    el.addEventListener("change", () => { set(el.value); if (onChange) onChange(); });
    return el;
  };
  const check = (get, set, label, onChange) => { const c = h("input", { type: "checkbox", checked: !!get() }); c.addEventListener("change", () => { set(c.checked); if (onChange) onChange(); }); return h("label", { class: "check" }, c, label); };
  const lines = (get, set, attrs = {}) => { const el = h("textarea", { rows: "3", dir: "ltr", spellcheck: "false", ...attrs }, (get() || []).join("\n")); el.addEventListener("input", () => set(el.value.split(/\s+/).map((x) => x.trim()).filter(Boolean))); return el; };
  const section = (title, ...kids) => h("fieldset", { class: "ie-sec" }, h("legend", {}, title), ...kids);
  const keys = (kind, extra = {}) => api("POST", "/api/v2/admin/keys", { kind, ...extra });
  const randB64 = (n) => { const b = new Uint8Array(n); crypto.getRandomValues(b); return btoa(String.fromCharCode(...b)); };

  function draw() {
    tabs.replaceChildren(...[["form", ie("form")], ["json", ie("json")]].map(([k, l]) => h("button", { type: "button", role: "tab", "aria-current": mode === k ? "step" : null, onclick: () => switchTo(k) }, l)));
    body.replaceChildren(...(mode === "form" ? drawForm() : drawJSON()));
  }
  let jsonTpl, jsonMeta;
  function switchTo(k) {
    if (k === mode) return;
    if (mode === "json") {
      try { tpl = JSON.parse(jsonTpl.value); meta = JSON.parse(jsonMeta.value); } catch (x) { err.textContent = `${ie("badJson")}: ${x.message}`; return; }
    }
    err.textContent = ""; mode = k; draw();
  }
  function drawJSON() {
    jsonTpl = h("textarea", { class: "code", rows: "18", spellcheck: "false", dir: "ltr" }, JSON.stringify(tpl, null, 2));
    jsonMeta = h("textarea", { class: "code", rows: "8", spellcheck: "false", dir: "ltr" }, JSON.stringify(meta, null, 2));
    return [h("p", { class: "small muted" }, ie("jsonHint")), field(t("template"), jsonTpl), field(t("metaJson"), jsonMeta)];
  }

  function drawForm() {
    const proto = tpl.protocol;
    const settings = obj(tpl, "settings");
    const hasStream = !["wireguard", "shadowsocks"].includes(proto);
    const out = [];
    // General
    out.push(section(ie("general"),
      h("div", { class: "two" },
        field(ie("protocol"), h("input", { value: proto, disabled: true, dir: "ltr" })),
        field(ie("remark"), input(() => meta.remark, (v) => { meta.remark = v; }))),
      h("div", { class: "two" },
        field(ie("listen"), input(() => tpl.listen, (v) => { tpl.listen = v || undefined; }, { placeholder: "0.0.0.0", dir: "ltr" })),
        field(ie("port"), input(() => tpl.port, (v) => { tpl.port = v; }, { type: "number", min: "1", max: "65535", dir: "ltr", disabled: routed }), routed ? ie("portRouted") : null)),
      check(() => enabled, (v) => { enabled = v; }, ie("enabled"))));
    // Protocol
    if (proto === "vless") out.push(drawVless(settings));
    if (proto === "shadowsocks") out.push(drawSS(settings));
    if (proto === "wireguard") out.push(section(ie("protocol"), field(ie("mtu"), input(() => meta.mtu, (v) => { meta.mtu = v; }, { type: "number", min: "1280", max: "1500", placeholder: "1420", dir: "ltr" }))));
    if (hasStream && proto !== "hysteria") out.push(drawTransport());
    if (hasStream) out.push(drawSecurity());
    out.push(drawSniff());
    return out;
  }

  function drawVless(settings) {
    const st = obj(tpl, "streamSettings");
    const visionOK = (st.network || "tcp") === "tcp" && ["reality", "tls"].includes(st.security);
    const dec = settings.decryption && settings.decryption !== "none" ? settings.decryption : "";
    const kind = !dec ? "" : (dec.split(".").pop().length > 60 ? "vlessenc-mlkem" : "vlessenc-x25519");
    const decView = h("textarea", { class: "code", rows: "2", readonly: true, dir: "ltr" }, dec);
    const encView = h("textarea", { class: "code", rows: "2", readonly: true, dir: "ltr" }, meta.vless_encryption || "");
    const keyRow = h("div", { class: "form", hidden: !dec }, field(ie("server"), decView), field(ie("client"), encView));
    const regen = async (k) => {
      if (!k) { delete settings.decryption; delete meta.vless_encryption; decView.value = encView.value = ""; keyRow.hidden = true; regenBtn.hidden = true; return; }
      try { const r = await keys(k); settings.decryption = r.decryption; meta.vless_encryption = r.encryption; decView.value = r.decryption; encView.value = r.encryption; keyRow.hidden = false; regenBtn.hidden = false; } catch (x) { err.textContent = x.message; }
    };
    const vsel = select([["", ie("vencOff")], ["vlessenc-x25519", ie("vencX")], ["vlessenc-mlkem", ie("vencM")]], () => kind, () => {}, () => regen(vsel.value));
    const regenBtn = h("button", { type: "button", class: "ghost", hidden: !dec, onclick: () => regen(vsel.value) }, ie("vencNew"));
    return section(ie("protocol"),
      field(ie("flow"), select([["", ie("flowNone")], ["xtls-rprx-vision", "xtls-rprx-vision"]], () => settings.flow || "", (v) => { if (v) settings.flow = v; else delete settings.flow; }),
        visionOK ? null : "Vision: tcp + REALITY/TLS"),
      field(ie("venc"), h("div", { class: "row tight" }, vsel, regenBtn), ie("vencHint")), keyRow);
  }

  function drawSS(settings) {
    const sizes = { "2022-blake3-aes-128-gcm": 16, "2022-blake3-aes-256-gcm": 32, "2022-blake3-chacha20-poly1305": 32 };
    const key = input(() => settings.password, (v) => { settings.password = v; }, { dir: "ltr" });
    const meth = select(Object.keys(sizes).map((m) => [m, m]), () => settings.method, (v) => { settings.method = v; settings.password = randB64(sizes[v]); key.value = settings.password; });
    return section(ie("protocol"), h("p", { class: "small muted" }, ie("ssWarn")),
      field(ie("method"), meth),
      field(ie("ssKey"), h("div", { class: "row tight" }, key, h("button", { type: "button", class: "ghost", onclick: () => { settings.password = randB64(sizes[settings.method] || 32); key.value = settings.password; } }, ie("newKey")))));
  }

  function drawTransport() {
    const st = obj(tpl, "streamSettings");
    const net = st.network || "tcp";
    const kids = [];
    const netSel = select([["tcp", "TCP (RAW)"], ["ws", "WebSocket"], ["grpc", "gRPC"], ["httpupgrade", "HTTPUpgrade"], ["xhttp", "XHTTP"]], () => (net === "splithttp" ? "xhttp" : net), (v) => {
      for (const k of ["tcpSettings", "rawSettings", "wsSettings", "grpcSettings", "httpupgradeSettings", "xhttpSettings", "splithttpSettings"]) delete st[k];
      st.network = v;
      if (v === "ws") st.wsSettings = { path: "/" + Math.random().toString(36).slice(2, 8) };
      if (v === "grpc") st.grpcSettings = { serviceName: Math.random().toString(36).slice(2, 8) };
      if (v === "httpupgrade") st.httpupgradeSettings = { path: "/" + Math.random().toString(36).slice(2, 8) };
      if (v === "xhttp") st.xhttpSettings = { path: "/" + Math.random().toString(36).slice(2, 8), mode: "auto" };
      if (st.security === "reality" && !["tcp", "grpc", "xhttp"].includes(v)) { err.textContent = ie("realityNeeds"); }
      if (v !== "tcp" && tpl.settings?.flow) { delete tpl.settings.flow; err.textContent = ie("visionNeeds"); }
    }, draw);
    kids.push(field(ie("network"), netSel));
    if (net === "ws" || net === "httpupgrade") {
      const o = obj(st, net === "ws" ? "wsSettings" : "httpupgradeSettings");
      kids.push(h("div", { class: "two" }, field(ie("path"), input(() => o.path, (v) => { o.path = v; }, { dir: "ltr", placeholder: "/" })),
        field(ie("host"), input(() => o.host, (v) => { if (v) o.host = v; else delete o.host; }, { dir: "ltr" }))));
    }
    if (net === "grpc") {
      const o = obj(st, "grpcSettings");
      kids.push(field(ie("service"), input(() => o.serviceName, (v) => { o.serviceName = v; }, { dir: "ltr" })),
        check(() => o.multiMode, (v) => { if (v) o.multiMode = true; else delete o.multiMode; }, ie("multi")));
    }
    if (net === "xhttp" || net === "splithttp") {
      const o = obj(st, net === "xhttp" ? "xhttpSettings" : "splithttpSettings");
      kids.push(h("div", { class: "two" }, field(ie("path"), input(() => o.path, (v) => { o.path = v; }, { dir: "ltr", placeholder: "/" })),
        field(ie("host"), input(() => o.host, (v) => { if (v) o.host = v; else delete o.host; }, { dir: "ltr" }))),
        h("div", { class: "two" },
          field(ie("xmode"), select([["auto", "auto"], ["packet-up", "packet-up"], ["stream-up", "stream-up"], ["stream-one", "stream-one"]], () => o.mode || "auto", (v) => { o.mode = v; })),
          field(ie("padding"), input(() => o.xPaddingBytes, (v) => { if (v) o.xPaddingBytes = v; else delete o.xPaddingBytes; }, { dir: "ltr", placeholder: "100-1000" }))));
    }
    kids.push(check(() => st.sockopt?.acceptProxyProtocol, (v) => { const so = obj(st, "sockopt"); if (v) so.acceptProxyProtocol = true; else { delete so.acceptProxyProtocol; if (!Object.keys(so).length) delete st.sockopt; } }, ie("proxyProto")));
    return section(ie("transport"), ...kids);
  }

  function drawSecurity() {
    const st = obj(tpl, "streamSettings");
    const proto = tpl.protocol;
    const sec = proto === "hysteria" ? "tls" : (st.security || "none");
    const net = st.network || "tcp";
    const kids = [];
    const fpSel = () => field(ie("fp"), select(["chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"].map((x) => [x, x]), () => meta.fingerprint || "chrome", (v) => { meta.fingerprint = v; }));
    if (proto !== "hysteria") {
      const opts = [["none", ie("none")], ["tls", "TLS"]];
      if (["vless", "trojan"].includes(proto) && ["tcp", "grpc", "xhttp", "splithttp"].includes(net)) opts.push(["reality", "REALITY"]);
      kids.push(field(ie("sec"), select(opts, () => sec, async (v) => {
        delete st.realitySettings; delete st.tlsSettings; st.security = v;
        if (v === "reality") {
          const k = await keys("reality"); const sid = await keys("shortid");
          const name = meta.sni || "www.speedtest.net";
          st.realitySettings = { target: `${name}:443`, serverNames: [name], privateKey: k.private, shortIds: [sid.short_id.slice(0, 8)] };
          meta.public_key = k.public; meta.short_id = sid.short_id.slice(0, 8);
        } else { delete meta.public_key; delete meta.short_id; delete meta.spider_x; }
        if (v === "tls") st.tlsSettings = { serverName: "", alpn: ["h2", "http/1.1"], certificates: certList[0] ? [{ dark_cert: certList[0].id }] : [] };
        if (v === "none" && tpl.settings?.flow) { delete tpl.settings.flow; err.textContent = ie("visionNeeds"); }
        draw();
      })));
    }
    if (sec === "reality") {
      const rs = obj(st, "realitySettings");
      const tKey = rs.target !== undefined ? "target" : "dest";
      const pub = h("input", { value: meta.public_key || "", readonly: true, dir: "ltr" });
      const priv = input(() => rs.privateKey, async (v) => { rs.privateKey = v; try { const r = await keys("reality", { private: v }); meta.public_key = r.public; pub.value = r.public; err.textContent = ""; } catch (x) { err.textContent = x.message; } }, { dir: "ltr" });
      const sids = lines(() => rs.shortIds, (v) => { rs.shortIds = v; meta.short_id = v[0] || ""; });
      kids.push(
        h("div", { class: "two" },
          field(ie("target"), input(() => rs[tKey], (v) => { rs[tKey] = v; }, { dir: "ltr", placeholder: "www.speedtest.net:443" })),
          field(ie("xver"), select([["0", "0"], ["1", "1"], ["2", "2"]], () => String(rs.xver || 0), (v) => { if (+v) rs.xver = +v; else delete rs.xver; }))),
        h("div", { class: "two" },
          field(ie("sni"), lines(() => rs.serverNames, (v) => { rs.serverNames = v; })),
          field(ie("sids"), h("div", { class: "form" }, sids, h("button", { type: "button", class: "ghost", onclick: async () => {
            const r = await keys("shortid"); rs.shortIds = [...(rs.shortIds || []), r.short_id.slice(0, 8)]; if (!meta.short_id) meta.short_id = rs.shortIds[0]; sids.value = rs.shortIds.join("\n");
          } }, ie("addSid"))))),
        h("div", { class: "two" }, field(ie("priv"), priv), field(ie("pub"), pub)),
        h("div", { class: "row" }, h("button", { type: "button", class: "ghost", onclick: async () => {
          try { const k = await keys("reality"); rs.privateKey = k.private; meta.public_key = k.public; priv.value = k.private; pub.value = k.public; } catch (x) { err.textContent = x.message; }
        } }, ie("newKeys")), h("span", { class: "small muted" }, ie("keysWarn"))),
        h("div", { class: "two" }, fpSel(), field(ie("spx"), input(() => meta.spider_x, (v) => { if (v) meta.spider_x = v; else delete meta.spider_x; }, { dir: "ltr", placeholder: "/" }))));
    }
    if (sec === "tls") {
      const ts = obj(st, "tlsSettings");
      const certs = Array.isArray(ts.certificates) ? ts.certificates : (ts.certificates = []);
      const curCert = certs.find((c) => c.dark_cert)?.dark_cert || "";
      const alpnOpts = proto === "hysteria" ? ["h3"] : ["h3", "h2", "http/1.1"];
      const alpn = new Set(ts.alpn || []);
      kids.push(
        h("div", { class: "two" },
          field(ie("cert"), select([["", "—"], ...certList.map((c) => [c.id, `${c.id} · ${(c.domains || []).join(", ")}`])], () => curCert, (v) => {
            ts.certificates = v ? [{ dark_cert: v }] : []; const c = certList.find((x) => x.id === v); if (c?.domains?.[0] && !ts.serverName) ts.serverName = c.domains[0];
          }, draw)),
          field(ie("sniName"), input(() => ts.serverName, (v) => { ts.serverName = v; }, { dir: "ltr" }))),
        h("div", { class: "row" }, h("span", { class: "muted small" }, ie("alpn")), ...alpnOpts.map((a) => check(() => alpn.has(a), (v) => { if (v) alpn.add(a); else alpn.delete(a); ts.alpn = alpnOpts.filter((x) => alpn.has(x)); if (!ts.alpn.length) delete ts.alpn; }, a))),
        h("div", { class: "two" },
          field(ie("minVer"), select([["", "—"], ["1.2", "1.2"], ["1.3", "1.3"]], () => ts.minVersion || "", (v) => { if (v) ts.minVersion = v; else delete ts.minVersion; })),
          fpSel()),
        check(() => meta.insecure, (v) => { if (v) meta.insecure = true; else delete meta.insecure; }, ie("insecure")));
    }
    return section(ie("security"), ...kids);
  }

  function drawSniff() {
    const sn = obj(tpl, "sniffing");
    const dest = new Set(sn.destOverride || []);
    return section(ie("sniffing"),
      h("div", { class: "row" }, check(() => sn.enabled, (v) => { sn.enabled = v; }, ie("sniffOn")),
        check(() => sn.routeOnly, (v) => { if (v) sn.routeOnly = true; else delete sn.routeOnly; }, ie("routeOnly")),
        check(() => sn.metadataOnly, (v) => { if (v) sn.metadataOnly = true; else delete sn.metadataOnly; }, ie("metaOnly"))),
      h("div", { class: "row" }, h("span", { class: "muted small" }, ie("dest")), ...["http", "tls", "quic", "fakedns"].map((x) => check(() => dest.has(x), (v) => { if (v) dest.add(x); else dest.delete(x); sn.destOverride = [...dest]; }, x))));
  }

  async function save() {
    err.textContent = "";
    if (mode === "json") { try { tpl = JSON.parse(jsonTpl.value); meta = JSON.parse(jsonMeta.value); } catch (x) { err.textContent = `${ie("badJson")}: ${x.message}`; return; } }
    const st = tpl.streamSettings || {};
    if (st.security === "tls" && tpl.protocol !== "hysteria" && !(st.tlsSettings?.certificates || []).length) { err.textContent = ie("needCert"); return; }
    if (st.security === "reality" && st.realitySettings) meta.short_id = (st.realitySettings.shortIds || [])[0] || meta.short_id;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}`, { node_id: raw.node_id, template: tpl, meta, enabled }));
      toast(ie("saved")); d.close("save"); route();
    } catch (x) { err.textContent = x.message; }
  }
  draw();
}

function certForm(id) {
  const idf = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: id || "", disabled: !!id, autofocus: !id });
  const cert = h("textarea", { class: "code", rows: "8", required: true, spellcheck: "false", dir: "ltr", placeholder: "-----BEGIN CERTIFICATE-----" });
  const key = h("textarea", { class: "code", rows: "6", required: true, spellcheck: "false", dir: "ltr", placeholder: "-----BEGIN PRIVATE KEY-----" });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [idf, cert, key]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/certs/${encodeURIComponent(idf.value)}`, { Cert: cert.value, Key: key.value }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(id ? `${t("edit")} · ${id}` : t("addCert"), h("div", { class: "form" },
    h("p", { class: "small muted" }, t("certHint")),
    h("label", {}, h("span", {}, t("tag")), idf), h("label", {}, h("span", {}, t("certPem")), cert), h("label", {}, h("span", {}, t("keyPem")), key), err), [save]);
}

function inboundForm(nodes, presets, certs = []) {
  const node = h("select", { required: true }, (nodes || []).map((n) => h("option", { value: n.ID }, `${n.Name || n.ID} (${n.ID})`)));
  const preset = h("select", {}, (presets || []).map((p) => h("option", { value: p }, p)));
  const tag = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", placeholder: "de-reality" });
  const port = h("input", { type: "number", min: "1", max: "65535", required: true, value: "443" });
  const sni = h("input", { value: "www.speedtest.net" });
  const path = h("input", {});
  const remark = h("input", { value: "Dark VPN" });
  const addr = h("input", { required: true, placeholder: "1.2.3.4" });
  const tunnel = h("input", { placeholder: "5.6.7.8:2400" });
  const cert = h("select", {}, h("option", { value: "" }, t("noCert")), certs.map((c) => h("option", { value: c.id }, `${c.id} · ${c.domains.join(", ")}`)));
  const certRow = h("label", { hidden: true }, h("span", {}, t("certFor")), cert);
  const fbNote = h("p", { class: "small muted", hidden: true }, t("fbHint"));
  const syncPreset = () => {
    fbNote.hidden = preset.value !== "vless-tls-fallback";
    path.placeholder = preset.value === "wireguard" ? "10.66.0.0/16" : "";
    const tls = preset.value.includes("-tls-");
    certRow.hidden = !tls;
    if (tls && cert.value === "" && certs.length) { cert.value = certs[0].id; sni.value = certs[0].domains[0] || sni.value; }
  };
  preset.addEventListener("change", syncPreset);
  cert.addEventListener("change", () => { const c = certs.find((x) => x.id === cert.value); if (c && c.domains[0]) sni.value = c.domains[0]; });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    for (const f of [tag, port, addr]) if (!f.reportValidity()) return;
    const endpoints = [{ name: "Direct", address: addr.value.trim(), port: +port.value }];
    if (tunnel.value.trim()) {
      const [a, p] = tunnel.value.trim().split(/:(?=\d+$)/);
      endpoints.push({ name: "Tunnel", address: a, port: +p || +port.value });
    }
    try {
      await guarded(() => api("POST", "/api/v2/admin/inbounds/preset", { tag: tag.value, node_id: node.value, preset: preset.value, port: +port.value,
        sni: sni.value.trim(), path: path.value.trim(), remark: remark.value.trim(), endpoints, cert: preset.value.includes("-tls-") ? cert.value : "" }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(`${t("add")} · ${t("inbounds")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("node")), node), h("label", {}, h("span", {}, t("preset")), preset)),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), tag), h("label", {}, h("span", {}, t("port")), port)),
    certRow, fbNote,
    h("label", {}, h("span", {}, t("sni")), sni), h("label", {}, h("span", {}, t("path")), path),
    h("label", {}, h("span", {}, t("remark")), remark),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("directAddr")), addr), h("label", {}, h("span", {}, t("tunnelAddr")), tunnel)),
    err), [save]);
}

async function viewNodes() {
  const nodes = await api("GET", "/api/v2/admin/nodes");
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("nodes")), h("span", { class: "spacer" }), updateAllButton(nodes || []), h("button", { class: "primary", onclick: nodeForm }, t("addNode"))),
    (nodes || []).length ? nodeTrafficCard(nodes) : null,
    h("div", { class: "list" }, (nodes || []).length ? nodes.map((n) => nodeCard(n, true)) : h("p", { class: "muted" }, t("empty"))));
}

function nodeForm() {
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", autofocus: true });
  const name = h("input", {});
  const out = h("div", {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    try {
      const r = await guarded(() => api("POST", "/api/v2/admin/nodes", { ID: id.value, Name: name.value }));
      out.replaceChildren(h("p", { class: "small" }, t("installCmd")), h("code", {}, r.install), h("button", { type: "button", onclick: () => copy(r.install) }, t("copy")));
      save.disabled = true;
    } catch {}
  } }, t("save"));
  openModal(t("addNode"), h("div", { class: "form" },
    h("label", {}, h("span", {}, t("nodeId")), id), h("label", {}, h("span", {}, t("name")), name), out), [save]);
}

// Owners manage resellers at /api/v2/admin/reps; a reseller manages its own
// sub-sellers at /api/v2/rep/sellers with the same forms.
const repBase = () => (me && me.role === "rep" ? "/api/v2/rep/sellers" : "/api/v2/admin/reps");

async function viewSellers() {
  const [reps, inbounds] = await Promise.all([api("GET", "/api/v2/rep/sellers"), api("GET", "/api/v2/rep/inbounds")]);
  const cards = (reps || []).map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.Name || r.ID), h("span", { class: "badge" }, r.ID),
      h("span", { class: `badge ${r.Enabled ? "good" : "bad"} spacer` }, t(r.Enabled ? "enabled" : "disabled"))),
    h("div", { class: "muted small" }, `${t("volumeCredit")}: ${bytes(r.VolumeCredit)} · ${t("units")}: ${fmtNum(r.UnlimitedUnits)} · ${t("clientsCount", { n: fmtNum(r.Clients) })}`),
    h("div", { class: "muted small" }, policySummary(r.Policy)),
    h("div", { class: "row" }, h("button", { onclick: () => creditForm(r) }, t("transferCredit")),
      h("button", { class: "ghost", onclick: () => policyForm(r, inbounds) }, t("policy")),
      h("a", { class: "btn ghost", href: `#/activity?rep=${encodeURIComponent(r.ID)}` }, t("activity")),
      h("button", { class: "ghost", onclick: () => repForm(r) }, t("edit")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("sellers")), h("button", { class: "primary spacer", onclick: () => repForm(null) }, t("add"))),
    h("p", { class: "small muted" }, t("sellersHint")),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

async function viewReps() {
  const [reps, inbounds] = await Promise.all([api("GET", "/api/v2/admin/reps"), api("GET", "/api/v2/admin/inbounds")]);
  const cards = (reps || []).map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.Name || r.ID), h("span", { class: "badge" }, r.ID),
      r.ParentID ? h("span", { class: "badge" }, `${t("subOf")} ${r.ParentID}`) : null,
      h("span", { class: `badge ${r.Enabled ? "good" : "bad"} spacer` }, t(r.Enabled ? "enabled" : "disabled"))),
    h("div", { class: "muted small" }, `${t("volumeCredit")}: ${bytes(r.VolumeCredit)} · ${t("units")}: ${fmtNum(r.UnlimitedUnits)} · ${t("clientsCount", { n: fmtNum(r.Clients) })}`),
    h("div", { class: "muted small" }, policySummary(r.Policy)),
    h("div", { class: "row" }, h("button", { onclick: () => creditForm(r) }, t("addCredit")),
      h("button", { class: "ghost", onclick: () => policyForm(r, inbounds) }, t("policy")),
      h("button", { class: "ghost", onclick: () => paymentForm(r) }, t("payment")),
      h("a", { class: "btn ghost", href: `#/activity?rep=${encodeURIComponent(r.ID)}` }, t("activity")),
      h("button", { class: "ghost", onclick: () => repForm(r) }, t("edit")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("reps")), h("button", { class: "primary spacer", onclick: () => repForm(null) }, t("add"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

function repForm(r) {
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: r ? r.ID : "", disabled: !!r });
  const name = h("input", { value: r ? r.Name : "" });
  const en = h("input", { type: "checkbox", checked: r ? r.Enabled : true });
  const user = h("input", { autocomplete: "off", pattern: "[a-z0-9][a-z0-9._-]{0,62}" });
  const pass = h("input", { type: "password", autocomplete: "new-password", minlength: "10" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `${repBase()}/${encodeURIComponent(id.value)}`, { Name: name.value, Enabled: en.checked, Username: user.value, Password: pass.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(r ? `${t("edit")} · ${r.ID}` : `${t("add")} · ${t("reps")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("repId")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("label", { class: "check" }, en, t("enabled")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("loginUser")), user), h("label", {}, h("span", {}, t("loginPass")), pass))), [save]);
}

function creditForm(r) {
  const gb = h("input", { type: "number", step: "1", value: "0" });
  const units = h("input", { type: "number", step: "1", value: "0" });
  const note = h("input", {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      await guarded(() => api("POST", `${repBase()}/${encodeURIComponent(r.ID)}/credit`, { VolumeGB: +gb.value || 0, Units: +units.value || 0, Note: note.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(`${t(me.role === "rep" ? "transferCredit" : "addCredit")} · ${r.Name || r.ID}`, h("div", { class: "form" },
    me.role === "rep" ? h("p", { class: "small muted" }, t("transferHint")) : null,
    h("div", { class: "two" }, h("label", {}, h("span", {}, `${t("volumeCredit")} (GB)`), gb), h("label", {}, h("span", {}, t("units")), units)),
    h("p", { class: "small muted" }, t("unitsHint")), h("label", {}, h("span", {}, t("note")), note)), [save]);
}

function policySummary(p) {
  if (!p) return "";
  const parts = [];
  if (p.MaxClients) parts.push(`≤ ${fmtNum(p.MaxClients)} ${t("clients")}`);
  if (p.MaxDays) parts.push(`≤ ${fmtNum(p.MaxDays)}d`);
  if (p.MaxClientGB) parts.push(`≤ ${fmtNum(p.MaxClientGB)} GB`);
  if (!p.AllowUnlimited) parts.push(`✕ ${t("planUnlimited")}`);
  if (!p.CanDelete) parts.push(`✕ ${t("delete")}`);
  if (p.RefundUnused) parts.push(`↺ ${t("refunded")}`);
  if ((p.Inbounds || []).length) parts.push(p.Inbounds.join(", "));
  return parts.length ? `${t("policy")}: ${parts.join(" · ")}` : "";
}

function policyForm(r, inbounds) {
  const p = r.Policy || {};
  const num = (v) => h("input", { type: "number", min: "0", step: "1", value: v || 0 });
  const box = (v) => h("input", { type: "checkbox", checked: !!v });
  const f = { mc: num(p.MaxClients), md: num(p.MaxDays), mg: num(p.MaxClientGB), au: box(p.AllowUnlimited), cd: box(p.CanDelete), ru: box(p.RefundUnused), pg: num(p.PriceGB), pu: num(p.PriceUnit), mp: box(r.ManageProducts), cc: box(!p.DenyCreate), cr: box(!p.DenyRenew), ce: box(!p.DenyEdit) };
  const allowed = new Set(p.Inbounds || []);
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: allowed.has(ib.tag) }), `${ib.tag} · ${ib.node_id}`));
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      await guarded(() => api("PUT", `${repBase()}/${encodeURIComponent(r.ID)}/policy`, {
        MaxClients: +f.mc.value || 0, MaxDays: +f.md.value || 0, MaxClientGB: +f.mg.value || 0,
        AllowUnlimited: f.au.checked, CanDelete: f.cd.checked, RefundUnused: f.ru.checked,
        DenyCreate: !f.cc.checked, DenyRenew: !f.cr.checked, DenyEdit: !f.ce.checked,
        Inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value),
        PriceGB: +f.pg.value || 0, PriceUnit: +f.pu.value || 0 }));
      if (me.role === "owner" && f.mp.checked !== !!r.ManageProducts) await api("PUT", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/products`, { Enabled: f.mp.checked });
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  const lab = (k, el) => h("label", {}, h("span", {}, t(k)), el);
  openModal(`${t("policy")} · ${r.Name || r.ID}`, h("div", { class: "form" },
    h("div", { class: "two" }, lab("maxClients", f.mc), lab("maxDays", f.md)),
    lab("maxClientGB", f.mg),
    h("label", { class: "check" }, f.cc, t("canCreate")),
    h("label", { class: "check" }, f.cr, t("canRenew")),
    h("label", { class: "check" }, f.ce, t("canEdit")),
    h("label", { class: "check" }, f.au, t("allowUnlimited")),
    h("label", { class: "check" }, f.cd, t("canDelete")),
    h("label", { class: "check" }, f.ru, t("refundUnused")),
    me.role === "owner" ? h("label", { class: "check" }, f.mp, t("manageProducts")) : null,
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("allowedInbounds")), boxes),
    me.role === "rep" ? null : h("div", { class: "two" }, lab("priceGB", f.pg), lab("priceUnit", f.pu)), err), [save]);
}

function paymentForm(r) {
  const amount = h("input", { type: "number", step: "1", required: true, autofocus: true });
  const note = h("input", {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!amount.reportValidity()) return;
    try {
      await guarded(() => api("POST", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/payment`, { Amount: Math.round(+amount.value), Note: note.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(`${t("payment")} · ${r.Name || r.ID}`, h("div", { class: "form" },
    h("label", {}, h("span", {}, t("amount")), amount), h("label", {}, h("span", {}, t("note")), note)), [save]);
}

function money(n) { return fmtNum(n); }

async function viewShop() {
  const owner = me.role === "owner";
  const [products, orders, inbounds, reps] = await Promise.all([
    api("GET", "/api/v2/shop/products"), api("GET", "/api/v2/shop/orders"),
    owner ? api("GET", "/api/v2/admin/inbounds") : api("GET", "/api/v2/rep/inbounds"),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([])]);
  const canEdit = owner || me.manage_products;
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const planText = (p) => `${p.plan === "unlimited" ? t("planUnlimited") : `${fmtNum(p.gb)} GB`} · ${fmtNum(p.days)}d${p.max_ips ? ` · IP ≤ ${fmtNum(p.max_ips)}` : ""}`;
  const stCls = { published: "good", draft: "", archived: "bad", pending: "warn", fulfilled: "good", cancelled: "bad" };
  const cards = products.map((p) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, p.name), h("span", { class: "badge" }, p.id),
      owner && p.seller ? h("span", { class: "badge" }, `${t("seller")}: ${p.seller}`) : null,
      h("span", { class: `badge spacer ${stCls[p.status]}` }, t(`status_${p.status}`))),
    h("div", { class: "row small muted" }, h("span", {}, planText(p)), h("strong", { class: "spacer" }, money(p.price))),
    h("div", { class: "row" },
      p.status === "published" ? h("button", { onclick: () => orderForm(products, p.id) }, t("newOrder")) : null,
      canEdit && p.status !== "archived" ? h("button", { class: "ghost", onclick: () => productForm(p, inbounds, reps) }, t("edit")) : null)));
  const body = h("tbody", {}, orders.map((o) => h("tr", {},
    h("td", { class: "small" }, `#${o.id}`, h("div", { class: "muted" }, fmtTime(o.created_unix))),
    h("td", {}, o.product_name || o.product_id, owner && o.seller ? h("div", { class: "small muted" }, o.seller) : null),
    h("td", { class: "small" }, o.buyer || "—", o.note ? h("div", { class: "muted" }, o.note) : null),
    h("td", { class: "small mono" }, o.client_email ? `${o.renew ? "↻ " : ""}${o.client_email}` : t("newClient")),
    h("td", {}, money(o.amount)),
    h("td", {}, h("span", { class: `badge ${stCls[o.status]}` }, t(`status_${o.status}`))),
    h("td", {}, o.status === "pending" ? h("div", { class: "row" },
      h("button", { class: "primary", onclick: async () => {
        const r = await guarded(() => api("POST", `/api/v2/shop/orders/${o.id}/fulfill`));
        toast(t("delivered", { x: r.client_email })); route();
      } }, t("fulfill")),
      h("button", { class: "ghost danger", onclick: async () => { await guarded(() => api("POST", `/api/v2/shop/orders/${o.id}/cancel`)); route(); } }, t("cancelOrder"))) : null))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("shop")),
      h("span", { class: "spacer" }), canEdit ? h("button", { onclick: () => productForm(null, inbounds, reps) }, `${t("add")} · ${t("product")}`) : null,
      products.some((p) => p.status === "published") ? h("button", { class: "primary", onclick: () => orderForm(products) }, t("newOrder")) : null),
    h("p", { class: "small muted" }, t("shopHint")),
    h("h2", {}, t("products")),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted small" }, t("empty"))),
    h("h2", { class: "section" }, t("orders")),
    orders.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
      h("thead", {}, h("tr", {}, ["#", t("product"), t("buyer").split(" (")[0], t("clients"), t("price"), t("statusLabel"), ""].map((x) => h("th", {}, x)))), body))
      : h("p", { class: "muted small" }, t("empty")));
}

function productForm(p, inbounds, reps) {
  const owner = me.role === "owner";
  const v = p || { id: "", name: "", plan: "volume", gb: 50, days: 30, max_ips: 2, inbounds: [], price: 0, status: "published", first_use: true, seller: "" };
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: v.id, disabled: !!p });
  const name = h("input", { required: true, maxlength: "120", value: v.name });
  const plan = h("select", {}, h("option", { value: "volume", selected: v.plan === "volume" }, t("planVolume")), h("option", { value: "unlimited", selected: v.plan === "unlimited" }, t("planUnlimited")));
  const gb = h("input", { type: "number", min: "1", value: v.gb || 50 });
  const days = h("input", { type: "number", min: "1", max: "3650", required: true, value: v.days });
  const ips = h("input", { type: "number", min: "0", value: v.max_ips });
  const price = h("input", { type: "number", min: "0", step: "1", value: v.price });
  const stars = h("input", { type: "number", min: "0", max: "100000", step: "1", value: v.price_stars || 0, dir: "ltr" });
  const first = h("input", { type: "checkbox", checked: v.first_use });
  const status = h("select", {}, ["draft", "published", "archived"].map((x) => h("option", { value: x, selected: v.status === x }, t(`status_${x}`))));
  const seller = owner ? h("select", {}, h("option", { value: "" }, t("ownerSelf")), (reps || []).map((r) => h("option", { value: r.ID, selected: v.seller === r.ID }, r.Name || r.ID))) : null;
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: (v.inbounds || []).includes(ib.tag) || (!p && inbounds.length === 1) }), `${ib.tag} · ${ib.node_id}`));
  const gbRow = h("label", {}, h("span", {}, "GB"), gb);
  const sync = () => { gbRow.hidden = plan.value === "unlimited"; };
  plan.addEventListener("change", sync); sync();
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [id, name, days]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/shop/products/${encodeURIComponent(id.value)}`, {
        name: name.value, plan: plan.value, gb: +gb.value || 0, days: +days.value, max_ips: +ips.value || 0, price: Math.round(+price.value || 0), price_stars: Math.round(+stars.value || 0),
        status: status.value, first_use: first.checked, seller: seller ? seller.value : "",
        inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value) }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(p ? `${t("edit")} · ${p.name}` : `${t("add")} · ${t("product")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("plan")), plan), gbRow),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("days").split(" (")[0]), days), h("label", {}, h("span", {}, t("maxIPs")), ips)),
    h("label", { class: "check" }, first, t("firstUse")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("price")), price), h("label", {}, h("span", {}, t("statusLabel")), status)),
    owner ? h("label", {}, h("span", {}, t("priceStars")), stars) : null,
    owner ? h("p", { class: "small muted" }, t("priceStarsHint")) : null,
    seller ? h("label", {}, h("span", {}, t("seller")), seller) : null,
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("assignInbounds")), boxes), err), [save]);
}

async function orderForm(products, preselect) {
  const list = products.filter((p) => p.status === "published");
  const clients = await api("GET", "/api/v2/admin/clients");
  const prod = h("select", {}, list.map((p) => h("option", { value: p.id, selected: p.id === preselect }, `${p.name} · ${money(p.price)}`)));
  const buyer = h("input", { maxlength: "200", autofocus: true });
  const renew = h("select", {}, h("option", { value: "" }, t("newClient")), clients.map((c) => h("option", { value: c.Email }, c.Email)));
  const note = h("input", { maxlength: "1000" });
  const err = h("div", { class: "error" });
  const mk = (deliver) => h("button", { class: deliver ? "primary" : "", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    try {
      const r = await api("POST", "/api/v2/shop/orders", { product_id: prod.value, buyer: buyer.value, client_email: renew.value, note: note.value });
      if (deliver) { const d = await api("POST", `/api/v2/shop/orders/${r.id}/fulfill`); toast(t("delivered", { x: d.client_email })); } else toast(t("saved"));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, deliver ? `${t("save")} + ${t("fulfill")}` : t("save"));
  openModal(t("newOrder"), h("div", { class: "form" },
    h("label", {}, h("span", {}, t("product")), prod), h("label", {}, h("span", {}, t("buyer")), buyer),
    h("label", {}, h("span", {}, t("renewClient")), renew), h("label", {}, h("span", {}, t("note")), note), err), [mk(false), mk(true)]);
}


async function viewFinance() {
  const days = new URLSearchParams(location.hash.split("?")[1] || "").get("days") ?? "30";
  const rows = await api("GET", `/api/v2/admin/finance?days=${encodeURIComponent(days)}`);
  const period = h("select", { onchange: (e) => { location.hash = `#/finance?days=${e.target.value}`; } },
    [["7", t("last7")], ["30", t("last30")], ["0", t("allTime")]].map(([v, l]) => h("option", { value: v, selected: v === days }, l)));
  const sum = (k) => rows.reduce((a, r) => a + r[k], 0);
  const stat = (k, v, cls = "") => h("div", { class: `card stat ${cls}` }, h("div", { class: "v" }, v), h("div", { class: "k" }, t(k)));
  const head = ["reps", "sold", "refunded", "granted", "revenue", "paid", "debt", "balance"];
  const table = h("div", { class: "table-wrap" }, h("table", { class: "table" },
    h("thead", {}, h("tr", {}, head.map((k) => h("th", { scope: "col" }, t(k))))),
    h("tbody", {}, rows.map((r) => h("tr", {},
      h("th", { scope: "row" }, h("a", { href: `#/activity?rep=${encodeURIComponent(r.RepID)}` }, r.Name || r.RepID), h("div", { class: "small muted" }, t("clientsCount", { n: fmtNum(r.Clients) }))),
      h("td", {}, bytes(r.SoldBytes), r.SoldUnits ? h("div", { class: "small muted" }, `${fmtNum(r.SoldUnits)} ${t("units")}`) : null),
      h("td", {}, bytes(r.RefundBytes), r.RefundUnits ? h("div", { class: "small muted" }, `${fmtNum(r.RefundUnits)} ${t("units")}`) : null),
      h("td", {}, bytes(r.GrantedBytes), r.GrantedUnits ? h("div", { class: "small muted" }, `${fmtNum(r.GrantedUnits)} ${t("units")}`) : null),
      h("td", {}, money(r.Revenue)), h("td", {}, money(r.Paid)),
      h("td", { class: r.Debt > 0 ? "warn-text" : "" }, money(r.Debt)),
      h("td", {}, bytes(r.VolumeCredit), h("div", { class: "small muted" }, `${fmtNum(r.Units)} ${t("units")}`)))))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("finance")), h("label", { class: "spacer inline" }, h("span", { class: "small muted" }, t("period")), period)),
    h("div", { class: "grid" }, stat("sold", bytes(sum("SoldBytes"))), stat("revenue", money(sum("Revenue"))), stat("paid", money(sum("Paid"))),
      stat("debt", money(sum("Debt")), sum("Debt") > 0 ? "warn" : "")),
    rows.length ? table : h("p", { class: "muted" }, t("empty")));
}

async function viewActivity() {
  const q = new URLSearchParams(location.hash.split("?")[1] || "");
  const rep = q.get("rep") || "";
  const owner = me.role === "owner";
  const [first, reps] = await Promise.all([
    api("GET", `/api/v2/rep/ledger?rep=${encodeURIComponent(rep)}`),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([]),
  ]);
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const delta = (e) => [
    e.VolumeDelta ? `${e.VolumeDelta > 0 ? "+" : "−"}${bytes(Math.abs(e.VolumeDelta))}` : "",
    e.UnitsDelta ? `${e.UnitsDelta > 0 ? "+" : "−"}${fmtNum(Math.abs(e.UnitsDelta))} ${t("units")}` : "",
    e.Amount ? `${t("amount")} ${money(e.Amount)}` : ""].filter(Boolean).join(" · ");
  const cls = { charge: "", refund: "good", adjust: "good", delete: "bad", payment: "good", transfer: "warn" };
  const body = h("tbody", {});
  const add = (list) => body.append(...list.map((e) => h("tr", {},
    h("td", { class: "small" }, fmtTime(e.Unix)),
    owner && !rep ? h("td", {}, e.RepID) : null,
    h("td", {}, h("span", { class: `badge ${cls[e.Kind] || ""}` }, t(`kind_${e.Kind}`)), e.ClientEmail ? ` ${e.ClientEmail}` : ""),
    h("td", { class: "mono small" }, delta(e)),
    h("td", { class: "small muted" }, [e.Actor, e.Note].filter(Boolean).join(" — ")))));
  add(first);
  let last = first.at(-1);
  const more = h("button", { class: "ghost", hidden: first.length < 100, onclick: async () => {
    const next = await api("GET", `/api/v2/rep/ledger?rep=${encodeURIComponent(rep)}&before=${last.ID}`);
    add(next); last = next.at(-1) || last; more.hidden = next.length < 100;
  } }, t("more"));
  const repSel = owner ? h("select", { onchange: (e) => { location.hash = `#/activity${e.target.value ? `?rep=${encodeURIComponent(e.target.value)}` : ""}`; } },
    h("option", { value: "" }, t("all")), reps.map((r) => h("option", { value: r.ID, selected: r.ID === rep }, r.Name || r.ID))) : null;
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("activity")), repSel ? h("span", { class: "spacer" }, repSel) : null),
    first.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
      h("thead", {}, h("tr", {}, h("th", {}, t("when")), owner && !rep ? h("th", {}, t("reps")) : null, h("th", {}, t("what")), h("th", {}, t("credit")), h("th", {}, t("who")))),
      body)) : h("p", { class: "muted" }, t("empty")), more);
}

// Daily traffic as a neon line chart: grid, glowing lines, soft area fill.
function barsSvg(days, W = 600) {
  const NS = "http://www.w3.org/2000/svg", H = 190, P = 8, L = 0;
  const el = (n, a) => { const x = document.createElementNS(NS, n); for (const [k, v] of Object.entries(a)) x.setAttribute(k, v); return x; };
  const svg = el("svg", { viewBox: `0 0 ${W} ${H + 20}`, class: "bars", role: "img", "aria-label": t("usageChart") });
  const id = `c${++markSeq}`;
  const defs = el("defs", {});
  const glow = el("filter", { id: `${id}g`, x: "-10%", y: "-30%", width: "120%", height: "160%" });
  glow.append(el("feGaussianBlur", { stdDeviation: "3.5", result: "b" }));
  const merge = el("feMerge", {}); merge.append(el("feMergeNode", { in: "b" }), el("feMergeNode", { in: "SourceGraphic" })); glow.append(merge);
  const area = (col, key) => { const g = el("linearGradient", { id: `${id}${key}`, x1: 0, y1: 0, x2: 0, y2: 1 });
    g.append(el("stop", { offset: "0", "stop-color": col, "stop-opacity": ".35" }), el("stop", { offset: "1", "stop-color": col, "stop-opacity": "0" })); return g; };
  defs.append(glow, area("#2de2e6", "d"), area("#9b5cff", "u"));
  svg.append(defs);
  if (!days.length) return svg;
  const top = Math.max(1, ...days.map((d) => Math.max(d.Down, d.Up))) * 1.12;
  const x = (i) => L + (days.length === 1 ? W / 2 : (i / (days.length - 1)) * (W - L - 2 * P) + P);
  const y = (v) => H - (v / top) * (H - P);
  for (let k = 0; k <= 4; k++) {
    const yy = P + ((H - P) * k) / 4;
    svg.append(el("line", { x1: 0, x2: W, y1: yy, y2: yy, class: "grid-line" }));
  }
  const series = (key, col) => {
    const pts = days.map((d, i) => [x(i), y(d[key])]);
    const line = pts.map(([a, b]) => `${a.toFixed(1)},${b.toFixed(1)}`).join(" ");
    svg.append(el("polygon", { points: `${pts[0][0]},${H} ${line} ${pts.at(-1)[0]},${H}`, fill: `url(#${id}${key === "Down" ? "d" : "u"})` }));
    svg.append(el("polyline", { points: line, fill: "none", stroke: col, "stroke-width": 2.2, "stroke-linejoin": "round", "stroke-linecap": "round", filter: `url(#${id}g)`, "vector-effect": "non-scaling-stroke" }));
    const [lx, ly] = pts.at(-1);
    svg.append(el("circle", { cx: lx, cy: ly, r: 4, fill: col, filter: `url(#${id}g)` }));
  };
  series("Up", "#9b5cff");
  series("Down", "#2de2e6");
  const step = Math.max(1, Math.round(days.length / 6));
  days.forEach((d, i) => {
    const hit = el("rect", { x: x(i) - W / days.length / 2, y: 0, width: W / days.length, height: H, class: "bar-hit" });
    const title = el("title", {}); title.textContent = `${dayLabel(d.Day, true)}: ↓ ${bytes(d.Down)} · ↑ ${bytes(d.Up)}`;
    hit.append(title); svg.append(hit);
    if ((days.length - 1 - i) % step === 0) {
      const lab = el("text", { x: x(i), y: H + 15, "text-anchor": "middle", class: "bar-label" }); lab.textContent = dayLabel(d.Day); svg.append(lab);
    }
  });
  return svg;
}

function legend() {
  return h("span", { class: "spacer legend" }, h("i", { class: "sw down" }), `↓ ${t("inRate")}`, h("i", { class: "sw up" }), `↑ ${t("outRate")}`);
}

async function usageChart(c) {
  const days = await api("GET", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/daily?days=30`);
  const sum = days.reduce((a, d) => a + d.Up + d.Down, 0);
  openModal(`${t("usageChart")} · ${c.Email}`, h("div", { class: "form" },
    h("div", { class: "row small" }, h("strong", {}, t("totalLast30", { v: bytes(sum) })), legend()), barsSvg(days)));
}

function subSettingsCard() {
  const fa = lang === "fa";
  const L = fa ? { h: "اشتراک و صفحهٔ مشتری", hint: "وقتی مشتری لینک اشتراکش را در مرورگر باز کند، یک صفحه با حجم، زمان، QR و دکمهٔ افزودن به برنامه‌ها می‌بیند. برنامه‌ها مثل قبل کانفیگ می‌گیرند.",
      title: "عنوان (خالی = نام کلاینت)", tpl: "نام کانفیگ‌ها", vars: "متغیرها: {remark} {path} {left} {used} {total} {days} {email} {proto} {tag}", info: "نمایش حجم و روز باقی‌مانده به‌صورت دو ردیف بالای فهرست سرورها",
      ann: "اعلان (در صفحه و برنامه‌هایی مثل Happ و Hiddify)", sup: "لینک پشتیبانی (https://t.me/...)", save: "ذخیره", preview: "پیش‌نمایش",
      frag: "fragment برای عبور از فیلتر (فرمت Xray JSON؛ v2rayNG و Happ در صفحهٔ مشتری همین را می‌گیرند)", fl: "طول تکه‌ها (بایت)", fi: "فاصله (میلی‌ثانیه)", noise: "noise روی UDP" }
    : { h: "Subscription & customer page", hint: "Opening the subscription link in a browser shows a page with volume, time, QR and add-to-app buttons. Apps keep getting configs as before.",
      title: "Title (empty = client name)", tpl: "Config names", vars: "Variables: {remark} {path} {left} {used} {total} {days} {email} {proto} {tag}", info: "Show remaining volume and days as two entries at the top of the server list",
      ann: "Announcement (page and apps such as Happ, Hiddify)", sup: "Support link (https://t.me/...)", save: "Save", preview: "Preview",
      frag: "Fragment to get past filtering (Xray JSON format; v2rayNG and Happ on the customer page get it)", fl: "Fragment length (bytes)", fi: "Interval (ms)", noise: "UDP noise" };
  const card = h("div", { class: "card form" }, h("h2", {}, L.h), h("p", { class: "small muted" }, "…"));
  api("GET", "/api/v2/admin/sub-settings").then((v) => {
    const title = h("input", { value: v.title || "" });
    const tpl = h("input", { value: v.remark_template || "", dir: "ltr", placeholder: "{remark} | {path}" });
    const prevOut = h("bdi", { class: "mono", dir: "ltr" });
    const prev = h("div", { class: "small muted" }, `${L.preview}: `, prevOut);
    const sample = { remark: "Dark VPN", path: "Direct", left: "12.40 GB", used: "17.60 GB", total: "30 GB", days: "18", email: "ali", proto: "vless", tag: "multi-reality" };
    const upd = () => { prevOut.textContent = (tpl.value || "{remark} | {path}").replace(/\{(\w+)\}/g, (m, k) => sample[k] ?? m); };
    tpl.addEventListener("input", upd); upd();
    const info = h("input", { type: "checkbox", checked: !!v.info_lines });
    const ann = h("textarea", { rows: 2 }); ann.value = v.announce || "";
    const sup = h("input", { value: v.support_url || "", dir: "ltr", placeholder: "https://t.me/" });
    const err = h("div", { class: "error small" });
    const frag = h("input", { type: "checkbox", checked: !!v.fragment });
    const fl = h("input", { value: v.fragment_length || "", placeholder: "100-200", dir: "ltr" });
    const fi = h("input", { value: v.fragment_interval || "", placeholder: "10-20", dir: "ltr" });
    const noise = h("input", { type: "checkbox", checked: !!v.noise });
    const mini = h("input", { type: "checkbox", checked: !!v.miniapp });
    const miniURL = h("input", { value: v.miniapp_url || "", dir: "ltr", placeholder: "https://panel.example.com/app/" });
    fill(card, h("h2", {}, L.h), h("p", { class: "small muted" }, L.hint),
      h("label", {}, h("span", {}, L.title), title),
      h("label", {}, h("span", {}, L.tpl), tpl), h("div", { class: "small muted" }, L.vars), prev,
      h("label", { class: "check" }, info, h("span", {}, L.info)),
      h("label", {}, h("span", {}, L.ann), ann), h("label", {}, h("span", {}, L.sup), sup),
      h("label", { class: "check" }, frag, h("span", {}, L.frag)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, L.fl), fl), h("label", {}, h("span", {}, L.fi), fi)),
      h("label", { class: "check" }, noise, h("span", {}, L.noise)),
      h("h2", { class: "section" }, t("miniTitle")), h("p", { class: "small muted" }, t("miniHint")),
      h("label", { class: "check" }, mini, h("span", {}, t("miniOn"))),
      h("label", {}, h("span", {}, t("miniURL")), miniURL), err,
      h("button", { class: "primary", onclick: async () => {
        err.textContent = "";
        try { await guarded(() => api("PUT", "/api/v2/admin/sub-settings", { title: title.value, remark_template: tpl.value, info_lines: info.checked, announce: ann.value, support_url: sup.value,
          fragment: frag.checked, fragment_length: fl.value.trim(), fragment_interval: fi.value.trim(), noise: noise.checked, miniapp: mini.checked, miniapp_url: miniURL.value.trim() })); toast(t("saved")); }
        catch (x) { err.textContent = x.message; }
      } }, L.save));
  }).catch(() => {});
  return card;
}

function tfaCard() {
  const out = h("div", { class: "form" });
  const card = h("div", { class: "card form" }, h("h2", {}, t("tfaTitle")), h("p", { class: "small muted" }, t("tfaHint")), out);
  const draw = () => {
    if (me.totp) {
      const pw = h("input", { type: "password", autocomplete: "current-password", required: true });
      const code = h("input", { inputmode: "numeric", autocomplete: "one-time-code", required: true, maxlength: "7", dir: "ltr" });
      const err = h("div", { class: "error" });
      fill(out, h("p", {}, h("span", { class: "badge good" }, t("tfaOn"))), h("form", { class: "form", onsubmit: async (e) => {
        e.preventDefault(); err.textContent = "";
        try { await api("POST", "/api/v2/auth/2fa/disable", { Password: pw.value, Code: code.value }); me.totp = false; toast(t("tfaOff")); draw(); }
        catch (x) { err.textContent = x.message; }
      } }, h("div", { class: "two" }, h("label", {}, h("span", {}, t("password")), pw), h("label", {}, h("span", {}, t("tfaCode")), code)), err,
        h("button", { class: "danger", type: "submit" }, t("tfaDisable"))));
      return;
    }
    fill(out, h("button", { onclick: async () => {
      const r = await guarded(() => api("POST", "/api/v2/auth/2fa/setup"));
      if (!r) return;
      const code = h("input", { inputmode: "numeric", autocomplete: "one-time-code", required: true, maxlength: "7", dir: "ltr", autofocus: true });
      const err = h("div", { class: "error" });
      fill(out, h("p", { class: "small" }, t("tfaScan")), h("img", { class: "qr", src: r.qr, alt: "QR", width: 200, height: 200 }),
        h("p", { class: "small" }, `${t("tfaKey")}: `, h("code", { dir: "ltr", class: "tfakey" }, r.secret.replace(/(.{4})/g, "$1 ").trim())),
        h("form", { class: "form", onsubmit: async (e) => {
          e.preventDefault(); err.textContent = "";
          try { await api("POST", "/api/v2/auth/2fa/enable", { Code: code.value }); me.totp = true; toast(t("tfaEnabled")); draw(); }
          catch (x) { err.textContent = x.message; }
        } }, h("label", {}, h("span", {}, t("tfaCode")), code), err, h("button", { class: "primary", type: "submit" }, t("tfaEnable"))));
      code.focus();
    } }, t("tfaStart")));
  };
  draw();
  return card;
}

function webhookCard(wh, L) {
  const on = h("input", { type: "checkbox", checked: wh.enabled });
  const url = h("input", { type: "url", value: wh.url || "", placeholder: "https://example.com/dark-hook", dir: "ltr" });
  const secret = h("input", { type: "password", autocomplete: "off", placeholder: wh.has_secret ? t("whSecretSaved") : "" });
  const kinds = ["client", "change", "node"].map((k) => [k, h("input", { type: "checkbox", checked: (wh.kinds || []).includes(k) })]);
  const err = h("div", { class: "error" });
  const status = h("p", { class: "small muted" }, `${t("whLastOK")}: ${relTime(wh.last_ok, L)}`, wh.last_error ? h("span", { class: "bad-text" }, ` · ${t("whLastErr")}: ${wh.last_error}`) : null);
  const body = () => ({ enabled: on.checked, url: url.value.trim(), secret: secret.value, kinds: kinds.filter(([, c]) => c.checked).map(([k]) => k) });
  return h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault(); err.textContent = "";
    try { await api("PUT", "/api/v2/admin/webhook", body()); toast(t("saved")); secret.value = ""; } catch (x) { err.textContent = x.message; }
  } }, h("h2", {}, t("whTitle")), h("p", { class: "small muted" }, t("whHint")),
    h("label", { class: "check" }, on, h("span", {}, t("whOn"))),
    h("label", {}, h("span", {}, t("whURL")), url), h("label", {}, h("span", {}, t("whSecret")), secret),
    h("fieldset", {}, h("legend", {}, t("whKinds")), kinds.map(([k, c]) => h("label", { class: "check" }, c, h("span", {}, t(`wh_${k}`))))),
    status, err,
    h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, t("save")),
      h("button", { type: "button", onclick: async () => {
        err.textContent = "";
        try { await api("PUT", "/api/v2/admin/webhook", body()); secret.value = ""; await api("POST", "/api/v2/admin/webhook/test"); toast(t("whTestOK")); } catch (x) { err.textContent = x.message; }
      } }, t("whTest"))));
}

function keysCard(keys, L) {
  const roleBadge = (r) => h("span", { class: `badge ${r === "admin" ? "warn" : ""}` }, t(`role_${r}`));
  const add = () => {
    const name = h("input", { required: true, maxlength: "64", placeholder: "billing-bot" });
    const level = h("select", {}, ["viewer", "admin"].map((r) => h("option", { value: r }, t(`role_${r}`))));
    const days = h("input", { type: "number", min: "0", max: "3650", value: "0", dir: "ltr" });
    const err = h("div", { class: "error" });
    const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
      e.preventDefault(); err.textContent = "";
      try {
        const r = await api("POST", "/api/v2/admin/apikeys", { Name: name.value, Level: level.value, Days: +days.value || 0 });
        const d = openModal(t("addKey"), h("div", { class: "form" }, h("p", { class: "small" }, t("keyOnce")),
          h("code", { dir: "ltr", class: "tfakey keyout" }, r.key)), [h("button", { class: "primary", onclick: (ev) => { ev.preventDefault(); copy(r.key); } }, t("copy"))]);
        d.addEventListener("close", () => route(), { once: true });
      } catch (x) { err.textContent = x.message; }
    } }, t("save"));
    openModal(t("addKey"), h("div", { class: "form" }, h("p", { class: "small muted" }, t("keysHint")),
      h("label", {}, h("span", {}, t("keyName")), name), h("div", { class: "two" }, h("label", {}, h("span", {}, t("role")), level), h("label", {}, h("span", {}, t("keyDays")), days)), err), [save]);
  };
  const ref = async () => {
    const routes = await guarded(() => api("GET", "/api/v2/admin/docs"));
    if (!routes) return;
    openModal(t("apiRef"), h("div", { class: "table-wrap apiref" }, h("table", { class: "dtable" }, h("tbody", {}, routes.map((x) => h("tr", {},
      h("td", {}, h("span", { class: `badge method m-${x.method.toLowerCase()}` }, x.method)),
      h("td", {}, h("code", { dir: "ltr", class: "path" }, x.path)),
      h("td", {}, h("span", { class: `badge ${x.access === "owner" ? "bad" : x.access === "admin" ? "warn" : x.access === "public" ? "good" : ""}` }, t(`acc_${x.access}`)))))))));
  };
  const rows = keys.map((k) => h("tr", {},
    h("td", { class: "name" }, k.name), h("td", {}, roleBadge(k.level)),
    h("td", { class: "muted small" }, ltr(`dk_${k.id}_…`)),
    h("td", { class: "muted small" }, relTime(k.last_used, L)),
    h("td", { class: "small" }, k.expires_unix ? h("span", { class: `badge ${k.expires_unix * 1000 < Date.now() ? "bad" : ""}` }, `${t("keyUntil")} ${fmtDT(k.expires_unix)}`) : null),
    h("td", {}, h("button", { class: "small danger", onclick: async () => {
      if (!await confirmAction(t("keyDelAsk", { x: k.name }))) return;
      await guarded(() => api("DELETE", `/api/v2/admin/apikeys/${encodeURIComponent(k.id)}`)); route();
    } }, t("delete")))));
  return h("div", { class: "card form" },
    h("div", { class: "row spread" }, h("h2", {}, t("keysTitle")), h("div", { class: "row" }, h("button", { onclick: ref }, t("apiRef")), h("button", { class: "primary", onclick: add }, t("addKey")))),
    h("p", { class: "small muted" }, t("keysHint")),
    keys.length ? h("div", { class: "table-wrap" }, h("table", { class: "dtable" }, h("tbody", {}, rows))) : h("p", { class: "muted" }, t("keyNone")));
}

async function viewStaff() {
  const [list, first, keys, wh] = await Promise.all([api("GET", "/api/v2/admin/staff"), api("GET", "/api/v2/admin/audit?limit=100"), api("GET", "/api/v2/admin/apikeys"), api("GET", "/api/v2/admin/webhook")]);
  const roleBadge = (r) => h("span", { class: `badge ${r === "owner" ? "good" : r === "admin" ? "warn" : ""}` }, t(`role_${r}`));
  const L = CT[lang] || CT.en;
  const editLogin = (l) => {
    const user = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: l ? l.username : "", disabled: !!l, dir: "ltr" });
    const role = h("select", {}, ["admin", "viewer", "owner"].map((r) => h("option", { value: r, selected: l ? l.role === r : r === "admin" }, t(`role_${r}`))));
    const pass = h("input", { type: "password", autocomplete: "new-password", minlength: "10", required: !l });
    const err = h("div", { class: "error" });
    const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
      e.preventDefault(); err.textContent = "";
      try { await api("PUT", `/api/v2/admin/staff/${encodeURIComponent(user.value.trim())}`, { Role: role.value, Password: pass.value }); document.getElementById("modal").close(); toast(t("saved")); route(); }
      catch (x) { err.textContent = x.message; }
    } }, t("save"));
    openModal(l ? l.username : t("addLogin"), h("div", { class: "form" }, h("p", { class: "small muted" }, t("staffHint")),
      h("label", {}, h("span", {}, t("username")), user), h("label", {}, h("span", {}, t("role")), role),
      h("label", {}, h("span", {}, l ? t("newPassOpt") : t("password")), pass), err), [save]);
  };
  const rows = list.map((l) => h("tr", {},
    h("td", { class: "name" }, ltr(l.username), l.username === me.user ? h("span", { class: "muted small" }, ` (${t("you")})`) : null),
    h("td", {}, roleBadge(l.role), l.totp ? h("span", { class: "badge good" }, "2FA") : null),
    h("td", { class: "muted small" }, relTime(l.last_seen, L)),
    h("td", { class: "row" },
      h("button", { class: "small", onclick: () => editLogin(l) }, t("edit")),
      l.totp && l.username !== me.user ? h("button", { class: "small", onclick: async () => {
        if (!await confirmAction(t("tfaResetAsk", { x: l.username }), t("tfaReset"))) return;
        await guarded(() => api("DELETE", `/api/v2/admin/staff/${encodeURIComponent(l.username)}/2fa`)); route();
      } }, t("tfaReset")) : null,
      l.username !== me.user ? h("button", { class: "small danger", onclick: async () => {
        if (!await confirmAction(t("delLoginAsk", { x: l.username }))) return;
        await guarded(() => api("DELETE", `/api/v2/admin/staff/${encodeURIComponent(l.username)}`)); route();
      } }, t("delete")) : null)));
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "medium" }).format(new Date(u * 1000));
  const what = (e) => {
    if (e.path === "/api/v2/auth/login") return e.status === 200 ? t("act_login") : e.status === 429 ? (lang === "fa" ? "ورود بسته (رمز اشتباه زیاد)" : "locked out (too many wrong passwords)") : t("act_loginFail");
    return h("code", { dir: "ltr", class: "path" }, `${e.method} ${e.path.replace(/^\/api\/v2\/(admin\/)?/, "")}`);
  };
  const tone = (e) => e.status >= 400 ? "bad" : e.method === "DELETE" ? "warn" : "";
  const body = h("tbody", {});
  let oldest = 0;
  const add = (items) => {
    body.append(...items.map((e) => h("tr", { class: tone(e) },
      h("td", { class: "muted small" }, ltr(fmtTime(e.unix))),
      h("td", { class: "name" }, ltr(e.actor), " ", h("span", { class: "muted small" }, e.role === "login" ? "" : t(`role_${e.role}`) || e.role)),
      h("td", {}, what(e), e.status >= 400 && e.path !== "/api/v2/auth/login" ? h("span", { class: "badge bad" }, `${t("act_denied")} ${e.status}`) : null),
      h("td", { class: "muted small" }, ltr(e.ip)))));
    if (items.length) oldest = items[items.length - 1].id;
    more.hidden = items.length < 100;
  };
  const filter = h("input", { type: "search", placeholder: t("auditFilter") });
  const more = h("button", { onclick: async () => add(await api("GET", `/api/v2/admin/audit?limit=100&before=${oldest}&q=${encodeURIComponent(filter.value.trim())}`)) }, t("auditMore"));
  let timer;
  filter.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(async () => {
    body.replaceChildren(); oldest = 0;
    add(await api("GET", `/api/v2/admin/audit?limit=100&q=${encodeURIComponent(filter.value.trim())}`));
  }, 300); });
  add(first);
  return h("div", { class: "form" },
    h("div", { class: "card form" }, h("div", { class: "row spread" }, h("h1", {}, t("staffTitle")), h("button", { class: "primary", onclick: () => editLogin(null) }, t("addLogin"))),
      h("p", { class: "small muted" }, t("staffHint")),
      h("div", { class: "table-wrap" }, h("table", { class: "dtable" }, h("thead", {}, h("tr", {}, h("th", {}, t("username")), h("th", {}, t("role")), h("th", {}, t("lastSeen")), h("th", {}, ""))), h("tbody", {}, rows)))),
    lockoutsCard(), keysCard(keys, L), webhookCard(wh, L),
    h("div", { class: "card form" }, h("h2", {}, t("auditTitle")), h("p", { class: "small muted" }, t("auditHint")), filter,
      h("div", { class: "table-wrap" }, h("table", { class: "dtable audit" }, body)), more));
}

function panelHTTPSCard() {
  const out = h("div", { class: "form" }, h("p", { class: "muted small" }, "…"));
  const card = h("div", { class: "card form" }, h("h2", {}, t("phTitle")), h("p", { class: "small muted" }, t("phHint")), out);
  (async () => {
    let st;
    try { st = await api("GET", "/api/v2/admin/panel-https"); } catch (e) { fill(out, h("p", { class: "error" }, e.message)); return; }
    const domain = h("input", { value: st.domain || "", placeholder: "panel.example.com", dir: "ltr" });
    const port = h("input", { type: "number", min: "0", max: "65535", value: String(st.port || 0), dir: "ltr" });
    const err = h("div", { class: "error" });
    const info = h("div", {});
    const show = (x) => fill(info,
      x.domain ? h("p", { class: "small" }, x.cert_id ? h("span", { class: "badge good" }, t("phCert", { x: `\u2066${x.cert_id}\u2069`, d: fmtNum(x.days_left) })) : h("span", { class: "badge warn" }, t("phNoCert"))) : null,
      h("p", { class: "small muted" }, x.running ? t("phRunning", { p: ltr(String(x.running)) }) : t("phNotRunning"),
        x.url && x.running === x.port ? h("span", {}, " · ", h("a", { href: x.url, target: "_blank", rel: "noopener", dir: "ltr" }, x.url)) : null));
    show(st);
    const save = async () => { err.textContent = ""; const x = await api("PUT", "/api/v2/admin/panel-https", { domain: domain.value, port: +port.value || 0 }); show(x); return x; };
    fill(out, h("div", { class: "two" }, h("label", {}, h("span", {}, t("phDomain")), domain), h("label", {}, h("span", {}, t("phPort")), port)), info, err,
      h("div", { class: "row" },
        h("button", { onclick: async () => { try { await save(); toast(t("saved")); } catch (x) { err.textContent = x.message; } } }, t("save")),
        h("button", { class: "primary", onclick: async () => {
          try {
            await save();
            if (!await confirmAction(t("phRestartAsk"), t("phRestart"))) return;
            await api("POST", "/api/v2/admin/panel-https/restart");
            toast(t("restoreWait"));
            for (let i = 0; i < 40; i++) {
              await new Promise((r) => setTimeout(r, 2000));
              try { const r = await fetch("/api/v2/auth/me", { credentials: "same-origin" }); if (r.ok) { route(); return; } } catch {}
            }
          } catch (x) { err.textContent = x.message; }
        } }, t("phRestart"))));
  })();
  return card;
}

function trustedCard() {
  const area = h("textarea", { rows: 3, dir: "ltr", placeholder: "10.0.0.0/8\n203.0.113.7" });
  const err = h("div", { class: "error" });
  api("GET", "/api/v2/admin/settings/trusted-ips").then((r) => { area.value = (r.ips || []).join("\n"); }).catch(() => {});
  return h("div", { class: "card form" }, h("h2", {}, t("trustTitle")), h("p", { class: "small muted" }, t("trustHint")), area, err,
    h("button", { class: "primary", onclick: async () => {
      err.textContent = "";
      try { await api("PUT", "/api/v2/admin/settings/trusted-ips", { ips: area.value.split(/\n+/) }); toast(t("saved")); } catch (x) { err.textContent = x.message; }
    } }, t("save")));
}

function tgCommerceCard() {
  const out = h("div", { class: "form" }, h("p", { class: "muted small" }, "…"));
  const card = h("div", { class: "card form" }, h("h2", {}, t("tcTitle")), out);
  Promise.all([api("GET", "/api/v2/admin/tg/commerce"), api("GET", "/api/v2/admin/inbounds")]).then(([c, inbounds]) => {
    const tr = c.trial || {}, rf = c.referral || {};
    const num = (v, attrs = {}) => h("input", { type: "number", min: "0", value: String(v ?? 0), dir: "ltr", ...attrs });
    const trOn = h("input", { type: "checkbox", checked: !!tr.enabled }), trGB = num(tr.gb), trH = num(tr.hours || 24, { min: "1" }), trIPs = num(tr.max_ips ?? 1);
    const trIns = h("div", { class: "chips" }, (inbounds || []).map((i) => h("label", { class: "check" }, h("input", { type: "checkbox", value: i.tag, checked: (tr.inbounds || []).includes(i.tag) }), h("span", {}, i.tag))));
    const rfOn = h("input", { type: "checkbox", checked: !!rf.enabled }), rfPct = num(rf.percent ?? 10, { max: "100" }), rfFix = num(rf.fixed), rfMax = num(rf.max_reward);
    const rfFirst = h("input", { type: "checkbox", checked: !!rf.first_only });
    const err = h("div", { class: "error" });
    const chat = h("input", { inputmode: "numeric", dir: "ltr", placeholder: "123456789" }), delta = h("input", { dir: "ltr", placeholder: "+50" }), wout = h("div", { class: "small" });
    fill(out,
      h("h3", {}, t("tcTrial")), h("label", { class: "check" }, trOn, h("span", {}, t("tcTrialOn"))),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcGB")), trGB), h("label", {}, h("span", {}, t("tcHours")), trH)),
      h("label", {}, h("span", {}, t("maxIPs")), trIPs), trIns,
      h("h3", {}, t("tcRef")), h("label", { class: "check" }, rfOn, h("span", {}, t("tcRefOn"))),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcPercent")), rfPct), h("label", {}, h("span", {}, t("tcFixed")), rfFix)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcMax")), rfMax), h("label", { class: "check" }, rfFirst, h("span", {}, t("tcFirst")))), err,
      h("button", { class: "primary", onclick: async () => {
        err.textContent = "";
        try {
          await api("PUT", "/api/v2/admin/tg/trial", { enabled: trOn.checked, gb: +trGB.value || 0, hours: +trH.value || 24, max_ips: +trIPs.value || 0, inbounds: [...trIns.querySelectorAll("input:checked")].map((x) => x.value) });
          await api("PUT", "/api/v2/admin/tg/referral", { enabled: rfOn.checked, percent: +rfPct.value || 0, fixed: +rfFix.value || 0, max_reward: +rfMax.value || 0, first_only: rfFirst.checked });
          toast(t("saved"));
        } catch (x) { err.textContent = x.message; }
      } }, t("save")),
      h("h3", {}, t("tcWallet")),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcChat")), chat), h("label", {}, h("span", {}, t("tcDelta")), delta)), wout,
      h("div", { class: "row" },
        h("button", { onclick: async () => { try { const r = await api("GET", `/api/v2/admin/tg/wallet/${encodeURIComponent(chat.value.trim())}`); fill(wout, t("tcBalance", { n: fmtNum(r.balance) })); } catch (x) { fill(wout, x.message); } } }, t("tcLookup")),
        h("button", { onclick: async () => { try { const r = await api("POST", `/api/v2/admin/tg/wallet/${encodeURIComponent(chat.value.trim())}`, { delta: parseInt(delta.value, 10) || 0, note: "panel" }); fill(wout, t("tcBalance", { n: fmtNum(r.balance) })); toast(t("saved")); } catch (x) { fill(wout, x.message); } } }, t("tcApply"))));
  }).catch((e) => fill(out, h("p", { class: "error" }, e.message)));
  return card;
}

// ---------- Move from 3x-ui / Marzban / PasarGuard, secret panel address ----------
const S4 = {
  fa: { fTitle: "انتقال از 3x-ui، Marzban یا PasarGuard", fHint: "فایل دیتابیس پنل قبلی را بدهید: در 3x-ui فایل \u2066/etc/x-ui/x-ui.db\u2069، در Marzban فایل \u2066/var/lib/marzban/db.sqlite3\u2069 و در PasarGuard فایل \u2066/var/lib/pasarguard/db.sqlite3\u2069. کاربرها با همان UUID و رمز Trojan، حجم، انقضا، محدودیت IP، مصرف و آیدی تلگرام منتقل می‌شوند و اینباندهای 3x-ui و PasarGuard روی نودی که انتخاب می‌کنید ساخته می‌شوند. لینک‌های اشتراک قبلی هم جواب می‌دهند، به شرطی که دامنهٔ قبلی را به این سرور وصل کنید. فایل فقط خوانده می‌شود. اول پیش‌نمایش بگیرید.",
    fFile: "فایل دیتابیس", fNode: "نود اینباندهای منتقل‌شده", fAddr: "آدرسی که کاربرها وصل می‌شوند (خالی = IP نود)", fExtra: "این اینباندهای موجود هم به کاربرهای منتقل‌شده داده شود",
    fWould: "پیش‌نمایش ({k}) — هنوز چیزی نوشته نشده:", fGot: "منتقل شد ({k}):", f_inbounds: "اینباند", f_clients: "کاربر", f_disabled: "غیرفعال", f_usage: "با مصرف", f_reps: "نماینده", f_logins: "ورود پنل", f_links: "لینک قبلی حفظ‌شده",
    fAsk: "همین حالا منتقل شود؟ نام‌های موجود رد می‌شوند و هرگز بازنویسی نمی‌شوند.", fNeedFile: "اول فایل دیتابیس را انتخاب کنید.",
    ppTitle: "آدرس مخفی پنل", ppHint: "با آدرس مخفی، صفحهٔ ورود و پنل فقط از آدرس مخفی باز می‌شوند و بقیه خطای ۴۰۴ می‌بینند، پس اسکنرها و حدس‌زن‌ها اصلاً صفحهٔ ورود را پیدا نمی‌کنند. هر مرورگر فقط یک بار باید این آدرس را باز کند. اسکریپت‌ها و کلیدهای API، لینک‌های اشتراک و نصب نود تغییری نمی‌کنند. آدرس برای مالک‌ها در تلگرام هم فرستاده می‌شود. اگر گمش کردید، دستور \u2066dark-hub2 admin\u2069 روی سرور آن را پاک می‌کند.",
    ppPath: "مسیر مخفی (۶ تا ۶۴ حرف، عدد، - یا _)", ppRandom: "ساخت تصادفی", ppSave: "ذخیره", ppOff: "خاموش کردن", ppNow: "آدرس پنل: {x}", ppNone: "خاموش — پنل از ریشهٔ آدرس باز می‌شود.", ppCopy: "کپی آدرس", ppAsk: "آدرس مخفی ذخیره شود؟ این مرورگر باز می‌ماند؛ بقیهٔ دستگاه‌ها باید یک بار آدرس جدید را باز کنند." },
  en: { fTitle: "Move from 3x-ui, Marzban or PasarGuard", fHint: "Give the old panel's database file: /etc/x-ui/x-ui.db for 3x-ui, /var/lib/marzban/db.sqlite3 for Marzban, /var/lib/pasarguard/db.sqlite3 for PasarGuard. Clients keep their UUID and Trojan password, volume, expiry, IP limit, usage and Telegram id; 3x-ui and PasarGuard inbounds are created on the node you pick. Old subscription links keep answering once the old domain points here. The file is only read — preview first.",
    fFile: "Database file", fNode: "Node for the moved inbounds", fAddr: "Address clients connect to (empty = the node's IP)", fExtra: "Also give the moved clients these existing inbounds",
    fWould: "Preview ({k}) — nothing written yet:", fGot: "Moved ({k}):", f_inbounds: "inbounds", f_clients: "clients", f_disabled: "disabled", f_usage: "with usage", f_reps: "resellers", f_logins: "logins", f_links: "old links kept",
    fAsk: "Move it into this panel now? Existing names are skipped, never overwritten.", fNeedFile: "Pick the database file first.",
    ppTitle: "Secret panel address", ppHint: "With a secret address the sign-in page and the panel only open from it; everyone else gets 404, so scanners and guessers never find a login page. Each browser opens the address once. Scripts and API keys, subscription links and node installs are not affected. The address is also sent to the owners on Telegram; if you lose it, run dark-hub2 admin on the server to clear it.",
    ppPath: "Secret path (6–64 letters, digits, - or _)", ppRandom: "Random", ppSave: "Save", ppOff: "Turn off", ppNow: "Panel address: {x}", ppNone: "Off — the panel opens at the root address.", ppCopy: "Copy address", ppAsk: "Save the secret address? This browser stays in; other devices open the new address once." },
};
const s4 = (k, vars) => { let s = (S4[lang] || S4.en)[k] ?? S4.en[k] ?? k; for (const [a, b] of Object.entries(vars || {})) s = s.replaceAll(`{${a}}`, b); return s; };

function foreignCard() {
  const out = h("div", { class: "form" });
  const file = h("input", { type: "file", accept: ".db,.sqlite,.sqlite3,application/octet-stream" });
  const node = h("select", {}, h("option", { value: "" }, "—"));
  const addr = h("input", { dir: "ltr", placeholder: "vpn.example.com" });
  const ins = h("div", { class: "chips" }, h("span", { class: "muted small" }, "…"));
  api("GET", "/api/v2/admin/nodes").then((list) => fill(node, (list || []).map((n, i) => h("option", { value: n.ID, selected: i === 0 }, `${n.Name || n.ID}${n.PublicIP ? ` · ${n.PublicIP}` : ""}`)))).catch(() => {});
  api("GET", "/api/v2/admin/inbounds").then((list) => fill(ins, (list || []).map((i) => h("label", { class: "check" }, h("input", { type: "checkbox", value: i.tag }), h("span", {}, i.tag))))).catch(() => {});
  const show = (r, dry) => {
    const keys = ["clients", "disabled", "usage", "reps", "logins", "links"];
    fill(out, h("p", { class: "small" }, h("b", {}, s4(dry ? "fWould" : "fGot", { k: r.kind }))),
      h("div", { class: "chips" }, h("span", { class: `badge ${r.inbounds.length ? "good" : ""}` }, `${fmtNum(r.inbounds.length)} ${s4("f_inbounds")}`),
        keys.map((k) => h("span", { class: `badge ${r[k] ? "good" : ""}` }, `${fmtNum(r[k] || 0)} ${s4("f_" + k)}`))),
      r.inbounds.length ? h("div", { class: "small muted", dir: "ltr" }, r.inbounds.join("  ")) : null,
      r.skipped?.length ? h("details", {}, h("summary", { class: "small" }, `${t("migSkipped")} (${fmtNum(r.skipped.length)})`), h("ul", { class: "small muted", dir: "ltr" }, r.skipped.map((x) => h("li", {}, x)))) : null,
      r.notes?.length ? h("details", { open: true }, h("summary", { class: "small" }, `${t("migNotes")} (${fmtNum(r.notes.length)})`), h("ul", { class: "small muted", dir: "ltr" }, r.notes.map((x) => h("li", {}, x)))) : null);
  };
  const run = async (dry, btn) => {
    if (!file.files[0]) { fill(out, h("p", { class: "error" }, s4("fNeedFile"))); return; }
    if (!dry && !await confirmAction(s4("fAsk"), t("migImport"))) return;
    btn.disabled = true; fill(out, h("p", { class: "muted small" }, t("migWorking")));
    const q = new URLSearchParams({ dry_run: dry ? "1" : "0", node: node.value, address: addr.value.trim(),
      inbounds: [...ins.querySelectorAll("input:checked")].map((x) => x.value).join(",") });
    try {
      const res = await fetch(`/api/v2/admin/migrate/foreign?${q}`, { method: "POST", credentials: "same-origin", body: file.files[0], headers: { "X-Dark-Request": "1", "Content-Type": "application/octet-stream" } });
      const text = await res.text();
      if (!res.ok) throw new Error(text.trim() || res.statusText);
      show(JSON.parse(text), dry);
      if (!dry) toast(t("migDone"));
    } catch (x) { fill(out, h("p", { class: "error" }, x.message)); }
    finally { btn.disabled = false; }
  };
  const preview = h("button", { onclick: () => run(true, preview) }, t("migPreview"));
  const imp = h("button", { class: "primary", onclick: () => run(false, imp) }, t("migImport"));
  return h("div", { class: "card form" }, h("h2", {}, s4("fTitle")), h("p", { class: "small muted" }, s4("fHint")),
    h("label", {}, h("span", {}, s4("fFile")), file),
    h("div", { class: "two" }, h("label", {}, h("span", {}, s4("fNode")), node), h("label", {}, h("span", {}, s4("fAddr")), addr)),
    h("div", { class: "small muted" }, s4("fExtra")), ins,
    h("div", { class: "row" }, preview, imp), out);
}

function panelPathCard() {
  const card = h("div", { class: "card form" });
  const draw = (cur) => {
    const input = h("input", { dir: "ltr", value: cur || "", placeholder: "my-door-82kq", pattern: "[A-Za-z0-9_-]{6,64}" });
    const url = cur ? `${location.origin}/${cur}/` : "";
    const err = h("div", { class: "error" });
    const save = async (v) => {
      err.textContent = "";
      if (v && !(await confirmAction(s4("ppAsk"), s4("ppSave")))) return;
      try { const r = await api("PUT", "/api/v2/admin/panel-path", { path: v }); toast(t("saved")); route(); return r; } catch (x) { err.textContent = x.message; }
    };
    fill(card, h("h2", {}, s4("ppTitle")), h("p", { class: "small muted" }, s4("ppHint")),
      h("p", { class: "small" }, cur ? h("span", {}, s4("ppNow", { x: "" }), h("code", { dir: "ltr" }, url)) : h("span", { class: "muted" }, s4("ppNone"))),
      h("label", {}, h("span", {}, s4("ppPath")), h("div", { class: "row tight" }, input,
        h("button", { type: "button", class: "ghost", onclick: () => { const b = new Uint8Array(9); crypto.getRandomValues(b); input.value = btoa(String.fromCharCode(...b)).replace(/[+/=]/g, "").toLowerCase(); } }, s4("ppRandom")))),
      err,
      h("div", { class: "row" }, h("button", { class: "primary", onclick: () => { if (input.reportValidity()) save(input.value.trim()); } }, s4("ppSave")),
        cur ? h("button", { onclick: () => copy(url) }, s4("ppCopy")) : null,
        cur ? h("button", { class: "ghost danger spacer", onclick: () => save("") }, s4("ppOff")) : null));
  };
  api("GET", "/api/v2/admin/panel-path").then((r) => draw(r.path)).catch((x) => fill(card, h("p", { class: "error" }, x.message)));
  return card;
}

function migrateCard() {
  const out = h("div", { class: "form" });
  const srcServer = h("input", { type: "radio", name: "migsrc", checked: true });
  const srcUpload = h("input", { type: "radio", name: "migsrc" });
  const path = h("input", { value: "/var/lib/dark-xray-pro-lab/review/business.sqlite", dir: "ltr" });
  const file = h("input", { type: "file", accept: ".sqlite,.db,application/octet-stream" });
  const archived = h("input", { type: "checkbox" });
  const ins = h("div", { class: "chips" }, h("span", { class: "muted small" }, "…"));
  api("GET", "/api/v2/admin/inbounds").then((list) => fill(ins, (list || []).map((i) => h("label", { class: "check" }, h("input", { type: "checkbox", value: i.tag }), h("span", {}, i.tag))))).catch(() => {});
  const show = (r, dry) => {
    const keys = ["reps", "sellers", "clients", "disabled", "usage", "groups", "logins", "links"];
    fill(out, h("p", { class: "small" }, h("b", {}, dry ? t("migWould") : t("migGot"))),
      h("div", { class: "chips" }, keys.map((k) => h("span", { class: `badge ${r[k] ? "good" : ""}` }, `${fmtNum(r[k] || 0)} ${t("mig_" + k)}`))),
      r.skipped && r.skipped.length ? h("details", {}, h("summary", { class: "small" }, `${t("migSkipped")} (${fmtNum(r.skipped.length)})`), h("ul", { class: "small muted", dir: "ltr" }, r.skipped.map((x) => h("li", {}, x)))) : null,
      r.notes && r.notes.length ? h("details", {}, h("summary", { class: "small" }, `${t("migNotes")} (${fmtNum(r.notes.length)})`), h("ul", { class: "small muted", dir: "ltr" }, r.notes.map((x) => h("li", {}, x)))) : null);
  };
  const run = async (dry, btn) => {
    if (!dry && !await confirmAction(t("migAsk"), t("migImport"))) return;
    btn.disabled = true; fill(out, h("p", { class: "muted small" }, t("migWorking")));
    const q = new URLSearchParams({ dry_run: dry ? "1" : "0", archived: archived.checked ? "1" : "0",
      inbounds: [...ins.querySelectorAll("input:checked")].map((x) => x.value).join(",") });
    try {
      let res;
      if (srcUpload.checked) {
        if (!file.files[0]) throw new Error(t("migUpload"));
        res = await fetch(`/api/v2/admin/migrate?${q}`, { method: "POST", credentials: "same-origin", body: file.files[0], headers: { "X-Dark-Request": "1", "Content-Type": "application/octet-stream" } });
      } else {
        q.set("path", path.value.trim());
        res = await fetch(`/api/v2/admin/migrate?${q}`, { method: "POST", credentials: "same-origin", headers: { "X-Dark-Request": "1" } });
      }
      const text = await res.text();
      if (!res.ok) throw new Error(text.trim() || res.statusText);
      show(JSON.parse(text), dry);
      if (!dry) toast(t("migDone"));
    } catch (x) { fill(out, h("p", { class: "error" }, x.message)); }
    finally { btn.disabled = false; }
  };
  const preview = h("button", { onclick: () => run(true, preview) }, t("migPreview"));
  const imp = h("button", { class: "primary", onclick: () => run(false, imp) }, t("migImport"));
  return h("div", { class: "card form" }, h("h2", {}, t("migTitle")), h("p", { class: "small muted" }, t("migHint")),
    h("fieldset", {}, h("legend", {}, t("migSrc")),
      h("label", { class: "check" }, srcServer, h("span", {}, t("migServer"))), h("label", {}, h("span", {}, t("migPath")), path),
      h("label", { class: "check" }, srcUpload, h("span", {}, t("migUpload"))), file),
    h("div", { class: "small muted" }, t("migInbounds")), ins,
    h("label", { class: "check" }, archived, h("span", {}, t("migArchived"))),
    h("div", { class: "row" }, preview, imp), out);
}

function restoreSection() {
  const file = h("input", { type: "file", accept: ".gz,.dkb,application/gzip,application/octet-stream", required: true });
  const pass = h("input", { type: "password", autocomplete: "off" });
  const out = h("div", { class: "form" });
  const check = h("button", { type: "submit" }, t("restoreCheck"));
  const form = h("form", { class: "form", onsubmit: async (e) => {
    e.preventDefault();
    const f = file.files[0];
    if (!f) return;
    check.disabled = true; fill(out, h("p", { class: "muted small" }, t("restoreChecking")));
    try {
      const res = await fetch("/api/v2/admin/restore", { method: "POST", credentials: "same-origin", body: f,
        headers: { "X-Dark-Request": "1", "Content-Type": "application/octet-stream", "X-Backup-Password": pass.value } });
      const text = await res.text();
      if (!res.ok) throw new Error(text.trim() || res.statusText);
      const p = JSON.parse(text);
      fill(out, h("p", {}, h("span", { class: "badge good" }, "✓"), " ", t("restoreFound", { c: fmtNum(p.clients), n: fmtNum(p.nodes), i: fmtNum(p.inbounds), l: fmtNum(p.logins) })),
        h("div", { class: "row" }, h("button", { class: "danger", type: "button", onclick: async () => {
          if (!await confirmAction(t("restoreAsk"), t("restoreApply"))) return;
          await api("POST", "/api/v2/admin/restore/apply");
          fill(out, h("p", { class: "small" }, t("restoreWait")));
          for (let i = 0; i < 60; i++) {
            await new Promise((r) => setTimeout(r, 2000));
            try { const r = await fetch("/api/v2/auth/me", { credentials: "same-origin" }); if (r.status === 200 || r.status === 401) { location.hash = "#/login"; location.reload(); return; } } catch {}
          }
        } }, t("restoreApply")), h("button", { type: "button", onclick: async () => { await api("DELETE", "/api/v2/admin/restore"); fill(out); form.reset(); } }, t("cancel"))));
    } catch (x) { fill(out, h("p", { class: "error" }, x.message)); }
    finally { check.disabled = false; }
  } }, h("label", {}, h("span", {}, t("restoreFile")), file), h("label", {}, h("span", {}, t("restorePass")), pass), check);
  return h("div", { class: "form" }, h("h2", { class: "section" }, t("restoreTitle")), h("p", { class: "small muted" }, t("restoreHint")), form, out);
}

// ---------- Telegram bot, backups to Telegram, sessions, login protection ----------
const S2 = {
  fa: { botTitle: "ربات تلگرام", botHint: "ربات را از @BotFather بسازید، توکنش را این‌جا بگذارید و شناسهٔ عددی تلگرام مالک‌ها را اضافه کنید (شناسه را از @userinfobot بگیرید). توکن فقط در یک فایل محافظت‌شده روی هاب می‌ماند و در بکاپ‌ها نیست.",
    botOn: "وصل است", botOff: "وصل نیست", botToken: "توکن ربات", botTokenKeep: "ذخیره شده ({x}) — خالی یعنی بدون تغییر", botOwners: "مالک‌ها (شناسهٔ عددی، هر خط یکی)",
    botSave: "ذخیره و اتصال", botTest: "پیام آزمایشی", botTestOK: "پیام آزمایشی برای مالک‌ها فرستاده شد", botOffBtn: "قطع ربات", botOffAsk: "ربات قطع و توکنش از هاب پاک شود؟ فروشگاه تلگرام و هشدارها تا اتصال دوباره کار نمی‌کنند.",
    botSaved: "ربات وصل شد: @{x}", botNA: "این هاب بدون پشتیبانی ربات اجرا شده.",
    bkTitle: "بکاپ خودکار به تلگرام", bkHint: "بکاپ کامل هاب با رمز شما رمزگذاری می‌شود و برای مالک‌ها در تلگرام فرستاده می‌شود. بدون این رمز بکاپ باز نمی‌شود؛ جایی امن نگهش دارید.",
    bkPass: "رمز بکاپ", bkPassSet: "ذخیره شده — خالی یعنی بدون تغییر، - یعنی حذف", bkEvery: "هر چند وقت", bkOff: "خاموش", bkH: "هر {n} ساعت", bkDay: "روزانه", bkLast: "آخرین ارسال: {d}",
    bkNever: "هنوز ارسال نشده", bkNow: "ارسال بکاپ الان", bkSent: "بکاپ برای مالک‌ها فرستاده شد", bkErr: "خطای آخرین ارسال: {x}",
    sessTitle: "نشست‌های فعال", sessHint: "دستگاه‌هایی که با ورود شما وارد پنل هستند. هر کدام را نمی‌شناسید ببندید و رمز را عوض کنید.", sessThis: "همین دستگاه",
    sessEnd: "خروج", sessOthers: "خروج از همهٔ دستگاه‌های دیگر", sessOthersAsk: "همهٔ نشست‌های دیگر شما بسته شود؟", sessEnded: "{n} نشست بسته شد", sessAll: "همهٔ ورودها",
    sessSeen: "آخرین فعالیت", sessFrom: "از",
    lockTitle: "امنیت ورود", lockHint: "بعد از ۵ رمز اشتباه برای یک نام ورود، یا ۱۰ رمز اشتباه از یک آدرس در ۱۵ دقیقه، آن آدرس موقتاً بسته می‌شود (بار اول ۱۵ دقیقه، هر بار دو برابر تا ۲۴ ساعت). نام ورود برای بقیهٔ آدرس‌ها باز می‌ماند تا کسی نتواند شما را بیرون نگه دارد. با ربات وصل، به مالک‌ها هشدار داده می‌شود.",
    lockNone: "الان هیچ آدرسی بسته نیست.", lockUntil: "تا {d}", lockOpen: "باز کردن", lockOpenAll: "باز کردن همه", lockUser: "نام ورود: {x}", lockStrikes: "بار {n}" },
  en: { botTitle: "Telegram bot", botHint: "Create the bot with @BotFather, paste its token here and add the owners' numeric Telegram ids (get yours from @userinfobot). The token stays in a protected file on the HUB and is never in backups.",
    botOn: "Connected", botOff: "Not connected", botToken: "Bot token", botTokenKeep: "saved ({x}) — empty keeps it", botOwners: "Owners (numeric ids, one per line)",
    botSave: "Save and connect", botTest: "Send a test", botTestOK: "Test message sent to the owners", botOffBtn: "Disconnect", botOffAsk: "Disconnect the bot and delete its token from the HUB? The Telegram store and alerts stop until it is connected again.",
    botSaved: "Bot connected: @{x}", botNA: "This HUB runs without bot support.",
    bkTitle: "Automatic backups to Telegram", bkHint: "A full HUB backup, encrypted with your password, is sent to the owners on Telegram. Without the password a backup cannot be opened — keep it somewhere safe.",
    bkPass: "Backup password", bkPassSet: "saved — empty keeps it, - removes it", bkEvery: "How often", bkOff: "Off", bkH: "Every {n} hours", bkDay: "Daily", bkLast: "Last sent: {d}",
    bkNever: "Not sent yet", bkNow: "Send a backup now", bkSent: "Backup sent to the owners", bkErr: "Last send failed: {x}",
    sessTitle: "Active sessions", sessHint: "Devices signed in with your login. End any you don't recognise and change your password.", sessThis: "This device",
    sessEnd: "Sign out", sessOthers: "Sign out every other device", sessOthersAsk: "End all your other sessions?", sessEnded: "{n} sessions ended", sessAll: "All logins",
    sessSeen: "Last active", sessFrom: "from",
    lockTitle: "Login protection", lockHint: "After 5 wrong passwords for one login, or 10 from one address within 15 minutes, that address is locked for a while (15 minutes the first time, doubling up to 24 hours). The login stays open from other addresses, so nobody can keep you out. With the bot connected, owners get an alert.",
    lockNone: "No address is locked right now.", lockUntil: "until {d}", lockOpen: "Unlock", lockOpenAll: "Unlock all", lockUser: "login: {x}", lockStrikes: "time {n}" },
};
const s2 = (k, vars) => { let s = (S2[lang] || S2.en)[k] ?? S2.en[k] ?? k; for (const [a, b] of Object.entries(vars || {})) s = s.replaceAll(`{${a}}`, b); return s; };
const when = (u) => new Date(u * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" });
const shortAgent = (a) => {
  if (!a) return "—";
  const os = /Android/.test(a) ? "Android" : /iPhone|iPad/.test(a) ? "iOS" : /Windows/.test(a) ? "Windows" : /Mac OS/.test(a) ? "macOS" : /Linux/.test(a) ? "Linux" : "";
  const br = /Edg\//.test(a) ? "Edge" : /Firefox\//.test(a) ? "Firefox" : /Chrome\//.test(a) ? "Chrome" : /Safari\//.test(a) ? "Safari" : /curl|Go-http|python/i.test(a) ? "script" : "";
  return [br, os].filter(Boolean).join(" · ") || a.slice(0, 40);
};

function telegramBotCard() {
  const card = h("div", { class: "card form" }, h("h2", {}, s2("botTitle")), h("p", { class: "muted small" }, "…"));
  const draw = (st) => {
    if (!st.available) { card.replaceChildren(h("h2", {}, s2("botTitle")), h("p", { class: "muted small" }, s2("botNA"))); return; }
    const tok = h("input", { type: "password", autocomplete: "off", dir: "ltr", placeholder: st.token_set ? s2("botTokenKeep", { x: st.token_hint || "…" }) : "123456789:AA…" });
    const owners = h("textarea", { rows: "2", dir: "ltr", inputmode: "numeric", placeholder: "1906987468" }, (st.owners || []).join("\n"));
    const err = h("div", { class: "error" });
    const save = h("button", { class: "primary", onclick: async () => {
      err.textContent = "";
      const ids = owners.value.split(/[\s,]+/).map((x) => parseInt(x, 10)).filter((x) => x > 0);
      try { const r = await api("PUT", "/api/v2/admin/telegram", { token: tok.value.trim(), owners: ids }); toast(s2("botSaved", { x: r.username || "?" })); draw(r); }
      catch (x) { err.textContent = x.message; }
    } }, s2("botSave"));
    const pass = h("input", { type: "password", autocomplete: "new-password", placeholder: st.backup_pass ? s2("bkPassSet") : "", minlength: "8" });
    const every = h("select", {}, [[0, s2("bkOff")], [6, s2("bkH", { n: fmtNum(6) })], [12, s2("bkH", { n: fmtNum(12) })], [24, s2("bkDay")], [48, s2("bkH", { n: fmtNum(48) })]]
      .map(([v, l]) => h("option", { value: String(v), selected: v === st.backup_hours }, l)));
    const bkErr = h("div", { class: "error" });
    fill(card,
      h("div", { class: "row" }, h("h2", {}, s2("botTitle")), h("span", { class: `badge ${st.running ? "good" : "bad"}` }, st.running ? s2("botOn") : s2("botOff")),
        st.running && st.username ? h("a", { class: "badge", dir: "ltr", href: `https://t.me/${st.username}`, target: "_blank", rel: "noopener" }, `@${st.username}`) : null),
      h("p", { class: "small muted" }, s2("botHint")),
      h("label", {}, h("span", {}, s2("botToken")), tok), h("label", {}, h("span", {}, s2("botOwners")), owners), err,
      h("div", { class: "row" }, save,
        st.running ? h("button", { onclick: async () => { try { await api("POST", "/api/v2/admin/telegram/test"); toast(s2("botTestOK")); } catch (x) { err.textContent = x.message; } } }, s2("botTest")) : null,
        st.token_set ? h("button", { class: "ghost danger spacer", onclick: async () => {
          if (!(await confirmAction(s2("botOffAsk"), s2("botOffBtn")))) return;
          try { await api("PUT", "/api/v2/admin/telegram", { token: "-", owners: [] }); route(); } catch (x) { toast(t("failed", { e: x.message })); }
        } }, s2("botOffBtn")) : null),
      noticesBox(),
      h("h2", { class: "section" }, s2("bkTitle")), h("p", { class: "small muted" }, s2("bkHint")),
      h("div", { class: "two" }, h("label", {}, h("span", {}, s2("bkPass")), pass), h("label", {}, h("span", {}, s2("bkEvery")), every)),
      h("p", { class: "small muted" }, st.backup_last ? s2("bkLast", { d: when(st.backup_last) }) : s2("bkNever")),
      st.backup_error ? h("p", { class: "small error" }, s2("bkErr", { x: st.backup_error })) : null, bkErr,
      h("div", { class: "row" },
        h("button", { onclick: async () => {
          bkErr.textContent = "";
          try { const r = await api("PUT", "/api/v2/admin/telegram/backup", { pass: pass.value, hours: +every.value }); toast(t("saved")); draw(r); } catch (x) { bkErr.textContent = x.message; }
        } }, t("save")),
        h("button", { class: "ghost", disabled: !st.running || !st.backup_pass, onclick: async (e) => {
          bkErr.textContent = ""; e.target.disabled = true;
          try { await api("POST", "/api/v2/admin/telegram/backup-now"); toast(s2("bkSent")); draw(await api("GET", "/api/v2/admin/telegram")); } catch (x) { bkErr.textContent = x.message; e.target.disabled = false; }
        } }, s2("bkNow"))));
  };
  api("GET", "/api/v2/admin/telegram").then(draw).catch((x) => card.replaceChildren(h("h2", {}, s2("botTitle")), h("p", { class: "error" }, x.message)));
  return card;
}

// When the bot warns customers: volume percent used, days left.
function noticesBox() {
  const fa = lang === "fa";
  const pct = h("input", { type: "number", min: "50", max: "99", dir: "ltr" });
  const days = h("input", { type: "number", min: "1", max: "14", dir: "ltr" });
  api("GET", "/api/v2/admin/telegram/notices").then((r) => { pct.value = r.percent; days.value = r.days; }).catch(() => {});
  return h("div", { class: "form" }, h("h2", { class: "section" }, fa ? "هشدار به مشتری" : "Customer notices"),
    h("p", { class: "small muted" }, fa ? "ربات به مشتری‌هایی که تلگرامشان وصل است خبر می‌دهد: اولین اتصال، نزدیک شدن به پایان حجم و زمان، و تمام شدن. هر هشدار برای هر دورهٔ سرویس یک بار فرستاده می‌شود." : "The bot tells customers whose Telegram is linked: first connection, volume and time running out, and when they end. Each notice goes once per service term."),
    h("div", { class: "two" }, h("label", {}, h("span", {}, fa ? "هشدار حجم بعد از مصرف (درصد)" : "Volume notice after (percent used)"), pct),
      h("label", {}, h("span", {}, fa ? "هشدار زمان وقتی این تعداد روز مانده" : "Time notice when this many days are left"), days)),
    h("div", { class: "row" }, h("button", { onclick: async () => { try { await api("PUT", "/api/v2/admin/telegram/notices", { percent: +pct.value, days: +days.value }); toast(t("saved")); } catch (x) { toast(t("failed", { e: x.message })); } } }, t("save"))));
}

function sessionsCard() {
  const list = h("div", { class: "cfglist" });
  let all = false;
  const load = async () => {
    try {
      const rows = await api("GET", `/api/v2/auth/sessions${all ? "?all=1" : ""}`);
      list.replaceChildren(...rows.map((x) => h("div", { class: "cfg sess" },
        h("span", { class: `dot ${x.current ? "on" : ""}` }),
        h("div", { class: "cfgname" }, h("div", {}, shortAgent(x.agent), all ? h("span", { class: "badge" }, x.user) : null, x.current ? h("span", { class: "badge good" }, s2("sessThis")) : null),
          h("div", { class: "small muted", dir: "auto" }, `${x.ip || "?"} · ${s2("sessSeen")}: ${when(x.last_seen || x.created || 0)}`)),
        x.current ? null : h("button", { type: "button", class: "ghost danger", onclick: async () => { try { await api("DELETE", `/api/v2/auth/sessions/${x.id}`); load(); } catch (e) { toast(t("failed", { e: e.message })); } } }, s2("sessEnd")))));
    } catch (x) { list.replaceChildren(h("p", { class: "error" }, x.message)); }
  };
  load();
  return h("div", { class: "card form" }, h("div", { class: "row" }, h("h2", {}, s2("sessTitle")),
      fullOwner() ? h("label", { class: "check spacer" }, (() => { const c = h("input", { type: "checkbox" }); c.addEventListener("change", () => { all = c.checked; load(); }); return c; })(), s2("sessAll")) : null),
    h("p", { class: "small muted" }, s2("sessHint")), list,
    h("div", { class: "row" }, h("button", { class: "ghost danger", onclick: async () => {
      if (!(await confirmAction(s2("sessOthersAsk"), s2("sessOthers")))) return;
      try { const r = await api("POST", "/api/v2/auth/sessions/end-others"); toast(s2("sessEnded", { n: fmtNum(r.ended) })); route(); } catch (x) { toast(t("failed", { e: x.message })); }
    } }, s2("sessOthers"))));
}

function lockoutsCard() {
  const list = h("div", { class: "cfglist" });
  const load = async () => {
    try {
      const rows = await api("GET", "/api/v2/admin/security/lockouts");
      if (!rows.length) { list.replaceChildren(h("p", { class: "muted small" }, s2("lockNone"))); return; }
      list.replaceChildren(...rows.map((x) => h("div", { class: "cfg" },
        h("span", { class: "badge bad", dir: "ltr" }, x.ip),
        h("span", { class: "cfgname small" }, [x.user ? s2("lockUser", { x: x.user }) : null, s2("lockUntil", { d: when(x.until) }), s2("lockStrikes", { n: fmtNum(x.strikes) })].filter(Boolean).join(" · ")),
        h("button", { type: "button", class: "ghost", onclick: async () => { await api("DELETE", `/api/v2/admin/security/lockouts?ip=${encodeURIComponent(x.ip)}`); load(); } }, s2("lockOpen")))),
        h("div", { class: "row" }, h("button", { class: "ghost", onclick: async () => { await api("DELETE", "/api/v2/admin/security/lockouts"); load(); } }, s2("lockOpenAll"))));
    } catch (x) { list.replaceChildren(h("p", { class: "error" }, x.message)); }
  };
  load();
  return h("div", { class: "card form" }, h("h2", {}, s2("lockTitle")), h("p", { class: "small muted" }, s2("lockHint")), list);
}

function viewSettings() {
  const langSel = h("select", { onchange: (e) => { lang = e.target.value; safeSet("lang", lang); applyLang(); route(); } },
    h("option", { value: "en", selected: lang === "en" }, "English"), h("option", { value: "fa", selected: lang === "fa" }, "فارسی"));
  const cur = safeGet("color-scheme") || "light dark";
  const themeSel = h("select", { onchange: (e) => {
    const v = e.target.value;
    safeSet("color-scheme", v === "light dark" ? null : v);
    document.querySelector('meta[name="color-scheme"]').content = v;
  } }, [["light dark", t("system")], ["light", t("light")], ["dark", t("dark")]].map(([v, l]) => h("option", { value: v, selected: v === cur }, l)));
  const oldP = h("input", { type: "password", autocomplete: "current-password", required: true });
  const newP = h("input", { type: "password", autocomplete: "new-password", required: true, minlength: "10" });
  const pwErr = h("div", { class: "error" });
  const pwForm = h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault(); pwErr.textContent = "";
    try { await api("POST", "/api/v2/auth/password", { Old: oldP.value, New: newP.value }); toast(t("saved")); location.hash = "#/login"; }
    catch (x) { pwErr.textContent = x.message; }
  } }, h("h2", {}, t("changePassword")), h("label", {}, h("span", {}, t("oldPassword")), oldP), h("label", {}, h("span", {}, t("newPassword")), newP), pwErr,
    h("button", { class: "primary", type: "submit" }, t("save")));
  const importIn = h("input", { type: "file", accept: "application/json,.json", hidden: true, onchange: async (e) => {
    const f = e.target.files[0];
    if (!f) return;
    try {
      const recs = JSON.parse(await f.text());
      const r = await api("POST", "/api/v2/admin/clients/import", recs);
      toast(t("imported", { n: fmtNum(r.imported) }));
    } catch (x) { toast(t("failed", { e: x.message })); }
    e.target.value = "";
  } });
  const backupCard = fullOwner() ? h("div", { class: "card form" }, h("h2", {}, t("backup")), h("p", { class: "small muted" }, t("backupHint")),
    h("div", { class: "row" }, h("a", { class: "btn", href: "/api/v2/admin/backup", download: "" }, t("download"))),
    h("h2", { class: "section" }, t("exportImport")), h("p", { class: "small muted" }, t("exportHint")),
    h("div", { class: "row" }, h("a", { class: "btn", href: "/api/v2/admin/clients/export", download: "" }, t("exportClients")),
      h("button", { onclick: () => importIn.click() }, t("importClients")), importIn), restoreSection()) : null;
  const tgOut = h("div", { class: "form" });
  const tgCard = me.role === "rep" ? h("div", { class: "card form" }, h("h2", {}, t("telegram")), h("p", { class: "small muted" }, t("telegramHint")),
    tgOut, h("button", { onclick: async () => {
      const r = await guarded(() => api("POST", "/api/v2/rep/telegram"));
      fill(tgOut, r.link ? h("a", { class: "btn", href: r.link, target: "_blank", rel: "noopener" }, t("openBot")) : null,
        h("code", {}, `/start rep_${r.code}`), h("p", { class: "small muted" }, t("codeExpires")));
    } }, t("connectTelegram"))) : null;
  return h("div", { class: "form" }, h("h1", {}, t("settings")), me.role === "owner" ? subSettingsCard() : null, me.role === "owner" ? trustedCard() : null, fullOwner() ? panelHTTPSCard() : null, fullOwner() ? panelPathCard() : null, me.role === "owner" ? tgCommerceCard() : null, fullOwner() ? migrateCard() : null, fullOwner() ? foreignCard() : null, pwForm, tfaCard(), sessionsCard(), fullOwner() ? telegramBotCard() : null, tgCard, backupCard,
    h("div", { class: "card form" },
      h("label", {}, h("span", {}, t("language")), langSel),
      h("label", {}, h("span", {}, t("theme")), themeSel),
      h("button", { class: "danger", onclick: async () => { await api("POST", "/api/v2/auth/logout"); location.hash = "#/login"; } }, t("logout"))));
}

function applyLang() {
  document.documentElement.lang = lang;
  document.documentElement.dir = lang === "fa" ? "rtl" : "ltr";
}

let routeSeq = 0;
async function route() {
  const seq = ++routeSeq; // a slower, older navigation must not overwrite a newer one
  const app = document.getElementById("app");
  const name = (location.hash.replace(/^#\//, "") || "dashboard").split("?")[0];
  if (name === "login") { app.replaceChildren(viewLogin()); return; }
  try {
    me = await api("GET", "/api/v2/auth/me");
  } catch { return; }
  routes = routesFor(me);
  const first = Object.keys(routes)[0];
  const hidden = { client: [viewClient, "clients"] };
  const view = hidden[name] ? hidden[name][0] : routes[name] || routes[first];
  try {
    const content = await view();
    if (seq !== routeSeq) return;
    app.replaceChildren(shell(hidden[name] ? hidden[name][1] : routes[name] ? name : first, content));
  } catch (e) {
    if (e.message !== "unauthorized") app.replaceChildren(shell(name, h("p", { class: "error" }, t("failed", { e: e.message }))));
  }
}

window.addEventListener("hashchange", route);
window.addEventListener("storage", (e) => {
  if (e.key === "color-scheme") document.querySelector('meta[name="color-scheme"]').content = e.newValue ?? "light dark";
});
applyLang();
route();
setInterval(() => { if (!document.hidden && !document.getElementById("modal").open && /dashboard|clients|nodes/.test(location.hash || "#/dashboard")) route(); }, 15000);

if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {});
