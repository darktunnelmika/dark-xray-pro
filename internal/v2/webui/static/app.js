// Dark Xray Pro panel (v2). No framework, no build step.
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
    reps: "Resellers", tenants: "Connected shops", acc_tenant: "shop key", changePassword: "Change password", oldPassword: "Current password", newPassword: "New password (10+ chars)",
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
    shop: "Shop", resale: "Panel sale", tickets: "Support", analytics: "Insights", products: "Products", orders: "Orders", newOrder: "New order", product: "Product", price: "Price", buyer: "Buyer (name, @telegram, phone)",
    renewClient: "Renew an existing client (optional)", fulfill: "Deliver", cancelOrder: "Cancel", status_pending: "Pending", status_fulfilled: "Delivered",
    status_cancelled: "Cancelled", status_draft: "Draft", status_published: "Published", status_archived: "Archived", statusLabel: "Status", seller: "Seller",
    delivered: "Delivered to {x}", manageProducts: "May create own shop products", shopHint: "Orders are recorded here; delivering one creates (or renews) the client in one step. A reseller's orders are paid from that reseller's credit.",
    newClient: "New client", firstUseShort: "starts on first use",
    telegram: "Telegram", telegramHint: "Connect your Telegram to get your order cards with Deliver/Cancel, your credit and your clients in the bot.",
    connectTelegram: "Connect Telegram", openBot: "Open the bot", codeExpires: "The link works once, for 15 minutes.",
    stepProfile: "Profile", stepNodes: "Nodes & routes", stepSwap: "Dark Swap", stepReview: "Review", next: "Next", back: "Back",
    newInbound: "New inbound", editInbound: "Nodes & routes", nodesRoutes: "Nodes & routes", pickNode: "Select at least one node.",
    clientTg: "Telegram", clientTgHint: "The customer's numeric Telegram id: the bot sends expiry and volume notices there and shows the service under \"My services\". Empty removes the link.", canCreate: "May create clients", canRenew: "May renew (add days or volume)", canEdit: "May change client settings (IP limit, on/off)", priceStars: "Price in Telegram Stars (0 = not sold for Stars)", priceStarsHint: "With a Stars price the bot and the store sell it for Telegram Stars: paid in Telegram and delivered at once, no approval. Customers can also pay with wallet credit.", tcTitle: "Telegram store: trial and referrals", tcTrial: "Free trial (one per Telegram account)", tcTrialOn: "Offer a free trial", tcGB: "GB (0 = unlimited)", tcHours: "Hours", tcRef: "Invite friends (wallet credit in Stars)", tcRefOn: "Reward customers who invite buyers", tcPercent: "% of the Stars price", tcFixed: "Fixed Stars per order", tcFirst: "Only the invited customer's first purchase", tcInvitee: "Invited customer's discount on first purchase (%)", tcBonus: "Volume gift to the inviter per invited buyer (GB)", tcStats: "Invitation results", tcNoStats: "Nobody has come with an invite link yet.", tcMax: "Most Stars per order (0 = no cap)", tcWallet: "Wallet of a customer", tcChat: "Telegram user id", tcDelta: "Add (+) or remove (−) Stars", tcBalance: "Balance: {n}⭐", tcLookup: "Show", tcApply: "Apply", migTitle: "Move from the old panel", migHint: "Brings resellers and sub-sellers with their credit, clients (same UUID, plan, IP limit, expiry, usage), groups, panel logins (old passwords keep working) and active subscription links (old links answer with the new subscription). The old panel is only read, never changed. Preview first: nothing is written until you press Import.", migSrc: "Source", migServer: "The old panel on this server", migUpload: "Upload its database file (business.sqlite)", migPath: "Path", migInbounds: "Give moved clients these inbounds", migArchived: "Also move archived clients (disabled)", migPreview: "Preview", migImport: "Import", migAsk: "Import into this panel now? Existing names are skipped, never overwritten.", migWorking: "Reading the old panel…", migDone: "Imported.", migWould: "Preview — nothing written yet:", migGot: "Done:", mig_reps: "resellers", mig_sellers: "sub-sellers", mig_clients: "clients", mig_disabled: "of them disabled", mig_usage: "with usage", mig_groups: "groups", mig_logins: "logins", mig_links: "old links kept", migSkipped: "Skipped", migNotes: "Notes", trustTitle: "IP limit exemptions", trustHint: "Addresses never counted toward a client's IP limit — an office or school NAT, a CDN or relay exit, a monitoring probe. One IP or network (CIDR) per line.", bulkCreate: "Create many", bcHint: "Creates numbered clients of one plan in one go, e.g. shop-001 … shop-050. If any name already exists nothing is created.", bcPrefix: "Name prefix", bcStart: "First number", bcCount: "How many", bcPad: "Digits", bcSuffix: "Suffix", bcVol: "Volume (GB, 0 = unlimited)", bcDays: "Days (0 = no expiry)", bcFirstUse: "Days start at the first connection", bcHold: "…or anyway after this many days unused (0 = wait)", bcIPs: "Devices (IP limit, 0 = any)", bcGroup: "Add to group", bcNone: "none", bcNote: "Note", bcPreview: "Names: {a} … {b}", bcDone: "{n} clients created", bcCSV: "Download names and links (CSV)", onHold: "On hold", onHoldHint: "Unused first-connection clients start anyway after this many days (0 = wait for the first connection).", onHoldUntil: "starts by {d} if unused", miniTitle: "Telegram store (Mini App)", miniHint: "Customers open the store from the bot: their services with usage, QR and links, the plans, and their orders. Telegram needs an HTTPS address with a real certificate — by default the public subscription address + /app/.", miniOn: "Show the store in the bot", miniURL: "Store address (empty = automatic)", wgSubnet: "Tunnel subnet (WireGuard)", phTitle: "Panel address (HTTPS)", phHint: "Serve the panel on its own port with a real certificate for your domain. Issue or upload a certificate that covers the domain (Inbounds → Certificates; Cloudflare DNS works without opening port 80), point the domain at this server, then save and restart.", phDomain: "Domain", phPort: "Port (0 = off)", phCert: "Certificate: {x} · {d} days left", phNoCert: "No certificate covers this domain yet — the panel answers with its self-signed one until there is.", phRunning: "Listening now on port {p}", phNotRunning: "Not listening yet", phRestart: "Save and restart the HUB", phRestartAsk: "Restart the HUB to apply? The panel and subscriptions are away for a few seconds; nodes keep running.", phOpen: "Open", whTitle: "Webhook", whHint: "Send what happens in the panel to another system as signed JSON (X-Dark-Signature: sha256=HMAC of the body). Items wait and are resent while the receiver is down. A Discord or Slack webhook address works directly — it gets readable messages.", whOn: "Send events", whURL: "Receiver address", whSecret: "Signing secret", whSecretSaved: "saved — empty keeps it, - removes it", whKinds: "What to send", wh_client: "Client events (expired, out of volume, too many IPs, resets, next plan, refused devices)", wh_change: "Changes in the panel (who changed what)", wh_node: "Node offline / back online", whTest: "Send a test", whTestOK: "The receiver answered OK", whLastOK: "Last delivered", whLastErr: "Last error", restoreTitle: "Restore a backup", restoreHint: "Upload a .sqlite.gz or encrypted .dkb backup. You see what is in it before anything changes; the current database is kept on the server.", restoreFile: "Backup file", restorePass: "Password (encrypted backups only)", restoreCheck: "Check backup", restoreChecking: "Checking…", restoreFound: "{c} clients · {n} nodes · {i} inbounds · {l} logins", restoreApply: "Restore and restart", restoreAsk: "Replace everything in the panel with this backup? The HUB restarts; nodes reconnect on their own. Sign in with a login from the backup.", restoreWait: "Restoring — the panel comes back in a few seconds…", xrayLogs: "Xray log", logsLoading: "Asking {x} for its log…", logsEmpty: "Nothing logged yet.", logsErrors: "Warnings and errors only", logsGrep: "Search (IP, email, domain…)", logsRefresh: "Refresh", restartXray: "Restart Xray", restartAsk: "Restart Xray on {x}? Connections drop for a second.", restarting: "Restarting Xray on {x}…", restarted: "Xray on {x} restarted", logsOld: "This node's agent is too old for logs — update the agent first.", keyDays: "Expires after (days, 0 = never)", keyUntil: "until", keysTitle: "API keys", keysHint: "For scripts, bots and other panels. Send it as Authorization: Bearer <key>. An admin key can change things, a viewer key only reads; staff, keys, backups and finance stay closed. Every change is in the change log.", keyName: "Name", addKey: "New key", keyOnce: "Copy this key now — it is not shown again.", keyDelAsk: "Delete the key {x}? Anything using it stops working.", keyNone: "No API keys yet.", apiRef: "API reference", acc_public: "public", acc_viewer: "read", acc_admin: "admin", acc_owner: "owner", acc_rep: "reseller", you: "you", staff: "Team", staffTitle: "Panel logins", staffHint: "Admins manage clients, inbounds, nodes and routing; viewers can only look. Staff, the change log, backups and finance stay with owners.", role: "Role", role_owner: "Owner", role_admin: "Admin", role_viewer: "Viewer", addLogin: "Add login", newPassOpt: "New password (empty = keep)", lastSeen: "Last change", tfaOn: "2FA on", tfaReset: "Reset 2FA", tfaResetAsk: "Turn off two-step login for {x}? They can sign in with the password alone until they set it up again.", delLoginAsk: "Delete the login {x}? Their sessions end now.", auditTitle: "Change log", auditHint: "Every change made in the panel or through the API, and every sign-in. Request contents are never stored.", auditFilter: "Filter by login, client or path", auditMore: "Older", act_login: "signed in", act_loginFail: "wrong password", act_denied: "refused", tfaTitle: "Two-step login", tfaHint: "Sign-in also asks for a 6-digit code from an authenticator app (Google Authenticator, Aegis, 2FAS…).", tfaStart: "Set up", tfaScan: "Scan this in the app, or enter the key by hand, then type the code it shows.", tfaKey: "Key", tfaCode: "6-digit code", tfaEnable: "Turn on", tfaEnabled: "Two-step login is on.", tfaDisable: "Turn off", tfaOff: "Two-step login is off.", tfaNeed: "Enter the code from your authenticator app.", fbHint: "One TLS port for everything: VLESS Vision answers directly, and VLESS, VMess and Trojan over WebSocket are passed on by path. Customers get all four in their subscription; traffic is counted under this inbound.", badTag: "Tag: lowercase letters, digits, dot, dash or underscore.", needCert: "TLS presets need a certificate (add one under TLS certificates).",
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
    reps: "نماینده‌ها", tenants: "فروشگاه‌های متصل", acc_tenant: "کلید فروشگاه", changePassword: "تغییر رمز", oldPassword: "رمز فعلی", newPassword: "رمز جدید (حداقل ۱۰ کاراکتر)",
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
    shop: "فروشگاه", resale: "فروش پنل", tickets: "پشتیبانی", analytics: "تحلیل", products: "محصولات", orders: "سفارش‌ها", newOrder: "سفارش جدید", product: "محصول", price: "قیمت", buyer: "خریدار (نام، آیدی تلگرام، تلفن)",
    renewClient: "تمدید کلاینت موجود (اختیاری)", fulfill: "تحویل", cancelOrder: "لغو", status_pending: "در انتظار", status_fulfilled: "تحویل شد",
    status_cancelled: "لغو شد", status_draft: "پیش‌نویس", status_published: "منتشر", status_archived: "حذف‌شده", statusLabel: "وضعیت", seller: "فروشنده",
    delivered: "تحویل شد: {x}", manageProducts: "اجازهٔ ساخت محصول فروشگاه", shopHint: "سفارش‌ها اینجا ثبت می‌شوند؛ با «تحویل» کلاینت یک‌جا ساخته (یا تمدید) می‌شود. سفارش‌های نماینده از اعتبار همان نماینده پرداخت می‌شود.",
    newClient: "کلاینت جدید", firstUseShort: "شروع از اولین اتصال",
    telegram: "تلگرام", telegramHint: "تلگرامتان را وصل کنید تا سفارش‌ها با دکمهٔ تحویل/لغو، اعتبار و کلاینت‌هایتان در ربات بیاید.",
    connectTelegram: "اتصال تلگرام", openBot: "باز کردن ربات", codeExpires: "این لینک یک‌بار مصرف است و ۱۵ دقیقه اعتبار دارد.",
    stepProfile: "پروفایل", stepNodes: "نودها و مسیرها", stepSwap: "دارک سواپ", stepReview: "بازبینی", next: "بعدی", back: "قبلی",
    newInbound: "اینباند جدید", editInbound: "نودها و مسیرها", nodesRoutes: "نودها و مسیرها", pickNode: "حداقل یک نود انتخاب کنید.",
    clientTg: "تلگرام", clientTgHint: "شناسهٔ عددی تلگرام مشتری: ربات هشدار انقضا و حجم را آنجا می‌فرستد و سرویس را در «سرویس‌های من» نشان می‌دهد. خالی یعنی حذف اتصال.", canCreate: "اجازهٔ ساخت کاربر", canRenew: "اجازهٔ تمدید (افزودن روز یا حجم)", canEdit: "اجازهٔ تغییر تنظیمات کاربر (محدودیت IP، روشن/خاموش)", priceStars: "قیمت به استارز تلگرام (۰ = با استارز فروخته نمی‌شود)", priceStarsHint: "با قیمت استارز، ربات و فروشگاه این محصول را با استارز تلگرام می‌فروشند: پرداخت داخل تلگرام و تحویل فوری، بدون نیاز به تأیید. مشتری می‌تواند با اعتبار کیف پول هم بپردازد.", tcTitle: "فروشگاه تلگرام: تست رایگان و دعوت", tcTrial: "تست رایگان (یک بار برای هر حساب تلگرام)", tcTrialOn: "ارائهٔ تست رایگان", tcGB: "گیگ (۰ = نامحدود)", tcHours: "ساعت", tcRef: "دعوت دوستان (اعتبار کیف پول به استارز)", tcRefOn: "پاداش به مشتری‌ای که خریدار دعوت کند", tcPercent: "درصد از قیمت استارز", tcFixed: "استارز ثابت برای هر سفارش", tcFirst: "فقط اولین خرید دوست دعوت‌شده", tcInvitee: "تخفیف دوست دعوت‌شده روی اولین خرید (٪)", tcBonus: "حجم هدیه به دعوت‌کننده برای هر دوستِ خریدار (گیگ)", tcStats: "نتیجهٔ دعوت‌ها", tcNoStats: "هنوز کسی با لینک دعوت نیامده است.", tcMax: "سقف استارز هر سفارش (۰ = بدون سقف)", tcWallet: "کیف پول یک مشتری", tcChat: "شناسهٔ عددی تلگرام", tcDelta: "افزودن (+) یا کم کردن (−) استارز", tcBalance: "موجودی: {n}⭐", tcLookup: "نمایش", tcApply: "اعمال", migTitle: "انتقال از پنل قدیمی", migHint: "نماینده‌ها و زیرفروشنده‌ها با اعتبارشان، کاربرها (همان UUID، پلن، محدودیت IP، انقضا و مصرف)، گروه‌ها، ورودهای پنل (رمزهای قبلی کار می‌کنند) و لینک‌های اشتراک فعال (لینک قدیمی اشتراک جدید را برمی‌گرداند) منتقل می‌شوند. پنل قدیمی فقط خوانده می‌شود و تغییری نمی‌کند. اول پیش‌نمایش بگیرید: تا «انتقال» را نزنید چیزی نوشته نمی‌شود.", migSrc: "منبع", migServer: "پنل قدیمی روی همین سرور", migUpload: "بارگذاری فایل دیتابیس آن (business.sqlite)", migPath: "مسیر", migInbounds: "این اینباندها به کاربرهای منتقل‌شده داده شود", migArchived: "کاربرهای بایگانی‌شده هم منتقل شوند (غیرفعال)", migPreview: "پیش‌نمایش", migImport: "انتقال", migAsk: "همین حالا به این پنل منتقل شود؟ نام‌های موجود رد می‌شوند و هرگز بازنویسی نمی‌شوند.", migWorking: "در حال خواندن پنل قدیمی…", migDone: "منتقل شد.", migWould: "پیش‌نمایش — هنوز چیزی نوشته نشده:", migGot: "انجام شد:", mig_reps: "نماینده", mig_sellers: "زیرفروشنده", mig_clients: "کاربر", mig_disabled: "از آن‌ها غیرفعال", mig_usage: "با سابقهٔ مصرف", mig_groups: "گروه", mig_logins: "ورود پنل", mig_links: "لینک قدیمی حفظ‌شده", migSkipped: "رد شده", migNotes: "توضیحات", trustTitle: "استثناهای محدودیت IP", trustHint: "آدرس‌هایی که در محدودیت IP کاربر شمرده نمی‌شوند — NAT دفتر یا مدرسه، خروجی CDN یا ریلی، سرویس مانیتورینگ. هر خط یک IP یا شبکه (CIDR).", bulkCreate: "ساخت گروهی", bcHint: "کاربرهای شماره‌دار با یک پلن را یک‌جا می‌سازد، مثل shop-001 تا shop-050. اگر یکی از نام‌ها از قبل باشد هیچ کاربری ساخته نمی‌شود.", bcPrefix: "پیشوند نام", bcStart: "شماره شروع", bcCount: "تعداد", bcPad: "تعداد رقم", bcSuffix: "پسوند", bcVol: "حجم (گیگ، ۰ = نامحدود)", bcDays: "روز (۰ = بدون انقضا)", bcFirstUse: "روزها از اولین اتصال شروع شود", bcHold: "…یا اگر استفاده نشد بعد از این تعداد روز (۰ = صبر کن)", bcIPs: "دستگاه (محدودیت IP، ۰ = آزاد)", bcGroup: "افزودن به گروه", bcNone: "هیچ", bcNote: "یادداشت", bcPreview: "نام‌ها: {a} … {b}", bcDone: "{n} کاربر ساخته شد", bcCSV: "دانلود نام‌ها و لینک‌ها (CSV)", onHold: "در انتظار شروع", onHoldHint: "کاربرهای «شروع از اولین اتصال» اگر استفاده نکنند بعد از این تعداد روز خودکار شروع می‌شوند (۰ = تا اولین اتصال صبر کن).", onHoldUntil: "اگر استفاده نشود تا {d} شروع می‌شود", miniTitle: "فروشگاه تلگرام (Mini App)", miniHint: "مشتری فروشگاه را از داخل ربات باز می‌کند: سرویس‌هایش با مصرف، QR و لینک، پلن‌ها و سفارش‌هایش. تلگرام آدرس HTTPS با گواهی معتبر می‌خواهد — پیش‌فرض آدرس عمومی اشتراک + /app/ است.", miniOn: "نمایش فروشگاه در ربات", miniURL: "آدرس فروشگاه (خالی = خودکار)", wgSubnet: "زیرشبکهٔ تونل (WireGuard)", phTitle: "آدرس پنل (HTTPS)", phHint: "پنل را روی پورت جدا با گواهی واقعی برای دامنهٔ خودتان بالا بیاورید. گواهی‌ای که دامنه را پوشش دهد بگیرید یا بارگذاری کنید (اینباندها ← گواهی‌ها؛ DNS کلودفلر بدون باز کردن پورت ۸۰ کار می‌کند)، دامنه را به این سرور وصل کنید، بعد ذخیره و ری‌استارت کنید.", phDomain: "دامنه", phPort: "پورت (۰ = خاموش)", phCert: "گواهی: {x} · {d} روز مانده", phNoCert: "هنوز گواهی‌ای برای این دامنه نیست — تا وقتی نباشد پنل با گواهی خودامضا جواب می‌دهد.", phRunning: "الان روی پورت {p} در حال کار", phNotRunning: "هنوز فعال نیست", phRestart: "ذخیره و ری‌استارت هاب", phRestartAsk: "برای اعمال، هاب ری‌استارت شود؟ پنل و اشتراک‌ها چند ثانیه در دسترس نیستند؛ نودها کار می‌کنند.", phOpen: "باز کردن", whTitle: "وب‌هوک", whHint: "اتفاق‌های پنل را به صورت JSON امضاشده به سیستم دیگری بفرستید (X-Dark-Signature: sha256=HMAC بدنه). وقتی گیرنده در دسترس نباشد، موارد صبر می‌کنند و دوباره فرستاده می‌شوند. آدرس وب‌هوک دیسکورد یا اسلک هم مستقیم کار می‌کند و پیام خوانا می‌گیرد.", whOn: "ارسال رویدادها", whURL: "آدرس گیرنده", whSecret: "کلید امضا", whSecretSaved: "ذخیره شده — خالی یعنی بدون تغییر، - یعنی حذف", whKinds: "چه چیزهایی فرستاده شود", wh_client: "رویدادهای کاربر (انقضا، اتمام حجم، IP زیاد، ریست، برنامهٔ بعدی، دستگاه ردشده)", wh_change: "تغییرات پنل (چه کسی چه چیزی را عوض کرد)", wh_node: "قطع و وصل شدن نودها", whTest: "ارسال آزمایشی", whTestOK: "گیرنده جواب درست داد", whLastOK: "آخرین ارسال موفق", whLastErr: "آخرین خطا", restoreTitle: "بازگردانی بکاپ", restoreHint: "فایل .sqlite.gz یا بکاپ رمزدار .dkb را بارگذاری کنید. قبل از هر تغییری محتوایش را می‌بینید؛ دیتابیس فعلی روی سرور نگه داشته می‌شود.", restoreFile: "فایل بکاپ", restorePass: "رمز (فقط برای بکاپ رمزدار)", restoreCheck: "بررسی بکاپ", restoreChecking: "در حال بررسی…", restoreFound: "{c} کاربر · {n} نود · {i} اینباند · {l} ورود", restoreApply: "بازگردانی و ری‌استارت", restoreAsk: "همه چیز پنل با این بکاپ جایگزین شود؟ هاب ری‌استارت می‌شود و نودها خودشان دوباره وصل می‌شوند. با یکی از ورودهای داخل بکاپ وارد شوید.", restoreWait: "در حال بازگردانی — پنل تا چند ثانیه دیگر برمی‌گردد…", xrayLogs: "لاگ Xray", logsLoading: "در حال گرفتن لاگ از {x}…", logsEmpty: "هنوز چیزی ثبت نشده.", logsErrors: "فقط هشدارها و خطاها", logsGrep: "جستجو (IP، ایمیل، دامنه…)", logsRefresh: "تازه کردن", restartXray: "ری‌استارت Xray", restartAsk: "Xray روی {x} ری‌استارت شود؟ اتصال‌ها یک لحظه قطع می‌شوند.", restarting: "در حال ری‌استارت Xray روی {x}…", restarted: "Xray روی {x} ری‌استارت شد", logsOld: "عامل این نود برای لاگ قدیمی است — اول عامل را به‌روز کنید.", keyDays: "انقضا بعد از (روز، ۰ = هرگز)", keyUntil: "تا", keysTitle: "کلیدهای API", keysHint: "برای اسکریپت، ربات و پنل‌های دیگر. به شکل Authorization: Bearer <کلید> بفرستید. کلید مدیر می‌تواند تغییر دهد و کلید ناظر فقط می‌خواند؛ تیم، کلیدها، بکاپ و مالی بسته می‌ماند. هر تغییر در گزارش تغییرات ثبت می‌شود.", keyName: "نام", addKey: "کلید جدید", keyOnce: "همین حالا این کلید را کپی کنید — دوباره نشان داده نمی‌شود.", keyDelAsk: "کلید {x} حذف شود؟ هر چیزی که از آن استفاده می‌کند از کار می‌افتد.", keyNone: "هنوز کلید API ندارید.", apiRef: "مرجع API", acc_public: "عمومی", acc_viewer: "خواندن", acc_admin: "مدیر", acc_owner: "مالک", acc_rep: "نماینده", you: "شما", staff: "تیم", staffTitle: "ورودهای پنل", staffHint: "مدیر کمکی کاربران، اینباندها، نودها و مسیریابی را مدیریت می‌کند؛ ناظر فقط می‌بیند. تیم، گزارش تغییرات، بکاپ و مالی فقط دست مالک است.", role: "نقش", role_owner: "مالک", role_admin: "مدیر کمکی", role_viewer: "ناظر", addLogin: "افزودن ورود", newPassOpt: "رمز جدید (خالی = بدون تغییر)", lastSeen: "آخرین تغییر", tfaOn: "دومرحله‌ای فعال", tfaReset: "حذف دومرحله‌ای", tfaResetAsk: "ورود دومرحله‌ای {x} خاموش شود؟ تا دوباره راه‌اندازی کند فقط با رمز وارد می‌شود.", delLoginAsk: "ورود {x} حذف شود؟ نشست‌هایش همین حالا بسته می‌شود.", auditTitle: "گزارش تغییرات", auditHint: "هر تغییری که در پنل یا با API انجام شود و هر ورود. محتوای درخواست‌ها هرگز ذخیره نمی‌شود.", auditFilter: "فیلتر با نام ورود، کاربر یا مسیر", auditMore: "قدیمی‌تر", act_login: "وارد شد", act_loginFail: "رمز اشتباه", act_denied: "رد شد", tfaTitle: "ورود دومرحله‌ای", tfaHint: "هنگام ورود یک کد ۶ رقمی از برنامهٔ احراز هویت (Google Authenticator، Aegis، 2FAS و…) هم پرسیده می‌شود.", tfaStart: "راه‌اندازی", tfaScan: "این را در برنامه اسکن کنید یا کلید را دستی وارد کنید، بعد کدی که نشان می‌دهد را بنویسید.", tfaKey: "کلید", tfaCode: "کد ۶ رقمی", tfaEnable: "فعال کن", tfaEnabled: "ورود دومرحله‌ای فعال شد.", tfaDisable: "خاموش کن", tfaOff: "ورود دومرحله‌ای خاموش شد.", tfaNeed: "کد برنامهٔ احراز هویت را وارد کنید.", fbHint: "یک پورت TLS برای همه: VLESS Vision مستقیم جواب می‌دهد و VLESS، VMess و Trojan روی WebSocket بر اساس مسیر تحویل داده می‌شوند. مشتری هر چهار را در اشتراکش می‌گیرد و ترافیک به حساب همین اینباند نوشته می‌شود.", badTag: "شناسه: حروف کوچک انگلیسی، عدد، نقطه، خط تیره یا زیرخط.", needCert: "الگوهای TLS گواهی لازم دارند (از بخش گواهی‌های TLS اضافه کنید).",
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

// The panel's own name (the owner can change it; customer pages keep their
// own brand). Last known name first, then the server's.
let panelName = safeGet("panelName") || "Dark Xray Pro";
function brandText() {
  const w = panelName.trim().split(/\s+/);
  const last = w.pop();
  return h("span", {}, w.length ? w.join(" ") + " " : "", h("b", { class: "neon-text" }, last));
}
async function loadPanelName() {
  try {
    const r = await fetch("/api/v2/panel-name", { credentials: "same-origin" });
    if (!r.ok) return;
    const n = ((await r.json()).name || "").trim() || "Dark Xray Pro";
    document.title = n;
    if (n !== panelName) { panelName = n; safeSet("panelName", n); document.querySelectorAll(".brand-text").forEach((el) => el.replaceChildren(brandText())); }
  } catch {}
}
document.title = panelName;
loadPanelName();

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

// movedHere: a reseller that came from another panel with its users' old
// usage — its lifetime total and its traffic on this panel differ.
function movedHere(total, here) { return (total || 0) - (here || 0) > 1073741824; }

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
  if (!res.ok) throw new Error(faErr(text.trim() || res.statusText));
  return text ? JSON.parse(text) : null;
}

// faErr: the server's English errors in plain Persian (numbers kept).
const FA_ERRS = [
  [/old users cannot be renewed/i, "کاربرهای قدیمی (منتقل‌شده از پنل قبلی) تمدید نمی‌شوند؛ برایشان کاربر جدید بسازید"],
  [/overdue bill is unpaid/i, "یک فاکتور از مهلتش گذشته و پرداخت نشده؛ تا پرداخت آن فروش و ساخت کاربر قفل است"],
  [/no volume credit left/i, "اعتبار حجمی تمام شده است"],
  [/wallet does not cover the wholesale price/i, "موجودی کیف پول برای قیمت عمده کافی نیست؛ اول کیف پول را از ربات اصلی شارژ کنید"],
  [/panel sale is (no longer )?(not )?allowed/i, "فروش پنل برای این نماینده مجاز نیست"],
  [/this username is taken or not valid/i, "این نام کاربری گرفته شده یا معتبر نیست"],
  [/the price of (.+) cannot be below the wholesale price (\d+)/i, "قیمت «$1» نمی‌تواند کمتر از قیمت عمده ($2 تومان) باشد"],
  [/bringing old users is not allowed/i, "آوردن کاربران قدیمی برای شما فعال نیست"],
  [/the domain is already moved/i, "دامنه منتقل شده؛ دیگر لینکی خوانده نمی‌شود"],
  [/links are still being read/i, "لینک‌های قبلی هنوز در حال خواندن‌اند؛ کمی صبر کنید"],
  [/bring at least one user first/i, "اول دست‌کم یک کاربر بیاورید"],
  [/1 to 5000 links/i, "بین ۱ تا ۵۰۰۰ لینک بفرستید"],
  [/the client limit \((\d+)\) is reached/i, "به سقف تعداد کاربر ($1) رسیده‌اید"],
  [/client already exists/i, "این نام قبلاً استفاده شده است"],
  [/the name may use only/i, "نام فقط حروف انگلیسی، عدد و . _ - باشد (بدون فاصله)"],
  [/insufficient credit/i, "اعتبار کافی نیست"],
  [/picture must be a JPEG or a PNG/i, "عکس باید JPEG یا PNG باشد"],
  [/picture must be under 2 MB/i, "عکس باید کمتر از ۲ مگابایت باشد"],
  [/button name is one line of at most 40/i, "نام دکمه یک خط و حداکثر ۴۰ نویسه است"],
  [/at least one button must stay visible/i, "دست‌کم یک دکمه باید نمایش داده شود"],
  [/upload a picture first/i, "اول یک عکس بارگذاری کنید"],
  [/password needs at least 10/i, "رمز باید حداقل ۱۰ نویسه باشد"],
  [/already used by another account|that username is taken/i, "این نام کاربری را حساب دیگری دارد؛ نام دیگری انتخاب کنید"],
  [/a reseller or a sign-in name .* already exists|that ID is taken/i, "این شناسه قبلاً گرفته شده است؛ شناسهٔ دیگری انتخاب کنید"],
  [/may only have lowercase letters/i, "فقط حروف کوچک انگلیسی، عدد، نقطه، خط تیره و زیرخط مجاز است"],
  [/at most (\d+) Telegram ids/i, "حداکثر $1 شناسهٔ تلگرام می‌شود وارد کرد"],
  [/the longest term allowed is (\d+) days/i, "بیشترین مدت مجاز برای شما $1 روز است"],
  [/the largest volume allowed is (\d+) GB/i, "بیشترین حجم مجاز برای شما $1 گیگ است"],
  [/unlimited plans are not allowed/i, "فروش نامحدود برای شما فعال نیست"],
  [/managing products is not allowed/i, "مدیریت محصولات برای شما فعال نیست؛ از مدیر بخواهید فعالش کند"],
  [/inbound "?([^" ]+)"? is not allowed/i, "اینباند $1 برای شما مجاز نیست"],
  [/unknown inbound "?([^" ]+)"?/i, "اینباند $1 دیگر وجود ندارد؛ از لیست اینباندها حذفش کنید"],
  [/a volume product needs GB/i, "برای محصول حجمی، حجم را وارد کنید"],
  [/that ID is taken/i, "این شناسه قبلاً استفاده شده"],
  [/category names must be unique and short/i, "نام دسته‌ها باید تکراری نباشد و کوتاه باشد (حداکثر ۴۰ حرف)"],
  [/at most 40 categories/i, "حداکثر ۴۰ دسته"],
  [/unknown seller/i, "فروشنده پیدا نشد"],
  [/product is gone/i, "این محصول حذف شده"],
  [/only pending orders can be cancelled/i, "فقط سفارش‌های منتظر لغو می‌شوند"],
  [/order changed meanwhile/i, "سفارش هم‌زمان تغییر کرد؛ صفحه را تازه کنید"],
  [/sub-sellers sell the products their reseller defines/i, "زیرفروشنده محصول نمی‌سازد؛ محصولات را نماینده برایش تعریف می‌کند"],
  [/sub-sellers (sell only|renew with) .*plan/i, "فقط پلن‌هایی که نماینده برای شما تعریف کرده قابل فروش و تمدید است"],
  [/^not found$/i, "پیدا نشد"],
  [/^temporarily unavailable$/i, "سرور موقتاً در دسترس نیست؛ کمی بعد دوباره امتحان کنید"],
  [/^invalid( input)?$/i, "اطلاعات واردشده درست نیست"],
];
function faErr(msg) {
  if (typeof lang === "undefined" || lang !== "fa") return msg;
  for (const [re, fa] of FA_ERRS) { const m = msg.match(re); if (m) return fa.replace(/\$(\d)/g, (_, i) => m[i] || ""); }
  return msg;
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
  d.classList.remove("wide");
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
const allRoutes = { dashboard: viewDashboard, online: viewOnline, clients: viewClients, groups: viewGroups, inbounds: viewInbounds, nodes: viewNodes, routing: viewRouting, reps: viewReps, tenants: viewTenants, shop: viewShop, resale: viewResale, tickets: viewTickets, analytics: viewAnalytics, finance: viewFinance, activity: viewActivity, staff: viewStaff, settings: viewSettings };
let me = null;
let routes = allRoutes;
function fullOwner() { return me && me.role === "owner" && !me.level; }
function routesFor(m) {
  if (m && m.role === "owner" && m.level) {
    const r = { ...allRoutes };
    delete r.finance; delete r.staff; delete r.tenants;
    return r;
  }
  if (m && m.role === "rep") {
    const r = { dashboard: viewRepDashboard, clients: viewClients, online: viewOnline, credit: viewCredit };
    if (m.can_sell) r.sellers = viewSellers;
    if (m.resale) r.resale = viewResaleRep;
    r.shop = viewShop; r.tickets = viewTickets; r.analytics = viewAnalytics; r.settings = viewSettings;
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
  g.append(svgEl("stop", { offset: "0", "stop-color": "#28f7a0" }), svgEl("stop", { offset: "1", "stop-color": "#4bb9ff" }));
  defs.append(g);
  svg.append(defs,
    svgEl("path", { d: "M16 2.5 28 9.25v13.5L16 29.5 4 22.75V9.25z", fill: "none", stroke: `url(#${id})`, "stroke-width": 2.2, "stroke-linejoin": "round" }),
    svgEl("circle", { cx: 12.5, cy: 19.5, r: 2.2, fill: `url(#${id})` }),
    svgEl("path", { d: "M12.5 14.5a5 5 0 0 1 5 5", stroke: `url(#${id})`, "stroke-width": 2.2, fill: "none", "stroke-linecap": "round" }),
    svgEl("path", { d: "M12.5 10a9.5 9.5 0 0 1 9.5 9.5", stroke: `url(#${id})`, "stroke-width": 2.2, fill: "none", "stroke-linecap": "round" }));
  return svg;
}
const ICONS = {
  copy: "M8 8h12v12H8zM4 16V4h12",
  power: "M12 2v10M18.4 6.6a9 9 0 1 1-12.8 0",
  refresh: "M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5",
  dashboard: "M3 13h8V3H3zM13 21h8V11h-8zM3 21h8v-6H3zM13 3v6h8V3z",
  clients: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75",
  groups: "M12 2 2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5",
  inbounds: "M6 3v6M18 3v6M4 9h16v3a8 8 0 0 1-16 0zM12 20v2",
  nodes: "M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01",
  reps: "M3 7h18v13H3zM8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M3 13h18",
  tenants: "M3 9 5 3h14l2 6M3 9h18v12H3zM9 21v-6h6v6M7 9a2 2 0 0 0 4 0 2 2 0 0 0 4 0 2 2 0 0 0 4 0",
  sellers: "M15 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M8.5 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM20 8v6M23 11h-6",
  analytics: "M4 20V10M10 20V4M16 20v-7M22 20H2",
  tickets: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2zM8 9h8M8 13h5",
  resale: "M3 9l1.5-5h15L21 9M3 9h18v2a3 3 0 0 1-6 0 3 3 0 0 1-6 0 3 3 0 0 1-6 0V9zM5 13v8h14v-8M10 21v-5h4v5",
  shop: "M6 2 3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4zM3 6h18M16 10a4 4 0 0 1-8 0",
  finance: "M3 3v18h18M7 15l4-4 3 3 6-6",
  activity: "M22 12h-4l-3 9L9 3l-3 9H2",
  credit: "M2 7h20v12H2zM2 11h20M6 15h4",
  staff: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10zM9 12l2 2 4-4",
  settings: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  routing: "M6 3v12M18 9v12M6 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM18 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM6 15c0-4 12-2 12-6",
};
function icon(name) {
  const svg = svgEl("svg", { viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": 1.8, "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true" });
  svg.append(svgEl("path", { d: ICONS[name] || ICONS.dashboard }));
  return svg;
}

// ---------- Shell: header actions, command palette, notifications ----------
const UX = {
  fa: { search: "جستجو…", searchHint: "نام کاربر، نود یا صفحه", noHits: "چیزی پیدا نشد", pages: "صفحه‌ها", clientsH: "کلاینت‌ها", nodesH: "نودها",
    notif: "اعلان‌ها", allGood: "همه چیز مرتب است ✨", nodeDown: "نود {x} آفلاین است", pendingOrders: "{n} سفارش در انتظار تأیید", theme: "پوسته", dark: "تیره", light: "روشن", auto: "خودکار",
    more: "بیشتر", of: "از", account: "حساب", ofTotal: "{a} از {b}", online: "آنلاین", offline: "آفلاین", nodesSection: "نودها" },
  en: { search: "Search…", searchHint: "Client, node or page", noHits: "Nothing found", pages: "Pages", clientsH: "Clients", nodesH: "Nodes",
    notif: "Notifications", allGood: "All good ✨", nodeDown: "Node {x} is offline", pendingOrders: "{n} orders waiting", theme: "Theme", dark: "Dark", light: "Light", auto: "Auto",
    more: "More", of: "of", account: "Account", ofTotal: "{a} of {b}", online: "online", offline: "offline", nodesSection: "Nodes" },
};
const ux = (k, vars) => { let s = (UX[lang] || UX.en)[k] ?? UX.en[k] ?? k; for (const [a, b] of Object.entries(vars || {})) s = s.replaceAll(`{${a}}`, b); return s; };
Object.assign(ICONS, {
  search: "M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.35-4.35",
  bell: "M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 0 1-3.46 0",
  moon: "M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z",
  sun: "M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10zM12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42",
  globe: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z",
  logout: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9",
  more: "M12 13a1 1 0 1 0 0-2 1 1 0 0 0 0 2zM19 13a1 1 0 1 0 0-2 1 1 0 0 0 0 2zM5 13a1 1 0 1 0 0-2 1 1 0 0 0 0 2z",
  traffic: "M3 17l6-6 4 4 8-8M14 7h7v7",
  pulse: "M22 12h-4l-3 9L9 3l-3 9H2",
  trash: "M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6M10 11v6M14 11v6",
  link: "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71",
});

function avatar(name, cls = "") {
  let hsh = 0;
  for (const ch of String(name)) hsh = (hsh * 31 + ch.codePointAt(0)) >>> 0;
  const hue = hsh % 360;
  return h("span", { class: `avatar ${cls}`, style: `--h:${hue}`, "aria-hidden": "true" }, [...String(name).replace(/[^\p{L}\p{N}]/gu, "")].slice(0, 2).join("").toUpperCase() || "?");
}

// A friendly empty screen with one clear next step.
function emptyState(title, text, action, onclick) {
  return h("div", { class: "card empty-state" }, h("h3", {}, title), h("p", { class: "muted" }, text),
    action ? h("button", { class: "primary", onclick }, action) : null);
}

function iconBtn(name, label, onclick, extra) {
  return h("button", { class: "iconbtn", type: "button", title: label, "aria-label": label, onclick }, icon(name), extra || null);
}

function setScheme(v) {
  safeSet("color-scheme", v === "light dark" ? null : v);
  document.querySelector('meta[name="color-scheme"]').content = v;
}

function openPalette() {
  const dlg = h("dialog", { class: "palette", closedby: "any" });
  const input = h("input", { type: "text", enterkeyhint: "go", placeholder: ux("searchHint"), autocomplete: "off", "aria-label": ux("search") });
  const list = h("div", { class: "hits", role: "listbox" });
  let clients = [], nodes = [], sel = 0, items = [];
  const go = (href) => { dlg.close(); location.hash = href; };
  const draw = () => {
    const q = input.value.trim().toLowerCase();
    const pages = Object.keys(routes).filter((r) => !q || t(r).toLowerCase().includes(q) || r.includes(q)).map((r) => ({ g: ux("pages"), ic: r, label: t(r), href: `#/${r}` }));
    const cs = q ? clients.filter((c) => (c.Email + " " + (c.Note || "")).toLowerCase().includes(q)).slice(0, 8).map((c) => ({ g: ux("clientsH"), av: c.Email, label: c.Email, sub: c.Note || "", href: `#/client?e=${encodeURIComponent(c.Email)}` })) : [];
    const ns = q ? nodes.filter((n) => (n.ID + " " + (n.Name || "")).toLowerCase().includes(q)).map((n) => ({ g: ux("nodesH"), ic: "nodes", label: n.Name || n.ID, sub: n.ID, href: "#/nodes" })) : [];
    items = [...cs, ...ns, ...pages].slice(0, 14);
    sel = Math.min(sel, Math.max(0, items.length - 1));
    let lastG = "";
    list.replaceChildren(...(items.length ? items.flatMap((it, i) => {
      const head = it.g !== lastG ? h("div", { class: "hit-group" }, (lastG = it.g)) : null;
      const btn = h("button", { type: "button", class: "hit", role: "option", "aria-selected": i === sel ? "true" : "false", onclick: () => go(it.href) },
        it.av ? avatar(it.av, "sm") : icon(it.ic), h("span", {}, it.label), it.sub ? h("span", { class: "muted small spacer" }, it.sub) : null);
      return head ? [head, btn] : [btn];
    }) : [h("p", { class: "muted small", style: "padding:14px" }, ux("noHits"))]));
  };
  input.addEventListener("input", () => { sel = 0; draw(); });
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { sel = Math.min(sel + 1, items.length - 1); draw(); e.preventDefault(); }
    else if (e.key === "ArrowUp") { sel = Math.max(sel - 1, 0); draw(); e.preventDefault(); }
    else if (e.key === "Enter" && items[sel]) go(items[sel].href);
    else if (e.key === "Escape") { e.preventDefault(); dlg.close(); }
  });
  dlg.append(h("div", { class: "pal-in" }, icon("search"), input, h("kbd", {}, "Esc")), list);
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();
  draw();
  api("GET", "/api/v2/admin/clients").then((c) => { clients = c || []; draw(); }).catch(() => {});
  if (me && me.role !== "rep") api("GET", "/api/v2/admin/nodes").then((n) => { nodes = n || []; draw(); }).catch(() => {});
}
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k" && me && !document.querySelector("dialog.palette")) { e.preventDefault(); openPalette(); }
});

// popMenu shows a small menu under a header button.
function popMenu(anchor, build) {
  document.querySelectorAll(".popmenu").forEach((m) => m.remove());
  const m = h("div", { class: "popmenu card", role: "menu" });
  build(m);
  document.body.append(m);
  const r = anchor.getBoundingClientRect();
  m.style.insetBlockStart = `${r.bottom + 8 + window.scrollY}px`;
  if (document.documentElement.dir === "rtl") m.style.left = `${Math.max(8, r.left)}px`; else m.style.right = `${Math.max(8, innerWidth - r.right)}px`;
  const off = (e) => { if (!m.contains(e.target) && !anchor.contains(e.target)) { m.remove(); document.removeEventListener("pointerdown", off); } };
  setTimeout(() => document.addEventListener("pointerdown", off), 0);
}

let notifCache = null;
async function loadNotifs() {
  if (!me || me.role === "rep") return [];
  const out = [];
  const [nodes, orders, si] = await Promise.all([api("GET", "/api/v2/admin/nodes").catch(() => []), api("GET", "/api/v2/shop/orders").catch(() => []), api("GET", "/api/v2/admin/system").catch(() => null)]);
  const now = Date.now() / 1000;
  for (const n of nodes || []) if (!n.online) out.push({ cls: "bad", text: ux("nodeDown", { x: n.Name || n.ID }), href: "#/nodes" });
  const pend = (orders || []).filter((o) => (o.status || o.Status) === "pending").length;
  if (pend) out.push({ cls: "warn", text: ux("pendingOrders", { n: fmtNum(pend) }), href: "#/shop" });
  for (const w of (si && si.warnings) || []) { const [k, p] = w.split(":"); out.push({ cls: "bad", text: s3(k === "disk" ? "diskWarn" : "ramWarn", { p: fmtNum(+p) }).split(/[.؛]/)[0], href: "#/dashboard" }); }
  notifCache = { at: now, list: out };
  return out;
}

// Sidebar groups (desktop); phones keep the same order in the tab bar.
const NAV_GROUPS = [
  ["overview", ["dashboard", "analytics", "online"]],
  ["people", ["clients", "groups", "reps", "sellers", "tenants"]],
  ["infra", ["inbounds", "nodes", "routing"]],
  ["sales", ["shop", "resale", "tickets", "credit", "finance"]],
  ["system", ["activity", "staff", "settings"]],
];
const NAV_LABELS = { fa: { overview: "نمای کلی", people: "کاربران و نماینده‌ها", infra: "زیرساخت", sales: "فروش", system: "سیستم" },
  en: { overview: "Overview", people: "People", infra: "Infrastructure", sales: "Sales", system: "System" } };
function shell(active, content) {
  const present = new Set(Object.keys(routes));
  const keys = [];
  const tabs = [];
  for (const [g, list] of NAV_GROUPS) {
    const ks = list.filter((k) => present.has(k));
    if (!ks.length) continue;
    tabs.push(h("span", { class: "nav-group", "aria-hidden": "true" }, NAV_LABELS[lang === "fa" ? "fa" : "en"][g]));
    for (const r of ks) { keys.push(r); tabs.push(h("a", { href: `#/${r}`, "aria-current": r === active ? "page" : null }, icon(r), h("span", {}, t(r)))); }
  }
  for (const r of present) if (!keys.includes(r)) { keys.push(r); tabs.push(h("a", { href: `#/${r}`, "aria-current": r === active ? "page" : null }, icon(r), h("span", {}, t(r)))); }
  // Phones: four tabs and a "more" sheet with the rest.
  const mainKeys = keys.slice(0, 4);
  const moreActive = !mainKeys.includes(active);
  const moreBtn = h("button", { type: "button", class: "more-tab", "aria-current": moreActive ? "page" : null, onclick: () => {
    const sheet = h("dialog", { class: "sheet", closedby: "any" },
      h("div", { class: "sheet-grid" }, keys.slice(4).map((r) => h("a", { href: `#/${r}`, "aria-current": r === active ? "page" : null, onclick: () => sheet.close() }, icon(r), h("span", {}, t(r))))));
    sheet.addEventListener("close", () => sheet.remove());
    document.body.append(sheet); sheet.showModal();
  } }, icon("more"), h("span", {}, ux("more")));
  const bell = h("span", { class: "dotcount", hidden: true });
  const bellBtn = iconBtn("bell", ux("notif"), (e) => {
    const list = (notifCache && notifCache.list) || [];
    popMenu(e.currentTarget, (m) => m.append(h("div", { class: "pm-head" }, ux("notif")),
      ...(list.length ? list.map((n) => h("a", { class: `pm-item ${n.cls}`, href: n.href, onclick: () => m.remove() }, h("i", { class: `dot ${n.cls === "bad" ? "off" : "warn"}` }), n.text)) : [h("p", { class: "muted small pm-empty" }, ux("allGood"))])));
  }, bell);
  loadNotifs().then((l) => { if (l.length) { bell.hidden = false; bell.textContent = fmtNum(l.length); } });
  const schemeNow = safeGet("color-scheme") || "light dark";
  const themeBtn = iconBtn(schemeNow === "light" ? "sun" : "moon", ux("theme"), (e) => popMenu(e.currentTarget, (m) => m.append(h("div", { class: "pm-head" }, ux("theme")),
    ...[["dark", "moon"], ["light", "sun"], ["light dark", "globe"]].map(([v, ic]) => h("button", { class: "pm-item", "aria-checked": (safeGet("color-scheme") || "light dark") === v ? "true" : "false", onclick: () => { setScheme(v); m.remove(); route(); } }, icon(ic), ux(v === "light dark" ? "auto" : v))))));
  const langBtn = h("button", { class: "iconbtn lang", type: "button", title: "زبان / Language", onclick: () => { lang = lang === "fa" ? "en" : "fa"; safeSet("lang", lang); applyLang(); route(); } }, lang === "fa" ? "EN" : "فا");
  const userBtn = me ? h("button", { class: "userchip", type: "button", onclick: (e) => popMenu(e.currentTarget, (m) => m.append(
    h("div", { class: "pm-head row" }, avatar(me.user), h("div", {}, h("b", {}, me.user), h("div", { class: "muted small" }, me.role === "rep" ? t("reps") : t(`role_${me.level ? "admin" : me.role}`)))),
    h("a", { class: "pm-item", href: "#/settings", onclick: () => m.remove() }, icon("settings"), t("settings")),
    h("button", { class: "pm-item bad", onclick: async () => { await api("POST", "/api/v2/auth/logout"); location.hash = "#/login"; } }, icon("logout"), t("logout")))) },
    avatar(me.user), h("span", { class: "uname" }, me.user)) : null;
  return h("div", { class: "shell" },
    h("header", { class: "top" }, h("a", { class: "brand", href: "#/dashboard" }, mark(), h("span", { class: "brand-text" }, brandText())),
      me && me.role === "rep" ? h("a", { class: "badge credit", href: "#/credit", title: t("volumeCredit") }, h("span", { class: "lbl" }, `${t("volumeCredit")}: `), h("b", { dir: "ltr" }, gbs(me.volume_credit || 0)), me.unlimited_units ? ` · ${t("units")}: ${fmtNum(me.unlimited_units)}` : "") : null,
      h("button", { class: "searchbox", type: "button", onclick: openPalette }, icon("search"), h("span", {}, ux("search")), h("kbd", {}, "Ctrl K")),
      h("div", { class: "actions" }, iconBtn("search", ux("search"), openPalette), me && me.role !== "rep" ? bellBtn : null, themeBtn, langBtn, userBtn)),
    h("nav", { class: "tabs", "aria-label": "Main" }, tabs, keys.length > 5 ? moreBtn : null),
    h("main", {}, content));
}

function viewLogin() {
  const err = h("div", { class: "error", role: "alert" });
  const user = h("input", { name: "u", autocomplete: "username", required: true, autofocus: true });
  const pass = h("input", { name: "p", type: "password", autocomplete: "current-password", required: true });
  const code = h("input", { name: "c", inputmode: "numeric", autocomplete: "one-time-code", pattern: "[0-9 ]{6,7}", maxlength: "7", dir: "ltr" });
  const codeRow = h("label", { hidden: true }, h("span", {}, t("tfaCode")), code);
  const btn = h("button", { class: "primary", type: "submit" }, t("login"));
  const eye = h("button", { type: "button", class: "ghost pw-eye", "aria-label": lang === "fa" ? "نمایش رمز" : "Show password", "aria-pressed": "false", onclick: () => {
    const show = pass.type === "password"; pass.type = show ? "text" : "password"; eye.setAttribute("aria-pressed", String(show)); eye.textContent = show ? "🙈" : "👁"; pass.focus();
  } }, "👁");
  const caps = h("div", { class: "small warn-text", hidden: true, role: "status" }, lang === "fa" ? "Caps Lock روشن است" : "Caps Lock is on");
  pass.addEventListener("keyup", (e) => { caps.hidden = !e.getModifierState?.("CapsLock"); });
  const form = h("form", { class: "form", onsubmit: async (e) => {
    e.preventDefault(); btn.disabled = true; err.textContent = "";
    try { await api("POST", "/api/v2/auth/login", { Username: user.value, Password: pass.value, Code: code.value }); location.hash = "#/dashboard"; }
    catch (x) {
      if (String(x.message).includes("need_code")) { codeRow.hidden = false; err.textContent = codeRow.dataset.asked ? t("badLogin") : t("tfaNeed"); codeRow.dataset.asked = "1"; code.value = ""; code.focus(); }
      else err.textContent = t("badLogin");
    } finally { btn.disabled = false; }
  } }, h("label", {}, h("span", {}, t("username")), user),
    h("label", {}, h("span", {}, t("password")), h("div", { class: "pw-wrap" }, pass, eye)), caps, codeRow, err, btn);
  const fa = lang === "fa";
  const langBtn = h("button", { type: "button", class: "ghost login-lang", onclick: () => { lang = fa ? "en" : "fa"; safeSet("lang", lang); applyLang(); route(); } }, fa ? "English" : "فارسی");
  return h("div", { class: "login" }, langBtn, h("div", { class: "card" }, h("h1", {}, mark(), h("span", { class: "brand-text" }, brandText())),
    h("p", { class: "muted small login-sub" }, fa ? "ورود به پنل مدیریت" : "Sign in to the control panel"), form));
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
  return h("div", { class: "card form hub-card wide-col" },
    h("div", { class: "row" }, h("h2", {}, s3("hubTitle")), h("span", { class: "badge spacer", dir: "ltr" }, `${si.version}${si.commit ? ` · ${si.commit}` : ""}`)),
    h("div", { class: "meters" },
      meter(`${s3("cpu")} (${fmtNum(si.cores)})`, si.cpu_percent, ltr(`${fmtNum(si.cpu_percent)}% · load ${fmtNum(si.load1)}`)),
      meter(s3("ram"), pct(si.ram_used, si.ram_total), ltr(`${bytes(si.ram_used)} / ${bytes(si.ram_total)}`)),
      meter(s3("disk"), pct(si.disk_used, si.disk_total), ltr(`${bytes(si.disk_used)} / ${bytes(si.disk_total)}`))),
    h("div", { class: "row small muted" },
      h("span", {}, `${s3("db")}: ${bytes(si.db_size)}`), h("span", {}, `${s3("backups", { n: fmtNum(si.backups) })}: ${bytes(si.backups_size)}`),
      h("span", { class: "spacer" }, `${s3("up")} ${dur(si.uptime_sec)}`)),
    fullOwner() ? hubUpdateRow() : null);
}

// Update the HUB by uploading a new dark-hub2 binary (checked before it replaces the old one).
function hubUpdateRow() {
  const fa = lang === "fa";
  const file = h("input", { type: "file", hidden: true });
  const out = h("span", { class: "small muted" });
  file.addEventListener("change", async () => {
    const f = file.files[0];
    if (!f) return;
    if (!(await confirmAction(fa ? `هاب با فایل ${f.name} به‌روز شود؟ اول اجرا و نسخه‌اش بررسی می‌شود؛ نسخهٔ فعلی نگه داشته می‌شود و اگر نسخهٔ جدید بالا نیاید خودکار برمی‌گردد. پنل چند ثانیه قطع می‌شود؛ نودها کار می‌کنند.` : `Update the HUB with ${f.name}? It is run and its version checked first; the current version is kept and comes back by itself if the new one does not stay up. The panel is away for a few seconds; nodes keep running.`, fa ? "به‌روزرسانی" : "Update"))) { file.value = ""; return; }
    fill(out, fa ? "در حال بارگذاری و بررسی…" : "Uploading and checking…");
    try {
      const res = await fetch("/api/v2/admin/update-hub", { method: "POST", credentials: "same-origin", body: f, headers: { "X-Dark-Request": "1", "Content-Type": "application/octet-stream" } });
      const text = await res.text();
      if (!res.ok) throw new Error(text.trim() || res.statusText);
      const r = JSON.parse(text);
      fill(out, h("span", { class: "badge good" }, `${r.from} → ${r.to}`), fa ? " ری‌استارت…" : " restarting…");
      setTimeout(() => location.reload(), 9000);
    } catch (x) { fill(out, h("span", { class: "error" }, x.message)); }
    file.value = "";
  });
  const rel = h("span", { class: "small" });
  const install = async (r) => {
    if (!(await confirmAction(fa ? `هاب به نسخهٔ ${r.tag} به‌روز شود؟ فایل از گیت‌هاب دانلود و با checksum بررسی می‌شود؛ اگر بالا نیاید خودکار به ${r.current} برمی‌گردد. فایل نود هم به‌روز می‌شود تا بعداً نودها را با یک دکمه به‌روز کنید.` : `Update the HUB to ${r.tag}? It is downloaded from GitHub and checked against its checksum; if it does not stay up, ${r.current} comes back by itself. The node binary is refreshed too so nodes can follow with one click.`, fa ? "به‌روزرسانی" : "Update"))) return;
    fill(rel, fa ? "در حال دانلود و بررسی…" : "Downloading and checking…");
    try {
      const x = await guarded(() => api("POST", "/api/v2/admin/update-hub/release"));
      fill(rel, h("span", { class: "badge good" }, `${x.from} → ${x.to}`), fa ? " ری‌استارت…" : " restarting…");
      setTimeout(() => location.reload(), 9000);
    } catch (e) { fill(rel, h("span", { class: "error" }, e.message)); }
  };
  const check = h("button", { class: "ghost", onclick: async () => {
    fill(rel, fa ? "در حال بررسی…" : "Checking…");
    try {
      const r = await api("GET", "/api/v2/admin/update-hub/release");
      fill(rel, r.newer
        ? [h("span", { class: "badge good" }, fa ? `نسخهٔ ${r.tag} آماده است` : `${r.tag} is available`), " ", h("button", { class: "primary", onclick: () => install(r) }, fa ? "نصب" : "Install")]
        : h("span", { class: "muted" }, fa ? `آخرین نسخه را دارید (${r.current})` : `Up to date (${r.current})`));
    } catch (e) { fill(rel, h("span", { class: "error" }, e.message)); }
  } }, fa ? "بررسی نسخهٔ جدید" : "Check for updates");
  return h("div", { class: "row small" }, check, rel, h("button", { class: "ghost spacer", onclick: () => file.click() }, fa ? "یا بارگذاری فایل dark-hub2" : "or upload a dark-hub2 file"), file, out);
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
        h("td", { "data-label": s3("onIPs"), class: "small" }, o.ips.length ? h("span", { class: "row", style: "gap:4px" }, o.ips.map((ip) => ipView(ip, ""))) : "—"),
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

// ---------- Charts: one kit for every chart in the panel ----------
// Drawn at the box's real pixel size (redrawn when it resizes), a numbered
// axis, and a readout under the cursor or finger that also works on phones.
// Colours go through the CSSOM (el.style), never a style attribute, so the
// panel's strict CSP (style-src 'self') does not drop them.
// o: { xs, series:[{id,name,color,values,area,dash}], mode: "lines"|"stack"|"share",
//      fmt, bin (1024 units, default true), int, max, suffix, height, partial (index),
//      xLabel(i), tipTitle(i), tipExtra(i), totals (numbers over bars), label,
//      overlay: [{name,color,values}] extra lines (null = gap) not counted in bars or the readout,
//      ref: {value,label} a dashed level such as the average, noX: no day labels }
const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];
function niceTicks(max, bin = true, int = false, suffix = "") {
  let u = 1, k = 0;
  if (bin) while (max / u >= 1024 && k < UNITS.length - 1) { u *= 1024; k++; }
  const m = Math.max(max / u, int ? 1 : 1e-9), raw = m / 4, p = 10 ** Math.floor(Math.log10(raw)), q = raw / p;
  let s = (q <= 1 ? 1 : q <= 2 ? 2 : q <= 2.5 ? 2.5 : q <= 5 ? 5 : 10) * p;
  if (int) s = Math.max(1, Math.round(s));
  const top = Math.ceil(m / s - 1e-9) * s, list = [];
  for (let v = 0; v <= top + s / 2; v += s) list.push(v * u);
  const f = bin ? (v) => `⁦${fmtNum(v / u)} ${UNITS[k]}${suffix}⁩` : (v) => `${fmtNum(v)}${suffix}`;
  return { top: top * u || 1, list, f };
}
// "سه‌شنبه ۴ آذر" / "Tue 24 Nov": the readout's title for a day.
function dayLong(day) {
  return new Intl.DateTimeFormat(lang === "fa" ? "fa-IR-u-ca-persian" : "en-GB", { weekday: "long", month: "long", day: "numeric" }).format(new Date(day + "T12:00:00Z"));
}
const utcDays = (n) => { const out = [], now = Date.now(); for (let i = n - 1; i >= 0; i--) out.push(new Date(now - i * 86400000).toISOString().slice(0, 10)); return out; };
const todayUTC = () => new Date().toISOString().slice(0, 10);

function chartKit(o) {
  const fa = lang === "fa";
  const n = o.xs.length, mode = o.mode || "lines", fmt = o.fmt || bytes;
  const fig = h("div", { class: "cfig" });
  fig.style.blockSize = `${o.height || 230}px`;
  const svg = svgEl("svg", { role: "img", "aria-label": o.label || "" });
  const tip = h("div", { class: "ctip", hidden: true });
  fig.append(svg, tip);
  const pid = `cp${++markSeq}`;
  let g = null;
  const draw = () => {
    const W = fig.clientWidth, H = fig.clientHeight;
    if (!W || !H) return;
    const S = o.series, narrow = W < 520, part = o.partial ?? -1;
    const L = narrow ? 46 : 62, R = 8, T = part >= 0 || o.totals ? 20 : 10, B = o.noX ? 6 : 22, iw = W - L - R, ih = H - T - B, bw = iw / Math.max(1, n);
    const col = (i) => S.reduce((a, s) => a + (s.values[i] || 0), 0);
    const over = o.overlay || [];
    const peak = o.max || Math.max(mode === "lines" ? Math.max(0, ...S.flatMap((s) => s.values)) : Math.max(0, ...o.xs.map((_, i) => col(i))), ...over.flatMap((s) => s.values.map((v) => v || 0)));
    const tk = mode === "share" ? { top: 1, list: [0, 0.25, 0.5, 0.75, 1], f: (v) => `${fmtNum(v * 100)}%` } : niceTicks(peak, o.bin !== false, o.int, o.suffix || "");
    if (o.axis && mode !== "share") tk.f = o.axis;
    if (ih < 90 && mode !== "share") tk.list = [0, tk.top]; // a strip: just zero and the top
    const y = (v) => T + ih - (Math.min(v, tk.top) / tk.top) * ih;
    svg.replaceChildren();
    svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
    const add = (tag, a, style) => { const e = svgEl(tag, a); if (style) Object.assign(e.style, style); svg.append(e); return e; };
    const txt = (x, yy, s, cls, anchor = "middle") => { add("text", { x, y: yy, "text-anchor": anchor, class: cls }).textContent = s; };
    const defs = add("defs", {});
    const pat = svgEl("pattern", { id: pid, width: 6, height: 6, patternUnits: "userSpaceOnUse", patternTransform: "rotate(45)" });
    pat.append(svgEl("line", { x1: 0, y1: 0, x2: 0, y2: 6, class: "c-hatch" })); defs.append(pat);
    const hl = add("rect", { x: 0, y: T, width: 0, height: ih, class: "c-hl" });
    for (const v of tk.list) { const yy = y(v); add("line", { x1: L, x2: W - R, y1: yy, y2: yy, class: "c-grid" }); txt(L - 7, yy + 3.5, v ? tk.f(v) : fmtNum(0), "c-ax", "end"); }
    const xc = (i) => L + i * bw + bw / 2;
    if (mode === "lines") {
      if (part >= 0 && part < n) { add("rect", { x: L + part * bw, y: T, width: bw, height: ih, fill: `url(#${pid})`, class: "c-part" }); txt(xc(part), T - 6, fa ? "تا الان" : "so far", "c-note"); }
      for (const s of [...S].reverse()) {
        const pts = s.values.map((v, i) => `${xc(i).toFixed(1)},${y(v || 0).toFixed(1)}`).join(" ");
        if (s.area) add("polygon", { points: `${xc(0)},${T + ih} ${pts} ${xc(n - 1)},${T + ih}`, class: "c-area" }, { fill: s.color });
        add("polyline", { points: pts, class: `c-line${s.dash ? " dash" : ""}` }, { stroke: s.color });
        if (n <= 120) add("circle", { cx: xc(n - 1), cy: y(s.values[n - 1] || 0), r: 3 }, { fill: s.color });
      }
    } else {
      const multi = S.length > 1;
      o.xs.forEach((_, i) => {
        const t = col(i);
        if (!t) return;
        let yy = T + ih;
        const x = L + i * bw + bw * 0.16, w = Math.max(2, bw * 0.68);
        for (const s of S) {
          const v = s.values[i] || 0;
          if (!v) continue;
          const hh = mode === "share" ? (v / t) * ih : (Math.min(v, tk.top) / tk.top) * ih;
          yy -= hh;
          add("rect", { x, y: yy, width: w, height: Math.max(hh - (multi ? 1 : 0), 0.8), rx: multi ? 0 : 2, class: "c-bar" }, { fill: s.color });
        }
        if (i === part) { add("rect", { x, y: yy, width: w, height: T + ih - yy, fill: `url(#${pid})` }); txt(x + w / 2, yy - 6, fa ? "تا الان" : "so far", "c-note"); }
        else if (o.totals && mode === "stack" && bw >= 34) txt(x + w / 2, yy - 5, o.totals(t), "c-val");
      });
    }
    for (const s of over) {
      let seg = [];
      const flush = () => { if (seg.length > 1) add("polyline", { points: seg.join(" "), class: "c-over" }, { stroke: s.color }); else if (seg.length) { const [cx, cy] = seg[0].split(","); add("circle", { cx, cy, r: 2.5 }, { fill: s.color }); } seg = []; };
      s.values.forEach((v, i) => { if (v == null) flush(); else seg.push(`${xc(i).toFixed(1)},${y(v).toFixed(1)}`); });
      flush();
    }
    if (o.ref?.value) { const yy = y(o.ref.value); add("line", { x1: L, x2: W - R, y1: yy, y2: yy, class: "c-ref" }); if (o.ref.label) txt(L + 6, yy - 5, o.ref.label, "c-refl", "start"); }
    const step = Math.max(1, Math.ceil(n / (narrow ? 5 : Math.max(6, Math.floor(iw / 72)))));
    if (!o.noX) for (let i = 0; i < n; i++) if ((n - 1 - i) % step === 0) txt(xc(i), H - 6, o.xLabel ? o.xLabel(i) : dayLabel(o.xs[i]), "c-ax");
    g = { L, bw, hl };
  };
  const hide = () => { tip.hidden = true; g?.hl.classList.remove("on"); };
  const show = (e) => {
    if (!g) return;
    const r = svg.getBoundingClientRect(), x = e.clientX - r.left, i = Math.floor((x - g.L) / g.bw);
    if (i < 0 || i >= n) return hide();
    g.hl.setAttribute("x", g.L + i * g.bw); g.hl.setAttribute("width", g.bw); g.hl.classList.add("on");
    const rows = o.series.map((s) => ({ s, v: s.values[i] || 0 }));
    const list = mode === "lines" ? rows : rows.filter((x) => x.v).sort((a, b) => b.v - a.v);
    const tot = rows.reduce((a, x) => a + x.v, 0), many = o.series.length > 2;
    fill(tip,
      h("b", {}, o.tipTitle ? o.tipTitle(i) : dayLong(o.xs[i]), i === o.partial ? (fa ? " · تا الان" : " · so far") : ""),
      o.series.length > 1 && (mode !== "lines" || o.total) ? h("div", { class: "ct-row tot" }, h("span", {}, fa ? "مجموع" : "Total"), h("span", { class: "v" }, fmt(tot))) : null,
      list.length ? list.map((x) => { const sw = h("i", {}); sw.style.background = x.s.color;
        return h("div", { class: "ct-row" }, sw, h("span", {}, x.s.name), h("span", { class: "v" }, fmt(x.v)), many && tot && mode !== "lines" ? h("em", {}, `${fmtNum(Math.round((x.v / tot) * 100))}%`) : h("em", {})); })
        : h("div", { class: "muted" }, fa ? "چیزی ثبت نشده" : "Nothing recorded"),
      o.tipExtra ? o.tipExtra(i) : null);
    tip.hidden = false;
    const tw = tip.offsetWidth;
    let left = x + 14;
    if (left + tw > r.width) left = x - tw - 14;
    tip.style.left = `${Math.max(0, left)}px`;
    tip.style.top = "4px";
  };
  svg.addEventListener("pointermove", show);
  svg.addEventListener("pointerdown", show);
  svg.addEventListener("pointerleave", (e) => { if (e.pointerType === "mouse") hide(); });
  new ResizeObserver(draw).observe(fig);
  return fig;
}

// Swatch legend; with onToggle each entry hides or shows its series.
function chartLegend(items, onToggle, hidden = new Set()) {
  const fa = lang === "fa";
  return h("div", { class: "clegend" }, items.map((it) => {
    const sw = h("i", {}); sw.style.background = it.color;
    const off = hidden.has(it.id);
    return onToggle
      ? h("button", { type: "button", class: off ? "off" : "", "aria-pressed": String(!off), onclick: () => onToggle(it.id) }, sw, it.name, it.value ? h("span", {}, it.value) : null)
      : h("span", {}, sw, it.name, it.value ? h("span", {}, it.value) : null);
  }), onToggle && hidden.size ? h("button", { type: "button", onclick: () => onToggle("*") }, fa ? "نمایش همه" : "Show all") : null);
}

// ---------- Dashboard: traffic per day ----------
// Range 7–90 days, stacked download/upload (or lines), the period's average,
// the period before it as a dotted line, four figures, and active users per
// day. It asks the HUB for twice the range: the first half is the period before.
function dashTrafficCard(ins0) {
  const fa = lang === "fa";
  let n = 14, view = "bars";
  try { n = +localStorage.getItem("dashDays") || 14; view = localStorage.getItem("dashView") || "bars"; } catch {}
  if (![7, 14, 30, 90].includes(n)) n = 14;
  const title = h("h2", {});
  const body = h("div", { class: "form" });
  let all = n === 14 ? ins0?.daily || [] : [];
  const render = () => {
    const today = todayUTC();
    const cur = all.slice(-n), prev = all.length >= 2 * n ? all.slice(-2 * n, -n) : [];
    title.textContent = fa ? `ترافیک ${fmtNum(n)} روز اخیر` : `Traffic, last ${n} days`;
    if (!cur.length) { fill(body, h("p", { class: "muted small" }, t("noData"))); return; }
    const tot = (d) => (d ? d.Up + d.Down : 0);
    const full = cur.filter((d) => d.Day !== today && tot(d) > 0);
    const sum = cur.reduce((a, d) => a + tot(d), 0);
    const prevFull = prev.length === n && prev.every((d) => tot(d) > 0), psum = prev.reduce((a, d) => a + tot(d), 0);
    const avg = full.length ? full.reduce((a, d) => a + tot(d), 0) / full.length : 0;
    const peak = full.reduce((a, d) => (tot(d) > tot(a) ? d : a), full[0]);
    const todayRow = cur.find((d) => d.Day === today);
    const pct = (a, b) => Math.round(((a - b) / b) * 100);
    const chg = (p) => h("span", { class: `chg ${p >= 0 ? "up" : "down"}`, dir: "ltr" }, `${p > 0 ? "+" : p < 0 ? "−" : ""}${fmtNum(Math.abs(p))}%`);
    const kpi = (v, k, extra) => h("div", { class: "ckpi" }, h("b", {}, v, extra || null), h("span", {}, k));
    const xs = cur.map((d) => d.Day), part = xs.indexOf(today);
    const series = [
      { id: "d", name: `↓ ${t("inRate")}`, color: "var(--ion)", area: true, values: cur.map((d) => d.Down) },
      { id: "u", name: `↑ ${t("outRate")}`, color: "var(--ember)", dash: true, values: cur.map((d) => d.Up) },
    ];
    const hasPrev = prev.some((d) => tot(d) > 0);
    const extra = (i) => {
      const d = cur[i], p = prev[i], v = tot(d), row = (k, val) => h("div", { class: "ct-row ex" }, h("span", {}, k), h("span", { class: "v" }, val));
      return [
        row(fa ? "کاربران فعال" : "Active users", fmtNum(d.Users || 0)),
        avg && v && d.Day !== today ? row(fa ? "نسبت به میانگین" : "Of the average", `${fmtNum(Math.round((v / avg) * 100))}%`) : null,
        p && v && d.Day !== today && tot(p) >= avg * 0.1 ? row(fa ? `در برابر ${dayLabel(p.Day)}` : `vs ${dayLabel(p.Day)}`, (() => { const x = pct(v, tot(p)); return `⁦${x > 0 ? "+" : x < 0 ? "−" : ""}${fmtNum(Math.abs(x))}%⁩`; })()) : null,
      ];
    };
    const users = cur.map((d) => d.Users || 0), fullUsers = full.map((d) => d.Users || 0);
    const leg = (cls, label) => h("span", { class: "legend-item" }, h("i", { class: cls }), label);
    fill(body,
      h("div", { class: "ckpis" },
        kpi(bytes(sum), prevFull ? (fa ? "مجموع بازه، در برابر دورهٔ قبل" : "Total, against the period before") : (fa ? "مجموع بازه" : "Total"), prevFull && psum ? chg(pct(sum, psum)) : null),
        kpi(bytes(avg), fa ? "میانگین هر روز کامل" : "Average full day"),
        kpi(peak ? bytes(tot(peak)) : "—", peak ? (fa ? `پرترافیک‌ترین روز: ${dayLong(peak.Day)}` : `Busiest day: ${dayLong(peak.Day)}`) : (fa ? "پرترافیک‌ترین روز" : "Busiest day")),
        kpi(bytes(tot(todayRow)), fa ? `امروز تا الان · ${fmtNum(todayRow?.Users || 0)} کاربر فعال` : `Today so far · ${todayRow?.Users || 0} active users`)),
      h("div", { class: "row small dleg" },
        leg("sw down", `↓ ${t("inRate")}`), leg("sw up", `↑ ${t("outRate")}`), leg("ln avg", fa ? "میانگین روزانه" : "Daily average"),
        hasPrev ? leg("ln prev", fa ? "دورهٔ قبل" : "Period before") : null),
      chartKit({ xs, series, mode: view === "lines" ? "lines" : "stack", partial: part, height: 270, label: title.textContent, tipExtra: extra, total: true,
        overlay: hasPrev ? [{ name: "prev", color: "var(--muted)", values: prev.map((d) => (tot(d) > 0 ? tot(d) : null)) }] : [],
        ref: avg ? { value: avg, label: `${fa ? "میانگین" : "avg"} ${bytes(avg)}` } : null }),
      h("div", { class: "row small" }, h("strong", {}, fa ? "کاربران فعال هر روز" : "Active users per day"),
        fullUsers.length ? h("span", { class: "muted spacer" }, fa ? `میانگین ${fmtNum(Math.round(fullUsers.reduce((a, b) => a + b, 0) / fullUsers.length))} · بیشترین ${fmtNum(Math.max(...fullUsers))}` : `avg ${Math.round(fullUsers.reduce((a, b) => a + b, 0) / fullUsers.length)} · peak ${Math.max(...fullUsers)}`) : null),
      chartKit({ xs, mode: "lines", bin: false, int: true, height: 100, noX: true, partial: -1, label: fa ? "کاربران فعال هر روز" : "Active users per day",
        series: [{ id: "n", name: fa ? "کاربران فعال" : "Active users", color: "var(--sky)", area: true, values: users }], fmt: (v) => fmtNum(v) }),
      h("a", { class: "small", href: "#/nodes" }, fa ? "تفکیک بر اساس نود ›" : "By node ›"));
  };
  const load = async () => {
    if (all.length < 2 * n) {
      fill(body, h("p", { class: "muted small" }, "…"));
      try { all = (await api("GET", `/api/v2/admin/insights?days=${2 * n}`)).daily || []; } catch { all = []; }
    }
    render();
  };
  const seg = (list, cur, set) => {
    const box = h("div", { class: "row cseg" });
    for (const [v, l] of list) box.append(h("button", { type: "button", class: v === cur ? "chip on" : "chip", "aria-pressed": String(v === cur), onclick: (e) => {
      box.querySelectorAll("button").forEach((b) => { b.classList.toggle("on", b === e.currentTarget); b.setAttribute("aria-pressed", String(b === e.currentTarget)); });
      set(v);
    } }, l));
    return box;
  };
  const views = seg([["bars", fa ? "ستونی" : "Bars"], ["lines", fa ? "خطی" : "Lines"]], view, (v) => { view = v; try { localStorage.setItem("dashView", v); } catch {} render(); });
  const ranges = seg([7, 14, 30, 90].map((d) => [d, fa ? `${fmtNum(d)} روز` : `${d} days`]), n, (v) => { n = v; try { localStorage.setItem("dashDays", v); } catch {} all = []; load(); });
  load();
  return h("div", { class: "card form span2 dash-traffic" }, h("div", { class: "row" }, title, h("span", { class: "spacer" }), views, ranges), body);
}

// ---------- Traffic per node ----------
// One colour per node, in the order of the node list, so a node keeps its
// colour; none is yellow. Past the palette the hue is generated (skipping
// yellow); a node that is gone is grey.
const NODE_COLORS = ["#4f8cff", "#ff8a4c", "#2fd39b", "#b36cff", "#22d3ee", "#ff5470", "#f472b6", "#14b8a6", "#8fa2ff", "#e879f9", "#c08457", "#7dd3a8"];
function nodeColorOf(nodes) {
  const order = nodes.map((n) => n.ID);
  return (id) => {
    const i = order.indexOf(id);
    if (i < 0) return "#6b7488";
    if (i < NODE_COLORS.length) return NODE_COLORS[i];
    let hue = (i * 137.5) % 360;
    if (hue > 35 && hue < 85) hue += 60;
    return `hsl(${hue.toFixed(0)} 70% 62%)`;
  };
}

// opt.url(days) and opt.title/opt.hint let a connected shop show the same
// chart for its own clients only (viewTenants).
function nodeTrafficCard(nodes, opt = {}) {
  const fa = lang === "fa";
  const url = opt.url || ((d) => `/api/v2/admin/nodes/traffic?days=${d}`);
  const body = h("div", { class: "form" });
  let days = 14, mode = "stack", rows = [];
  try { days = +sessionStorage.getItem("ntDays") || 14; mode = sessionStorage.getItem("ntMode") || "stack"; } catch {}
  const hidden = new Set();
  const color = nodeColorOf(nodes);
  const nameOf = (id) => { const n = nodes.find((x) => x.ID === id); return n ? n.Name || n.ID : `${id} (${fa ? "حذف‌شده" : "removed"})`; };
  const render = () => {
    const dayList = utcDays(days), today = todayUTC();
    const by = {};
    for (const r of rows) (by[r.day] ||= {})[r.node_id] = (r.up || 0) + (r.down || 0);
    const val = (d, id) => (by[d] || {})[id] || 0;
    const tot = {};
    for (const d of dayList) for (const [id, b] of Object.entries(by[d] || {})) tot[id] = (tot[id] || 0) + b;
    const ids = Object.keys(tot).filter((id) => tot[id] > 0).sort((a, b) => tot[b] - tot[a]);
    if (!ids.length) { fill(body, h("p", { class: "muted small" }, fa ? "در این بازه مصرفی ثبت نشده." : "No traffic in this range.")); return; }
    const sum = ids.reduce((a, id) => a + tot[id], 0);
    const full = dayList.filter((d) => d !== today && by[d]);
    const dayTot = (d) => Object.values(by[d] || {}).reduce((a, b) => a + b, 0);
    const avg = full.length ? full.reduce((a, d) => a + dayTot(d), 0) / full.length : 0;
    const kpi = (v, k) => h("div", { class: "ckpi" }, h("b", {}, v), h("span", {}, k));
    const series = ids.filter((id) => !hidden.has(id)).map((id) => ({ id, name: nameOf(id), color: color(id), values: dayList.map((d) => val(d, id)) }));
    const toggle = (id) => { if (id === "*") hidden.clear(); else if (hidden.has(id)) hidden.delete(id); else hidden.add(id); render(); };
    const top = ids[0], mx = tot[top];
    fill(body,
      h("div", { class: "ckpis" },
        kpi(bytes(sum), fa ? `مجموع ${fmtNum(days)} روز` : `${days} days in all`),
        kpi(bytes(avg), fa ? "میانگین هر روز کامل" : "Average full day"),
        kpi(bytes(dayTot(today)), fa ? "امروز تا الان" : "Today so far"),
        kpi(nameOf(top), fa ? `پرمصرف‌ترین · ${fmtNum(Math.round((tot[top] / sum) * 100))}%` : `Busiest · ${Math.round((tot[top] / sum) * 100)}%`)),
      series.length ? chartKit({ xs: dayList, series, mode, partial: dayList.indexOf(today), height: 290, label: s3("ntTitle") })
        : h("p", { class: "muted small" }, fa ? "همهٔ نودها پنهان‌اند." : "Every node is hidden."),
      chartLegend(ids.map((id) => ({ id, name: nameOf(id), color: color(id), value: bytes(tot[id]) })), toggle, hidden),
      h("div", { class: "ctable-wrap" }, h("table", { class: "ctable" },
        h("thead", {}, h("tr", {}, [[fa ? "نود" : "Node"], [fa ? "مجموع" : "Total", "num"], [fa ? "سهم" : "Share", "num"], [fa ? "میانگین روزانه" : "Daily average", "num avg"], [fa ? "امروز تا الان" : "Today so far", "num"], ["", "cshare"]].map(([x, c]) => h("th", { class: c }, x)))),
        h("tbody", {}, ids.map((id) => {
          const act = full.filter((d) => val(d, id)), a = act.length ? act.reduce((s, d) => s + val(d, id), 0) / act.length : 0, td = val(today, id);
          const sw = h("i", { class: "csw" }); sw.style.background = color(id);
          const bar = h("i", {}); bar.style.inlineSize = `${((tot[id] / mx) * 100).toFixed(1)}%`; bar.style.background = color(id);
          return h("tr", {},
            h("td", {}, sw, nameOf(id), " ", h("span", { class: "muted small cid", dir: "ltr" }, id)),
            h("td", { "data-label": fa ? "مجموع" : "Total", class: "num" }, bytes(tot[id])),
            h("td", { "data-label": fa ? "سهم" : "Share", class: "num" }, `${fmtNum(Math.round((tot[id] / sum) * 1000) / 10)}%`),
            h("td", { "data-label": fa ? "میانگین روزانه" : "Daily average", class: "num avg" }, a ? bytes(a) : "—"),
            h("td", { "data-label": fa ? "امروز تا الان" : "Today so far", class: "num" }, td ? bytes(td) : "—"),
            h("td", { class: "cshare" }, bar));
        })))));
  };
  const load = async () => {
    rows = await api("GET", url(days)).catch(() => []);
    render();
  };
  const seg = (list, cur, set) => {
    const box = h("div", { class: "row cseg" });
    for (const [v, l] of list) box.append(h("button", { type: "button", class: v === cur ? "chip on" : "chip", "aria-pressed": String(v === cur), onclick: (e) => {
      box.querySelectorAll("button").forEach((b) => { b.classList.toggle("on", b === e.currentTarget); b.setAttribute("aria-pressed", String(b === e.currentTarget)); });
      set(v);
    } }, l));
    return box;
  };
  const modes = seg([["stack", fa ? "انباشته" : "Stacked"], ["lines", fa ? "خطی" : "Lines"], ["share", fa ? "سهم ٪" : "Share %"]], mode, (v) => { mode = v; try { sessionStorage.setItem("ntMode", v); } catch {} render(); });
  const ranges = seg([7, 14, 30].map((n) => [n, s3("ntDays", { n: fmtNum(n) })]), days, (v) => { days = v; try { sessionStorage.setItem("ntDays", v); } catch {} load(); });
  load();
  return h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, opt.title || s3("ntTitle")), h("span", { class: "spacer" }), modes, ranges),
    opt.top || null,
    h("p", { class: "small muted" }, opt.hint ? opt.hint : fa ? "مجموع مصرف کاربرها روی هر نود در هر روز. موس یا انگشت را روی هر روز ببرید تا عدد هر نود را ببینید؛ با زدن نام هر نود آن را پنهان کنید." : "Client traffic summed per node and day. Point or tap at a day for each node's figure; tap a name to hide or show it."),
    body);
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

// Services that end within three days or have under a tenth of their volume
// left: the people to remind or renew today.
function endingSoonCard(list) {
  const fa = lang === "fa";
  const now = Date.now() / 1000;
  const rows = list.filter((c) => c.Enabled && !(c.VolumeBytes > 0 && c.Remaining <= 0)).map((c) => {
    const days = c.EffectiveUntil > 0 ? (c.EffectiveUntil - now) / 86400 : Infinity;
    const share = c.VolumeBytes > 0 ? c.Remaining / c.VolumeBytes : 1;
    return { c, days, share, score: Math.min(days / 3, share * 10) };
  }).filter((x) => x.days > 0 && (x.days <= 3 || x.share < 0.1)).sort((a, b) => a.score - b.score);
  return h("div", { class: "card form" }, h("div", { class: "row" }, h("h2", {}, fa ? "رو به پایان" : "Ending soon"), rows.length ? h("span", { class: "badge warn" }, fmtNum(rows.length)) : null),
    rows.length ? h("div", { class: "form" }, rows.slice(0, 8).map(({ c, days, share }) => h("a", { class: "row small end-row", href: `#/client?e=${encodeURIComponent(c.Email)}` },
      h("span", { class: "name" }, c.Email), h("span", { class: "spacer" }),
      days <= 3 ? h("span", { class: `badge ${days < 1 ? "bad" : "warn"}` }, days < 1 ? (fa ? "امروز" : "today") : fa ? `${fmtNum(Math.ceil(days))} روز` : `${Math.ceil(days)} d`) : null,
      share < 0.1 ? h("span", { class: "badge warn", dir: "ltr" }, bytes(c.Remaining)) : null)),
      rows.length > 8 ? h("a", { class: "small", href: "#/clients" }, fa ? `و ${fmtNum(rows.length - 8)} مورد دیگر…` : `and ${rows.length - 8} more…`) : null)
      : h("p", { class: "muted small" }, fa ? "سرویسی در سه روز آینده تمام نمی‌شود." : "Nothing ends in the next three days."));
}

// gauge: a ring with a percentage, a label and a detail line.
function gauge(label) {
  const ring = h("div", { class: "ring" }, h("b", {}, "–"));
  const v = h("div", { class: "g-v" }, "");
  const el = h("div", { class: "gauge" }, ring, h("div", {}, h("div", { class: "g-k" }, label), v));
  return { el, set(p, detail) {
    p = Math.max(0, Math.min(100, p || 0));
    ring.style.setProperty("--p", p.toFixed(1));
    ring.style.setProperty("--c", p >= 90 ? "var(--bad)" : p >= 75 ? "var(--warn)" : "var(--ion)");
    ring.firstChild.textContent = `\u2066${fmtNum(Math.round(p))}%\u2069`;
    v.textContent = detail || "";
  } };
}

// netMap is the dashboard's hero, a live tunnel matrix after DARK NOC: the
// hub in the middle and each node on a three-layer line (aura, backbone,
// bright core) with packets whose speed and glow follow its live traffic;
// a node that is down sits on a broken red line. Phones get a spine layout.
function netMap(nodes0, si0) {
  const fa = lang === "fa";
  const box = h("div", {});
  const sum = h("span", { class: "muted" });
  const el = h("section", { class: "card netmap", "aria-label": fa ? "نقشهٔ شبکه" : "Network map" },
    h("div", { class: "nm-head" }, h("h2", {}, fa ? "نقشهٔ زندهٔ شبکه" : "Live network map"), sum,
      h("a", { class: "spacer small", href: "#/nodes" }, fa ? "مدیریت نودها ›" : "Manage nodes ›")), box);
  const draw = (nodes) => {
    const ns = (nodes || []).slice().sort((a, b) => (b.online - a.online) || String(a.Name || a.ID).localeCompare(String(b.Name || b.ID)));
    const narrow = (el.clientWidth || window.innerWidth) < 640;
    const n = ns.length, rows = Math.max(1, Math.ceil(n / 2));
    const W = narrow ? 360 : 1000, BW = narrow ? 156 : 236, BH = narrow ? 58 : 76, RS = narrow ? 74 : 90;
    const H = narrow ? 128 + rows * RS : Math.max(230, rows * RS + 30);
    const hub = narrow ? { x: W / 2, y: 54 } : { x: W / 2, y: H / 2 };
    const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": fa ? "هاب و نودها" : "HUB and nodes" });
    const defs = svgEl("defs", {});
    const f = svgEl("filter", { id: "nm-glow", x: "-50%", y: "-50%", width: "200%", height: "200%" });
    f.append(svgEl("feGaussianBlur", { stdDeviation: "2.2", result: "b" }));
    const m = svgEl("feMerge", {}); m.append(svgEl("feMergeNode", { in: "b" }), svgEl("feMergeNode", { in: "SourceGraphic" })); f.append(m);
    defs.append(f); svg.append(defs);
    for (let gx = 20; gx < W; gx += 40) svg.append(svgEl("line", { x1: gx, y1: 0, x2: gx, y2: H, class: "nm-grid", "stroke-dasharray": "1 39" }));
    const links = svgEl("g", {}), boxes = svgEl("g", {});
    svg.append(links, boxes);
    const rtl = document.documentElement.dir === "rtl";
    const iso = (x) => (fa ? `⁧${x}⁩` : x);
    const txt = (x, y, anchor, cls, v) => { const e = svgEl("text", { x, y, "text-anchor": anchor, class: cls, direction: "ltr" }); e.textContent = v; return e; };
    let up = 0, users = 0;
    ns.forEach((nd, i) => {
      const host = nd.Host || {}, live = !!nd.online;
      if (live) { up++; users += host.online_users || 0; }
      let bx, by, ax, ay, d;
      if (narrow) {
        const col = i % 2, row = Math.floor(i / 2);
        bx = col ? W - 10 - BW : 10; by = 118 + row * RS;
        ax = col ? bx : bx + BW; ay = by + BH / 2;
        d = `M${hub.x} ${hub.y + 30} L ${hub.x} ${ay - 12} Q ${hub.x} ${ay}, ${hub.x + (ax > hub.x ? 12 : -12)} ${ay} L ${ax} ${ay}`;
      } else {
        const side = i % 2, row = Math.floor(i / 2), rowsSide = side ? Math.floor(n / 2) : Math.ceil(n / 2);
        by = (H - rowsSide * RS) / 2 + row * RS + (RS - BH) / 2;
        bx = side ? W - 20 - BW : 20;
        ax = side ? bx : bx + BW; ay = by + BH / 2;
        const mx = (hub.x + ax) / 2;
        d = `M${hub.x} ${hub.y} C ${mx} ${hub.y}, ${mx} ${ay}, ${ax} ${ay}`;
      }
      const r = (host.rx_rate || 0) + (host.tx_rate || 0);
      const lg = Math.log10(r + 1), energy = Math.max(0, Math.min(1, (lg - 3) / 4)); // 1 KB/s → 0, 10 MB/s → 1
      const g = svgEl("g", { class: `nm-route ${live ? "" : "down"}` });
      g.style.setProperty("--e", energy.toFixed(2));
      if (live) {
        g.append(svgEl("path", { d, class: "nm-link aura" }), svgEl("path", { d, class: "nm-link base" }), svgEl("path", { d, class: "nm-link core" }));
        if (r > 0) {
          const dur = Math.max(0.8, 5 - lg / 1.6);
          const fl = svgEl("path", { d, class: "nm-link flow" });
          fl.style.setProperty("animation-duration", `${(dur / 1.6).toFixed(2)}s`);
          g.append(fl);
          for (let k = 0; k < (energy > .5 ? 2 : 1); k++) {
            const pk = svgEl("circle", { r: 2.4, class: "nm-packet" });
            const mo = svgEl("animateMotion", { dur: `${dur.toFixed(2)}s`, repeatCount: "indefinite", path: d, begin: `${(-k * dur / 2).toFixed(2)}s` });
            pk.append(mo); g.append(pk);
          }
        }
      } else g.append(svgEl("path", { d, class: "nm-link dead" }));
      links.append(g);
      const a = svgEl("a", { href: "#/nodes", class: `nm-node ${live ? "up" : "down"}` });
      const pad = 12, sx = rtl ? bx + BW - pad : bx + pad, ex = rtl ? bx + pad : bx + BW - pad;
      const onl = live ? iso(`${fmtNum(host.online_users || 0)} ${fa ? "آنلاین" : "online"}`) : iso(fa ? "قطع" : "down");
      const ip = nd.PublicIP || "";
      const parts = [svgEl("rect", { x: bx, y: by, width: BW, height: BH, rx: 6, class: "box" }),
        svgEl("circle", { cx: rtl ? bx + BW - 8 : bx + 8, cy: by + 8, r: 2.6, class: "led" })];
      if (narrow) {
        parts.push(txt(bx + BW / 2, by + 24, "middle", "nm-name", nd.Name || nd.ID), txt(bx + BW / 2, by + 44, "middle", live ? "nm-users" : "nm-sub", onl));
      } else {
        parts.push(txt(sx, by + 23, rtl ? "end" : "start", "nm-name", nd.Name || nd.ID), txt(ex, by + 23, rtl ? "start" : "end", "nm-users", onl));
        if (live) parts.push(txt(sx, by + 44, rtl ? "end" : "start", "nm-sub", `↓ ${rate(host.rx_rate || 0)}   ↑ ${rate(host.tx_rate || 0)}`));
        if (ip) {
          const iw = ip.length * 6.1 + 10, ix = rtl ? bx + BW - pad - iw : bx + pad;
          parts.push(svgEl("rect", { x: ix, y: by + 52, width: iw, height: 16, rx: 3, class: "ip" }), txt(ix + iw / 2, by + 64, "middle", "nm-ip", ip));
        }
        if (live && host.cpu_percent != null) parts.push(txt(ex, by + 64, rtl ? "start" : "end", "nm-sub", `cpu ${Math.round(host.cpu_percent || 0)}%`));
      }
      a.append(...parts);
      boxes.append(a);
    });
    const g = svgEl("g", { class: "nm-hub" });
    const hexPts = (r) => [0, 1, 2, 3, 4, 5].map((k) => { const ang = Math.PI / 3 * k - Math.PI / 2; return `${(hub.x + r * Math.cos(ang)).toFixed(1)},${(hub.y + r * Math.sin(ang)).toFixed(1)}`; }).join(" ");
    g.append(svgEl("circle", { cx: hub.x, cy: hub.y, r: 34, class: "halo" }), svgEl("circle", { cx: hub.x, cy: hub.y, r: 42, class: "orbit" }),
      svgEl("polygon", { points: hexPts(30), class: "hex", "stroke-linejoin": "round" }));
    const ht = svgEl("text", { x: hub.x, y: hub.y + 4, "text-anchor": "middle", "font-size": 12 }); ht.textContent = "HUB";
    g.append(ht);
    svg.append(g);
    box.replaceChildren(svg, tunnelStrip(ns));
    sum.textContent = fa ? `${fmtNum(up)}/${fmtNum(n)} نود روشن · ${fmtNum(users)} کاربر آنلاین` : `${fmtNum(up)}/${fmtNum(n)} nodes up · ${fmtNum(users)} online`;
  };
  let last = nodes0;
  requestAnimationFrame(() => draw(last));
  draw(nodes0);
  return { el, update: (nodes) => { last = nodes; draw(nodes); } };
}

// tunnelStrip: every tunnel ending on a node, with its health, under the map.
function tunnelStrip(nodes) {
  const fa = lang === "fa";
  const items = [];
  for (const nd of nodes || []) for (const tn of (nd.online && nd.Host && nd.Host.tunnels) || []) items.push({ nd, tn });
  if (!items.length) return h("span", {});
  const ago = (u) => { const m = Math.max(0, Math.round((Date.now() / 1000 - u) / 60)); return m < 60 ? `${fmtNum(m)} ${fa ? "دقیقه" : "min"}` : m < 1440 ? `${fmtNum(Math.round(m / 60))} ${fa ? "ساعت" : "h"}` : `${fmtNum(Math.round(m / 1440))} ${fa ? "روز" : "d"}`; };
  items.sort((a, b) => (a.tn.up - b.tn.up) || String(a.tn.name).localeCompare(String(b.tn.name)));
  return h("div", { class: "tun-strip", role: "list", "aria-label": fa ? "تونل‌ها" : "Tunnels" },
    h("span", { class: "tun-title" }, fa ? "تونل‌ها" : "Tunnels"),
    items.map(({ nd, tn }) => h("span", { class: `tun ${tn.up ? "up" : "down"}`, role: "listitem",
      title: [tn.iface, tn.peer, tn.remote].filter(Boolean).join(" · ") + (tn.since ? ` · ${fa ? "از" : "since"} ${ago(tn.since)}` : "") },
      h("span", { class: "dot" }), h("b", { dir: "ltr" }, tn.name), h("span", { class: "muted" }, "→ ", nd.Name || nd.ID),
      h("span", { dir: "ltr", class: tn.up ? (tn.loss > 0 ? "warn" : "") : "bad" }, tn.up ? `${tn.rtt_ms > 0 ? `${Math.round(tn.rtt_ms)}ms` : (fa ? "وصل" : "up")}${tn.loss > 0 ? ` · ${Math.round(tn.loss)}%` : ""}` : (fa ? `قطع · ${ago(tn.since)}` : `down · ${ago(tn.since)}`)))));
}

function hubStrip(si) {
  const fa = lang === "fa";
  const cpu = gauge(t("cpu")), ram = gauge(t("ram")), disk = gauge(t("disk"));
  const down = h("b", {}, "–"), up = h("b", {}, "–");
  const meta = h("span", { class: "hs-meta" });
  const el = h("section", { class: "hub-strip", "aria-label": fa ? "وضعیت هاب" : "HUB status" },
    h("div", { class: "hs-head" }, h("span", { class: "hs-title" }, h("span", { class: "live-dot" }), fa ? "هاب" : "HUB"), meta),
    h("div", { class: "hs-body" }, cpu.el, ram.el, disk.el,
      h("div", { class: "netbox" }, h("div", { class: "nb down" }, h("span", { class: "arr" }, "↓"), down),
        h("div", { class: "nb up" }, h("span", { class: "arr" }, "↑"), up))));
  const update = (x) => {
    if (!x) return;
    cpu.set(x.cpu_percent, `${fmtNum(x.cores)} ${fa ? "هسته" : "cores"} · load ${(+x.load1 || 0).toFixed(2)}`);
    ram.set(x.ram_total ? (100 * x.ram_used) / x.ram_total : 0, `${bytes(x.ram_used)} / ${bytes(x.ram_total)}`);
    disk.set(x.disk_total ? (100 * x.disk_used) / x.disk_total : 0, `${bytes(x.disk_used)} / ${bytes(x.disk_total)}`);
    down.textContent = rate(x.rx_rate || 0); up.textContent = rate(x.tx_rate || 0);
    meta.textContent = [`v${String(x.version || "").replace(/^v/, "")}`, `${fa ? "روشن" : "up"} ${dur(x.uptime_sec)}`,
      x.backups ? `${fmtNum(x.backups)} ${fa ? "بکاپ" : "backups"}` : null].filter(Boolean).join(" · ");
  };
  update(si);
  return { el, update };
}

// upShort is uptime in a node row: "9d 22h", "5h 10m".
function upShort(sec) {
  const d = Math.floor(sec / 86400), hh = Math.floor((sec % 86400) / 3600), mm = Math.floor((sec % 3600) / 60);
  return d ? `${d}d ${hh}h` : hh ? `${hh}h ${mm}m` : `${mm}m`;
}

function nodeRow(n) {
  const fa = lang === "fa";
  const host = n.Host || {};
  const pct = (u, t) => (t ? (100 * u) / t : 0);
  const ramP = pct(host.ram_used, host.ram_total), diskP = pct(host.disk_used, host.disk_total);
  const mini = (k, p, txt) => h("div", { class: "mini" }, h("div", {}, k, " ", h("b", {}, txt)),
    h("div", { class: "bar" }, h("i", { class: p >= 90 ? "bad" : p >= 75 ? "warn" : "", style: `inline-size:${Math.round(Math.max(0, Math.min(100, p)))}%` })));
  const p = (v) => `\u2066${fmtNum(Math.round(v || 0))}%\u2069`;
  return h("a", { class: `node-row ${n.online ? "" : "off"}`, href: "#/nodes" },
    h("div", { class: "nr-name" }, h("span", { class: `dot ${n.online ? "on" : ""}` }),
      h("div", { style: "min-inline-size:0" }, h("span", {}, n.Name || n.ID, n.PublicIP ? h("span", { class: "nr-ip" }, n.PublicIP) : null),
        h("div", { class: "nr-sub" }, [n.ID, n.AgentVersion || "—", n.xray_version ? `xray ${n.xray_version}` : null].filter(Boolean).join(" · "), n.update_available ? (fa ? " · به‌روزرسانی" : " · update") : "",
          n.online && host.hub_rtt_ms ? h("span", { class: `tun-chip ${host.hub_fails_1h ? "down" : "up"}`, dir: "ltr", title: fa ? "ارتباط این نود با سرور اصلی؛ قطعی‌ها = همگام‌سازی‌های ناموفق در ساعت اخیر" : "Link to the HUB; failures = failed syncs in the last hour" },
            `⇆ hub ${Math.round(host.hub_rtt_ms)}ms${host.hub_fails_1h ? ` · ${host.hub_fails_1h}✕/1h` : ""}`) : null,
          ...(n.online ? host.tunnels || [] : []).map((tn) => h("span", { class: `tun-chip ${tn.up ? "up" : "down"}`, dir: "ltr" }, `⇄ ${tn.name} ${tn.up ? (tn.rtt_ms > 0 ? `${Math.round(tn.rtt_ms)}ms` : "✓") : "✕"}`))))),
    mini("CPU", host.cpu_percent || 0, p(host.cpu_percent)),
    mini(fa ? "رم" : "RAM", ramP, p(ramP)),
    mini(fa ? "دیسک" : "Disk", diskP, p(diskP)),
    h("div", { class: "nr-load" }, h("span", {}, "load ", h("b", {}, (+host.load1 || 0).toFixed(2))), h("span", {}, "up ", h("b", {}, host.uptime_sec ? upShort(host.uptime_sec) : "—"))),
    h("div", { class: "nr-net" }, h("span", { class: "d" }, "↓ "), rate(host.rx_rate || 0), "  ", h("span", { class: "u" }, "↑ "), rate(host.tx_rate || 0)),
    h("div", { class: "nr-users" }, h("b", {}, fmtNum(host.online_users || 0)), " ", fa ? "آنلاین" : "online"));
}

async function viewDashboard() {
  const fa = lang === "fa";
  const [o, nodes, ins] = await Promise.all([api("GET", "/api/v2/admin/overview"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/insights?days=28")]);
  const [si, cls] = await Promise.all([api("GET", "/api/v2/admin/system").catch(() => null), api("GET", "/api/v2/admin/clients").catch(() => [])]);
  const kpi = (ic, k, v) => h("div", { class: "card kpi" }, h("div", { class: "k" }, icon(ic), k), h("div", { class: "v" }, v));
  const frac = (a, b) => h("span", { dir: "ltr" }, fmtNum(a), h("small", {}, ` / ${fmtNum(b)}`));
  const fmtTime = (u) => new Intl.DateTimeFormat(fa ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const topMax = Math.max(1, ...ins.top.map((u) => u.bytes));
  const strip = si ? hubStrip(si) : null;
  const map = (nodes || []).length ? netMap(nodes, si) : null;
  const rowsBox = h("div", {}, (nodes || []).map(nodeRow));
  const onlineK = h("span", {}, fmtNum(o.OnlineUsers));
  const nodesK = h("span", {}, frac(o.NodesOnline, o.Nodes));
  const view = h("div", {},
    h("h1", {}, t("dashboard")),
    si ? systemWarnings(si) : null,
    map ? map.el : null,
    strip ? strip.el : null,
    h("div", { class: "kpis" },
      kpi("nodes", t("nodesOnline"), nodesK),
      kpi("clients", t("activeClients"), frac(o.ClientsActive, o.Clients)),
      kpi("online", t("onlineUsers"), onlineK),
      kpi("traffic", t("trafficToday"), bytes(o.TrafficToday)),
      kpi("finance", t("trafficTotal"), bytes(o.TrafficTotal))),
    (nodes || []).length ? h("div", { class: "card node-rows" },
      h("div", { class: "nr-head" }, h("h2", { style: "margin:0" }, ux("nodesSection")), h("a", { class: "spacer small", href: "#/nodes" }, fa ? "مدیریت نودها ›" : "Manage nodes ›")),
      rowsBox) : null,
    h("div", { class: "dash2" },
      dashTrafficCard(ins),
      h("div", { class: "card form" }, h("h2", {}, t("topToday")),
        ins.top.length ? h("div", { class: "meters" }, ins.top.map((u) => meter(u.email, (100 * u.bytes) / topMax, bytes(u.bytes), true))) : h("p", { class: "muted small" }, t("noData"))),
      endingSoonCard(cls || []),
      h("div", { class: "card form span2" }, h("h2", {}, t("recentEvents")),
        ins.events.length ? h("div", { class: "form" }, ins.events.slice(0, 8).map((e) => h("div", { class: "row small" },
          eventBadge(e.kind), h("span", {}, e.email), h("span", { class: "muted spacer" }, `${e.node_id} · ${fmtTime(e.unix)}`))))
          : h("p", { class: "muted small" }, t("noEvents")))));
  // Live: the HUB strip and node rows refresh every 5 s while the page is open.
  const timer = setInterval(async () => {
    if (!view.isConnected) { clearInterval(timer); return; }
    if (document.hidden) return;
    try {
      const [x, ns, ov] = await Promise.all([api("GET", "/api/v2/admin/system"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/overview")]);
      if (strip) strip.update(x);
      if (map) map.update(ns, x);
      rowsBox.replaceChildren(...(ns || []).map(nodeRow));
      onlineK.textContent = fmtNum(ov.OnlineUsers);
      nodesK.replaceChildren(frac(ov.NodesOnline, ov.Nodes));
    } catch {}
  }, 5000);
  return view;
}

// repGrowthBox: the owner's "become a reseller" button and the free trial
// panel in the main bot (repgrow.go).
function repGrowthBox() {
  const fa = lang === "fa";
  const T = (a, b) => (fa ? a : b);
  const req = h("input", { type: "checkbox" }), trial = h("input", { type: "checkbox" }), gb = h("input", { type: "number", min: 1, max: 100, value: 5, dir: "ltr" });
  api("GET", "/api/v2/admin/rep-growth").then((r) => { req.checked = !!r.requests; trial.checked = !!(r.trial && r.trial.on); gb.value = (r.trial && r.trial.gb) || 5; }).catch(() => {});
  return h("form", { class: "form", onsubmit: async (e) => {
    e.preventDefault();
    await guarded(() => api("PUT", "/api/v2/admin/rep-growth", { requests: req.checked, trial: { on: trial.checked, gb: +gb.value || 5 } }));
    toast(t("saved"));
  } },
    h("label", { class: "check" }, req, h("span", {}, T("دکمهٔ «درخواست نمایندگی» در منوی ربات اصلی (درخواست با دکمهٔ قبول و رد برای شما می‌آید)", "\"Become a reseller\" button in the main bot (requests reach you with Accept / Refuse)"))),
    h("label", { class: "check" }, trial, h("span", {}, T("پنل نمایندگی تستی: هر حساب تلگرام یک بار، با اعتبار رایگان", "Trial reseller panel: once per Telegram account, with free credit"))),
    h("label", {}, h("span", {}, T("اعتبار پنل تستی (گیگ، ۱ تا ۱۰۰)", "Trial credit (GB, 1–100)")), gb),
    h("p", { class: "small muted" }, T("نماینده‌ها هم در ربات خودشان «🧩 جذب نماینده» دارند: لینک دعوت زیرمجموعه و همین دکمهٔ درخواست.", "Resellers have \"Grow\" in their own bot too: a sub-seller invite link and the same request button.")),
    h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, t("save"))));
}

// reportXlsxLinks: the Excel report (day by day, resellers, ledger,
// clients) for 7, 30 or 90 days — the owner's whole panel, a reseller its own.
function reportXlsxLinks(rep) {
  const fa = lang === "fa";
  const q = rep ? `&rep=${encodeURIComponent(rep)}` : "";
  return h("span", { class: "row" }, h("span", { class: "small muted" }, fa ? "گزارش اکسل:" : "Excel report:"),
    ...[7, 30, 90].map((d) => h("a", { class: "btn ghost", href: `/api/v2/shop/report.xlsx?days=${d}${q}`, download: "" }, fa ? `${fmtNum(d)} روز` : `${d} days`)));
}

// autoCleanBox switches the automatic cleanup of dead configs on or off
// (the owner for its own clients, a reseller for its own).
function autoCleanBox() {
  const fa = lang === "fa";
  const T = (a, b) => (fa ? a : b);
  const on = h("input", { type: "checkbox" }), days = h("input", { type: "number", min: 7, max: 365, value: 30, dir: "ltr" });
  const info = h("p", { class: "small muted" });
  const load = async () => {
    const r = await api("GET", "/api/v2/shop/autoclean").catch(() => null);
    if (!r) return;
    on.checked = !!r.settings.on; days.value = r.settings.days || 30;
    info.textContent = T(`الان ${fmtNum(r.due || 0)} کانفیگ مرده با این شرط هست.`, `${fmtNum(r.due || 0)} dead configs match now.`) +
      (r.settings.total ? T(` تا حالا ${fmtNum(r.settings.total)} کانفیگ خودکار پاک شده.`, ` ${fmtNum(r.settings.total)} deleted so far.`) : "");
  };
  load();
  return h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault();
    await guarded(() => api("PUT", "/api/v2/shop/autoclean", { on: on.checked, days: +days.value || 30 }));
    toast(t("saved")); load();
  } },
    h("h3", {}, "🧹 ", T("پاک‌سازی خودکار کانفیگ‌های مرده", "Automatic cleanup of dead configs")),
    h("p", { class: "small muted" }, T("کانفیگی که زمانش تمام شده یا حجمش تمام شده و از آن به بعد مصرفی نداشته، بعد از این مدت خودکار پاک می‌شود. کانفیگی که تمام نشده (حتی اگر هنوز وصل نشده باشد) هیچ‌وقت پاک نمی‌شود.",
      "A config whose term or volume ended and that has been quiet since is deleted after this many days. A config that has not ended (even one never connected) is never deleted.")),
    h("label", { class: "check" }, on, h("span", {}, T("روشن", "On"))),
    h("label", {}, h("span", {}, T("روز بی‌مصرفی بعد از تمام شدن (۷ تا ۳۶۵)", "Quiet days after the end (7–365)")), days),
    info,
    h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, t("save"))));
}

// cleanupForm previews and deletes expired, used-up or disabled clients.
function cleanupForm() {
  const fa = lang === "fa";
  const exp = h("input", { type: "checkbox", checked: true }), vol = h("input", { type: "checkbox" }), dis = h("input", { type: "checkbox" });
  const days = h("input", { type: "number", min: 0, max: 3650, value: 30 });
  const out = h("div", { class: "form" });
  const body = () => ({ expired: exp.checked, used_up: vol.checked, disabled: dis.checked, min_days: +days.value || 0 });
  let found = [];
  const del = h("button", { class: "danger", type: "button", disabled: true, onclick: async () => {
    if (!(await confirmAction(fa ? `${fmtNum(found.length)} کاربر برای همیشه حذف شوند؟ تاریخچهٔ مصرف روزانه برای گزارش‌ها می‌ماند.` : `Delete ${fmtNum(found.length)} clients for good? Daily history stays for reports.`))) return;
    const r = await guarded(() => api("POST", "/api/v2/admin/clients/cleanup", body()));
    toast(fa ? `${fmtNum(r.count)} کاربر حذف شد` : `${fmtNum(r.count)} clients deleted`); document.getElementById("modal").close(); route();
  } }, fa ? "حذف" : "Delete");
  const preview = async () => {
    const r = await guarded(() => api("POST", "/api/v2/admin/clients/cleanup", { ...body(), dry_run: true }));
    found = r.clients || [];
    del.disabled = !found.length;
    del.textContent = fa ? `حذف ${fmtNum(found.length)} کاربر` : `Delete ${fmtNum(found.length)}`;
    out.replaceChildren(found.length ? h("div", { class: "row" }, found.slice(0, 60).map((e) => h("span", { class: "badge" }, e)), found.length > 60 ? h("span", { class: "muted small" }, `+${fmtNum(found.length - 60)}`) : null)
      : h("p", { class: "muted small" }, fa ? "کاربری با این شرط‌ها نیست ✨" : "No client matches ✨"));
  };
  [exp, vol, dis, days].forEach((x) => x.addEventListener("change", preview));
  openModal(fa ? "پاک‌سازی کاربرها" : "Clean up clients", h("div", { class: "form" },
    autoCleanBox(),
    h("p", { class: "small muted" }, fa ? "کاربرهایی که دیگر استفاده نمی‌شوند را یک‌جا حذف کنید. اول فهرست را ببینید؛ تا «حذف» را نزنید چیزی پاک نمی‌شود." : "Delete clients nobody uses any more. You see the list first; nothing is deleted until you press Delete."),
    h("label", { class: "check" }, exp, fa ? "منقضی‌شده‌ها" : "Expired"), h("label", { class: "check" }, vol, fa ? "حجم‌تمام‌ها" : "Out of volume"), h("label", { class: "check" }, dis, fa ? "غیرفعال‌ها" : "Disabled"),
    h("label", {}, h("span", {}, fa ? "حداقل چند روز از انقضا یا آخرین اتصال گذشته باشد" : "At least this many days since expiry or last use"), days), out), [del]);
  preview();
}

// cloneClient copies a client under a new name.
function cloneClient(c) {
  const fa = lang === "fa";
  const name = h("input", { required: true, value: `${c.Email}-2`, dir: "ltr", autofocus: true });
  const go = h("button", { class: "primary", type: "button", onclick: async () => {
    await guarded(() => api("POST", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/clone`, { email: name.value.trim() }));
    document.getElementById("modal").close(); toast(fa ? "کپی ساخته شد" : "Copy created");
    location.hash = `#/client?e=${encodeURIComponent(name.value.trim())}`;
  } }, fa ? "ساخت کپی" : "Create copy");
  openModal(fa ? `کپی از ${c.Email}` : `Copy ${c.Email}`, h("div", { class: "form" },
    h("p", { class: "small muted" }, fa ? "همان پلن، حجم، مدت، اینباندها، گروه و تنظیمات؛ با لینک و کلید جدا و مصرف صفر." : "Same plan, volume, term, inbounds, group and settings; its own link and keys, usage at zero."),
    h("label", {}, h("span", {}, t("username")), name)), [go]);
}

// ring draws a small donut with a value in the middle.
function ring(share, value, label, sub, time = false, plain = false) {
  const r = 30, c = 2 * Math.PI * r, p = Math.max(0, Math.min(1, share || 0));
  const svg = svgEl("svg", { viewBox: "0 0 76 76", class: "ring-svg", "aria-hidden": "true" });
  svg.append(svgEl("circle", { cx: 38, cy: 38, r, class: "ring-bg" }));
  if (!plain) svg.append(svgEl("circle", { cx: 38, cy: 38, r, class: `ring-fg ${time ? "time" : ""} ${time ? (p < .1 ? "bad" : p < .25 ? "warn" : "") : (p > .9 ? "bad" : p > .75 ? "warn" : "")}`, "stroke-dasharray": `${c * p} ${c}` }));
  return h("div", { class: "ring" }, h("div", { class: "ring-box" }, svg, h("b", {}, value)), h("div", {}, h("div", { class: "ring-label" }, label), h("div", { class: "muted small" }, sub)));
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

// A node's last hours on the chart kit: CPU and memory together, network in
// and out together, and online users; each with now / average / peak.
async function nodeHealth(n) {
  const fa = lang === "fa";
  const box = h("div", { class: "form" });
  let hours = 24;
  const fmtT = (u) => new Intl.DateTimeFormat(fa ? "fa-IR" : "en-GB", hours > 24 ? { weekday: "short", hour: "2-digit", minute: "2-digit" } : { hour: "2-digit", minute: "2-digit" }).format(new Date(u * 1000));
  const load = async () => {
    fill(box, h("p", { class: "muted small" }, "…"));
    const pts = await api("GET", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/history?hours=${hours}`).catch(() => []);
    if (!pts.length) { fill(box, h("p", { class: "muted" }, t("noData"))); return; }
    const xs = pts.map((p) => p.t);
    const stats = (vals, f) => { const avg = vals.reduce((a, b) => a + b, 0) / vals.length;
      return h("span", { class: "muted" }, `${fa ? "الان" : "now"} ${f(vals.at(-1))} · ${fa ? "میانگین" : "avg"} ${f(avg)} · ${fa ? "بیشترین" : "peak"} ${f(Math.max(...vals))}`); };
    const pct = (v) => `${fmtNum(Math.round(v))}%`;
    const block = (title, series, extra, f) => h("div", { class: "form" },
      h("div", { class: "row small" }, h("strong", {}, title), ...series.map((s) => h("span", { class: "legend-item" }, (() => { const i = h("i", { class: "sw" }); i.style.background = s.color; return i; })(), s.name, " ", stats(s.values, f)))),
      chartKit({ xs, series, mode: "lines", height: 150, xLabel: (i) => fmtT(xs[i]), tipTitle: (i) => fmtT(xs[i]), fmt: f, ...extra }));
    fill(box,
      block(`${t("cpu")} / ${t("ram")}`, [{ id: "c", name: t("cpu"), color: "var(--ion)", area: true, values: pts.map((p) => p.cpu) }, { id: "r", name: t("ram"), color: "var(--ember)", dash: true, values: pts.map((p) => p.ram) }], { bin: false, max: 100, suffix: "%" }, pct),
      block(t("net"), [{ id: "i", name: `↓ ${t("inRate")}`, color: "var(--ion)", area: true, values: pts.map((p) => p.rx) }, { id: "o", name: `↑ ${t("outRate")}`, color: "var(--ember)", dash: true, values: pts.map((p) => p.tx) }], { suffix: "/s" }, rate),
      block(t("onlineUsers"), [{ id: "u", name: t("onlineUsers"), color: "var(--good)", area: true, values: pts.map((p) => p.online) }], { bin: false, int: true }, (v) => fmtNum(Math.round(v))));
  };
  const chips = h("div", { class: "row cseg" }, [[6, fa ? "۶ ساعت" : "6 h"], [24, fa ? "۲۴ ساعت" : "24 h"], [168, fa ? "۷ روز" : "7 days"]].map(([v, l]) =>
    h("button", { type: "button", class: v === hours ? "chip on" : "chip", onclick: (e) => { hours = v; e.currentTarget.parentElement.querySelectorAll("button").forEach((b) => b.classList.toggle("on", b === e.currentTarget)); load(); } }, l)));
  openModal(`${t("health")} · ${n.Name || n.ID}`, h("div", { class: "form health-modal" }, chips, box));
  load();
}

function nodeCard(n, actions = false) {
  const host = n.Host;
  return h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: `dot ${n.online ? "on" : ""}` }), h("span", { class: "name" }, `${n.Name || n.ID}`),
      h("span", { class: "badge" }, n.ID), n.update_available ? h("span", { class: "badge warn" }, t("updateAvail")) : null,
      n.traffic_percent && n.traffic_percent !== 100 ? h("span", { class: "badge", dir: "ltr", title: lang === "fa" ? "ضریب ترافیک" : "Traffic rate" }, `×${fmtNum(n.traffic_percent / 100)}`) : null, n.xray_version ? h("span", { class: "badge", dir: "ltr" }, `Xray ${n.xray_version}${n.arch ? " · " + n.arch : ""}`) : null, h("span", { class: `badge ${n.online ? "good" : "bad"} spacer` }, t(n.online ? "online" : "offline"))),
    h("div", { class: "muted small" }, `${t("restarts")}: ${fmtNum(n.XrayRestarts)} · ${t("spooled")}: ${fmtNum(n.Spooled)} · ${n.AgentVersion || ""}`),
    host ? h("div", { class: "node-gauges" }, ...(() => {
      const c = gauge(t("cpu")), r = gauge(t("ram")), d = gauge(t("disk"));
      c.set(host.cpu_percent, host.cores ? `${fmtNum(host.cores)} ${lang === "fa" ? "هسته" : "cores"}` : "");
      r.set(host.ram_total ? (100 * host.ram_used) / host.ram_total : 0, `${bytes(host.ram_used)} / ${bytes(host.ram_total)}`);
      d.set(host.disk_total ? (100 * host.disk_used) / host.disk_total : 0, `${bytes(host.disk_used)} / ${bytes(host.disk_total)}`);
      return [c.el, r.el, host.disk_total ? d.el : null];
    })(),
      h("div", { class: "small muted full" }, `↓ ${rate(host.rx_rate)} · ↑ ${rate(host.tx_rate)} · ${t("onlineUsers")}: ${fmtNum(host.online_users)} · ${t("uptime")}: ${dur(host.uptime_sec)}`)) : null,
    n.LastError ? h("div", { class: "small", style: "color:var(--bad)" }, `${t("lastError")}: ${n.LastError}`) : null,
    actions ? h("div", { class: "row node-actions" },
      n.update_available ? h("button", { class: "primary", onclick: () => updateNode(n) }, t("updateNode")) : null,
      h("button", { onclick: () => nodeHealth(n) }, t("health")),
      h("button", { onclick: () => nodeLogs(n) }, t("xrayLogs")),
      iconBtn("more", ux("more"), (e) => popMenu(e.currentTarget, (m) => {
        const item = (ic, label, fn, cls = "") => h("button", { class: `pm-item ${cls}`, onclick: () => { m.remove(); fn(); } }, icon(ic), label);
        m.append(
          item("pulse", t("restartXray"), () => restartXray(n)),
          item("inbounds", t("xrayVer"), () => xrayPicker(n)),
          item("settings", t("edit"), () => renameNode(n)),
          item("search", t("findSni"), () => {
            const f = sniFinder({ nodes: [n], node: n.ID, onPick: (d) => copy(d) });
            const d = openModal(`${t("findSni")} · ${n.Name || n.ID}`, f);
            d.addEventListener("close", () => f.cancel(), { once: true });
          }),
          item("link", t("reinstall"), async () => {
            try { const r = await guarded(() => api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/install`));
              openModal(t("reinstall"), h("div", { class: "form" }, h("p", { class: "small" }, t("installCmd")), h("code", {}, r.install), h("button", { type: "button", onclick: () => copy(r.install) }, t("copy"))));
            } catch {}
          }),
          item("trash", t("delete"), async () => {
            if (await confirmAction(t("confirmNodeDelete", { x: n.ID }))) { await guarded(() => api("DELETE", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}`)); route(); }
          }, "bad"));
      }))) : null);
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
  const pub = h("input", { value: n.public_ip_fixed ? n.PublicIP || "" : "", placeholder: (fa ? "خودکار: " : "Automatic: ") + (n.PublicIP || "—"), dir: "ltr", inputmode: "decimal" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!pct.reportValidity()) return;
    try { await guarded(() => api("PUT", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}`, { Name: name.value, traffic_percent: +pct.value, public_ip: pub.value.trim() })); document.getElementById("modal").close("save"); route(); } catch {}
  } }, t("save"));
  openModal(`${t("edit")} · ${n.ID}`, h("div", { class: "form" }, h("label", {}, h("span", {}, t("name")), name),
    h("label", {}, h("span", {}, fa ? "ضریب ترافیک کاربرها (درصد)" : "Client traffic rate (percent)"), pct,
      h("small", { class: "muted" }, fa ? "مصرف کاربرها روی این نود با این درصد حساب می‌شود: ۱۵۰ یعنی هر ۱ گیگ ۱٫۵ گیگ حساب شود (نود ویژه) و ۵۰ یعنی نصف. آمار سرور واقعی می‌ماند." : "Client usage on this node is billed at this rate: 150 counts 1 GB as 1.5 GB (a premium node), 50 as half. Server totals stay real.")),
    h("label", {}, h("span", {}, fa ? "آی‌پی عمومی نود" : "Node public IP"), pub,
      h("small", { class: "muted" }, fa ? "خالی = خودکار (آدرسی که نود از آن وصل می‌شود). برای نودی که پشت NAT است یا از تونل هاب وصل می‌شود، آی‌پی واقعی‌اش را بنویس؛ Swap و لینک‌ها از آن استفاده می‌کنند." : "Empty = automatic (the address the node connects from). For a node behind NAT or connected through the HUB's tunnel, enter its real IP; Swap and links use it."))), [save]);
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
  fa: { legacy: "کاربران قدیمی", trial: "تست", all: "همه", online: "آنلاین", active: "فعال", expiring: "رو به انقضا", bad: "تمام‌شده", disabled: "غیرفعال", user: "کاربر", usage: "مصرف", remain: "باقی‌مانده",
    expiry: "انقضا", seen: "آخرین اتصال", actions: "", perPage: "در هر صفحه", page: "صفحهٔ {p} از {n}", prev: "قبلی", next: "بعدی", none: "کلاینتی با این فیلتر نیست.",
    reset: "صفر کردن مصرف", resetAsk: "مصرف {x} صفر شود؟ مقدار فعلی در سابقه می‌ماند.", resetDone: "مصرف {n} کلاینت صفر شد", enable: "فعال کردن", disable: "غیرفعال کردن",
    del: "حذف", delAsk: "{n} کلاینت حذف شوند؟", done: "{n} مورد انجام شد", never: "هرگز", now: "الان", ago: "{t} پیش", sub: "اشتراک", edit: "ویرایش", chart: "نمودار",
    sel: "{n} انتخاب‌شده", clear: "لغو انتخاب", allFilters: "همهٔ گروه‌ها و اینباندها", sortBy: "مرتب‌سازی" },
  en: { legacy: "Old users", trial: "Trial", all: "All", online: "Online", active: "Active", expiring: "Expiring", bad: "Ended", disabled: "Disabled", user: "Client", usage: "Usage", remain: "Remaining",
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

let clientsMem = null;
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
  const repNames = Object.fromEntries((reps || []).map((r) => [r.ID, r.Name || r.ID]));
  const saved = (() => { try { return JSON.parse(localStorage.getItem("clients-view") || "{}"); } catch { return {}; } })();
  // Search, filter and page survive the 15-second refresh (they used to reset).
  const st = clientsMem || { q: "", state: saved.state || "", extra: "", sort: saved.sort || "created", dir: saved.dir || 1, page: 0, size: saved.size || 25 };
  clientsMem = st;
  const remember = () => { try { localStorage.setItem("clients-view", JSON.stringify({ state: st.state, sort: st.sort, dir: st.dir, size: st.size })); } catch {} };
  const selected = new Set();
  const used = (c) => c.Usage.Up + c.Usage.Down;
  const counts = { "": list.length, online: 0, active: 0, expiring: 0, bad: 0, disabled: 0, trial: 0, legacy: 0 };
  for (const c of list) { counts[clientState(c)]++; if (c.Online) counts.online++; if (c.trial) counts.trial++; if (c.legacy) counts.legacy++; }
  const keyOf = {
    created: (c, i) => i, email: (c) => c.Email.toLowerCase(), usage: (c) => used(c),
    remain: (c) => (c.Remaining < 0 ? Infinity : c.Remaining), expiry: (c) => c.EffectiveUntil || Infinity, seen: (c) => -(c.Usage.LastSeen || 0),
  };
  const match = (c) => {
    if (st.q && !(c.Email + " " + (c.Note || "")).toLowerCase().includes(st.q.toLowerCase())) return false;
    if (st.state === "online" && !c.Online) return false;
    if (st.state === "trial" && !c.trial) return false;
    if (st.state === "legacy" && !c.legacy) return false;
    if (st.state && st.state !== "online" && st.state !== "trial" && st.state !== "legacy" && clientState(c) !== st.state) return false;
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
        me.role === "rep" && me.policy && me.policy.DenyRenew ? null : h("button", { onclick: () => renewForm([c]) }, lang === "fa" ? "تمدید" : "Renew"),
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
        h("div", { class: "row tight" }, h("span", { class: "av-wrap" }, avatar(c.Email, "sm"), h("span", { class: `dot ${c.Online ? "on" : ""}`, title: c.Online ? t("online") : t("offline") })),
          h("a", { class: "name", href: `#/client?e=${encodeURIComponent(c.Email)}` }, c.Email),
          c.trial ? h("span", { class: "badge trial" }, L.trial) : null),
        me && me.role !== "rep" ? h("div", { class: "small muted" }, (lang === "fa" ? "سازنده: " : "by ") + (c.RepID ? (repNames[c.RepID] || c.RepID) : (lang === "fa" ? "مالک" : "owner"))) : null,
        c.Note ? h("div", { class: "small muted clip" }, c.Note) : null),
      h("td", { class: "c-usage", "data-label": L.usage },
        h("div", { class: "small mono" }, `${bytes(u)} ${lang === "fa" ? "از" : "/"} ${c.VolumeBytes < 0 ? "∞" : bytes(c.VolumeBytes)}`),
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
      me.policy && me.policy.DenyRenew ? null : h("button", { onclick: () => renewForm(list.filter((c) => emails.includes(c.Email))) }, t("bulkRenew")),
      owner ? h("button", { onclick: () => bulkForm(emails) }, lang === "fa" ? "افزودن روز / حجم" : "Add days / volume") : null,
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
    chips.replaceChildren(...["", "online", "active", "expiring", "bad", "disabled", ...(counts.trial ? ["trial"] : []), ...(counts.legacy ? ["legacy"] : [])].map((k) => {
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
  q.value = st.q; extra.value = st.extra;
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
    })),
    me.role === "rep" && me.policy && me.policy.DenyRenew ? null : h("div", { class: "row" }, h("button", { class: "primary", onclick: () => renewForm(attention) }, lang === "fa" ? `تمدید هر ${fmtNum(attention.length)} مورد` : `Renew all ${attention.length}`))) : null;
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("clients")), h("span", { class: "muted small" }, t("clientsCount", { n: fmtNum(list.length) })),
      owner ? h("button", { class: "ghost spacer", onclick: () => templatesModal(inbounds) }, lang === "fa" ? "پلن‌های آماده" : "Ready plans") : h("span", { class: "spacer" }),
      owner ? h("button", { class: "ghost", onclick: () => cleanupForm() }, lang === "fa" ? "پاک‌سازی" : "Clean up")
        : me.role === "rep" ? h("button", { class: "ghost", onclick: () => openModal(lang === "fa" ? "پاک‌سازی خودکار" : "Automatic cleanup", autoCleanBox()) }, lang === "fa" ? "پاک‌سازی خودکار" : "Auto cleanup") : null,
      me.role === "rep" ? h("button", { class: "ghost", onclick: () => openModal(lang === "fa" ? "گزارش اکسل" : "Excel report", h("div", { class: "form" },
        h("p", { class: "small muted" }, lang === "fa" ? "فروش و برگشتی روزانه، پرداخت‌های فروشگاه، دفتر اعتبار و فهرست کلاینت‌ها." : "Daily sales and refunds, shop payments, credit ledger and the client list."), reportXlsxLinks())) }, lang === "fa" ? "گزارش اکسل" : "Excel report") : null,
      owner ? h("a", { class: "btn ghost", href: "/api/v2/admin/clients/export.csv", download: "", title: lang === "fa" ? "فهرست کاربرها برای اکسل" : "Client list for Excel" }, lang === "fa" ? "اکسل" : "Excel") : null,
      owner ? h("button", { onclick: () => bulkCreateForm(inbounds) }, t("bulkCreate")) : null,
      me.legacy ? h("a", { class: "btn", href: "#/legacy" }, "📥 ", lang === "fa" ? "آوردن کاربران قدیمی" : "Bring old users") : null,
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
  const L = fa ? { title: "پلن‌های آماده", hint: "هر پلن آماده بالای فرم ساخت کلاینت یک دکمه می‌شود؛ با یک کلیک همهٔ فیلدها پر می‌شوند.", id: "شناسه", name: "نام", gb: "حجم (گیگ، ۰ = نامحدود)", days: "روز",
      first: "زمان از اولین اتصال", ips: "حداکثر IP", dev: "حداکثر دستگاه", reset: "ریست دوره‌ای", prefix: "پیشوند نام", add: "افزودن پلن", del: "حذف", off: "خاموش", day: "روزانه", week: "هفتگی", month: "ماهانه", year: "سالانه", none: "هنوز پلن آماده‌ای نیست." }
    : { title: "Ready plans", hint: "When adding a client, “From template” fills every field.", id: "ID", name: "Name", gb: "Volume (GB, 0 = unlimited)", days: "Days",
      first: "Time from first connection", ips: "Max IPs", dev: "Max devices", reset: "Periodic reset", prefix: "Name prefix", add: "Add template", del: "Delete", off: "Off", day: "Daily", week: "Weekly", month: "Monthly", year: "Yearly", none: "No templates yet." };
  const list = h("div", { class: "list" });
  const f = { id: h("input", { pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", placeholder: "plan-30", dir: "ltr" }), name: h("input", { placeholder: "30 GB · 30 days" }),
    gb: h("input", { type: "number", min: "0", step: "0.5", value: "30" }), days: h("input", { type: "number", min: "0", value: "30" }),
    first: h("input", { type: "checkbox", checked: true }), ips: h("input", { type: "number", min: "0", value: "2" }), dev: h("input", { type: "number", min: "0", value: "0" }),
    reset: h("select", {}, [["", L.off], ["day", L.day], ["week", L.week], ["month", L.month], ["year", L.year]].map(([v, l]) => h("option", { value: v }, l))), prefix: h("input", { dir: "ltr" }) };
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
  openModal(`${lang === "fa" ? "افزودن روز / حجم" : "Add days / volume"} · ${t("selected", { n: fmtNum(emails.length) })}`, h("div", { class: "form two" },
    h("label", {}, h("span", {}, t("addDays")), days), h("label", {}, h("span", {}, t("addGB")), gb)), [save]);
}

async function viewGroups() {
  const fa = lang === "fa";
  const [groups, clients] = await Promise.all([api("GET", "/api/v2/admin/groups"), api("GET", "/api/v2/admin/clients")]);
  const cards = (groups || []).map((g) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, g.Name || g.ID), h("span", { class: "badge" }, g.ID),
      h("span", { class: "badge spacer" }, t("clientsCount", { n: fmtNum(g.Members.length) }))),
    g.Members.length ? h("div", { class: "chips" }, g.Members.slice(0, 8).map((m) => h("a", { class: "chip", href: `#/client?e=${encodeURIComponent(m)}`, dir: "ltr" }, m)),
      g.Members.length > 8 ? h("span", { class: "chip" }, `+${fmtNum(g.Members.length - 8)}`) : null)
      : h("p", { class: "small muted", style: "margin:0" }, fa ? "هنوز عضوی ندارد" : "No members yet"),
    h("div", { class: "row" },
      h("button", { onclick: () => renewGroup(g, clients) }, t("bulkRenew")),
      h("button", { class: "ghost", onclick: () => bulkGroup(g) }, lang === "fa" ? "افزودن روز / حجم" : "Add days / volume"),
      h("button", { class: "ghost", onclick: () => groupForm(g, clients) }, t("edit")),
      h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: g.Name || g.ID }))) { await guarded(() => api("DELETE", `/api/v2/admin/groups/${encodeURIComponent(g.ID)}`)); route(); }
      } }, t("delete")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("groups")), h("button", { class: "primary spacer", onclick: () => groupForm(null, clients) }, t("add"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

function renewGroup(g, clients) {
  const list = (clients || []).filter((c) => g.Members.includes(c.Email));
  if (list.length) renewForm(list);
}

function bulkGroup(g) {
  const days = h("input", { type: "number", min: "0", value: "30" });
  const gb = h("input", { type: "number", min: "0", step: "0.5", value: "0" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try { const r = await api("POST", "/api/v2/admin/clients/bulk", { Group: g.ID, AddDays: +days.value || 0, AddGB: +gb.value || 0 });
      toast(t("changed", { n: fmtNum(r.changed) })); document.getElementById("modal").close("save"); route(); } catch (x) { toast(t("failed", { e: x.message })); }
  } }, t("save"));
  openModal(`${lang === "fa" ? "افزودن روز / حجم" : "Add days / volume"} · ${g.Name || g.ID}`, h("div", { class: "form two" },
    h("label", {}, h("span", {}, t("addDays")), days), h("label", {}, h("span", {}, t("addGB")), gb)), [save]);
}

// groupForm: members are picked with a search, not from a wall of every
// client; the chosen ones sit on top and are removed with a tap.
function groupForm(g, clients) {
  const fa = lang === "fa";
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", value: g ? g.ID : "", disabled: !!g, autofocus: !g, dir: "ltr" });
  const name = h("input", { value: g ? g.Name : "" });
  const picked = new Set(g ? g.Members : []);
  const all = (clients || []).map((c) => c.Email);
  const chosen = h("div", { class: "chips" });
  const q = h("input", { type: "search", dir: "ltr", placeholder: fa ? "جستجوی نام کلاینت…" : "Search clients…" });
  const results = h("div", { class: "pick-list" });
  const count = h("span", { class: "badge" });
  const draw = () => {
    count.textContent = fmtNum(picked.size);
    chosen.replaceChildren(...(picked.size ? [...picked].map((m) => h("button", { type: "button", class: "chip", "aria-pressed": "true", dir: "ltr", title: fa ? "حذف از گروه" : "Remove", onclick: () => { picked.delete(m); draw(); } }, m, " ✕"))
      : [h("span", { class: "muted small" }, fa ? "هنوز کسی انتخاب نشده" : "Nobody picked yet")]));
    const f = q.value.trim().toLowerCase();
    const hits = all.filter((e) => !picked.has(e) && (!f || e.toLowerCase().includes(f)));
    results.replaceChildren(
      ...hits.slice(0, 40).map((e) => h("button", { type: "button", class: "pick", dir: "ltr", onclick: () => { picked.add(e); draw(); } }, "+ ", e)),
      hits.length > 40 ? h("div", { class: "muted small" }, fa ? `${fmtNum(hits.length - 40)} مورد دیگر؛ دقیق‌تر جستجو کنید` : `${hits.length - 40} more; narrow the search`) : null,
      f && hits.length > 1 ? h("button", { type: "button", class: "ghost small", onclick: () => { hits.forEach((e) => picked.add(e)); draw(); } }, fa ? `افزودن همهٔ ${fmtNum(hits.length)} نتیجه` : `Add all ${hits.length} results`) : null,
      !hits.length ? h("div", { class: "muted small" }, fa ? "نتیجه‌ای نیست" : "No match") : null);
  };
  q.addEventListener("input", draw);
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    try { await guarded(() => api("PUT", `/api/v2/admin/groups/${encodeURIComponent(id.value)}`, { Name: name.value, Members: [...picked] }));
      document.getElementById("modal").close("save"); route(); } catch {}
  } }, t("save"));
  draw();
  openModal(g ? `${t("edit")} · ${g.Name || g.ID}` : `${t("add")} · ${t("groups")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("div", {}, h("div", { class: "row" }, h("span", { class: "lbl" }, t("members")), count), chosen),
    h("div", {}, q, results)), [save]);
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
      me.role === "rep" && me.policy && me.policy.DenyRenew ? null : h("button", { class: "ghost", onclick: () => renewForm([c]) }, lang === "fa" ? "تمدید" : "Renew"),
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
  const fa = lang === "fa";
  const canQuick = me.role !== "rep";
  const quick = async (body, msg) => { await guarded(() => api("POST", "/api/v2/admin/clients/bulk", { Emails: [c.Email], ...body })); toast(msg); route(); };
  const volShare = c.VolumeBytes > 0 ? Math.min(1, used / c.VolumeBytes) : 0;
  const totalDays = c.DurationSeconds ? c.DurationSeconds / 86400 : (left || 0);
  const timeShare = left == null || !totalDays ? 1 : Math.max(0, Math.min(1, left / Math.max(totalDays, left)));
  const hero = h("div", { class: "card hero" },
    h("div", { class: "hero-id" }, h("a", { class: "btn ghost back", href: "#/clients", "aria-label": t("clients") }, fa ? "›" : "‹"),
      h("span", { class: "av-wrap" }, avatar(c.Email, "lg"), h("span", { class: `dot ${c.Online ? "on" : ""}` })),
      h("div", {}, h("h1", {}, c.Email),
        h("div", { class: "row tight" }, h("span", { class: `badge ${cls}` }, status), c.Online ? h("span", { class: "badge good" }, t("online")) : null,
          c.trial ? h("span", { class: "badge trial" }, lang === "fa" ? "تست رایگان" : "Free trial") : null,
          c.RepID ? h("span", { class: "badge" }, c.RepID) : null, c.Note ? h("span", { class: "muted small" }, c.Note) : null))),
    h("div", { class: "hero-rings" },
      ring(volShare, c.VolumeBytes > 0 ? `${fmtNum(Math.round(volShare * 100))}%` : "∞", t("used"), c.VolumeBytes > 0 ? `${bytes(used)} ${fa ? "از" : "/"} ${bytes(c.VolumeBytes)}` : bytes(used)),
      ring(timeShare, left == null ? "∞" : fmtNum(left), t("timeLeft"), left == null ? (c.DurationSeconds ? t("notStarted") : t("noExpiry")) : t("daysLeft", { n: fmtNum(left) }), true),
      ring(0, c.MaxIPs ? fmtNum(c.MaxIPs) : "∞", t("maxIPs").split(" (")[0], `${t("last30Traffic")}: ${bytes(sum30)}`, false, true)),
    h("div", { class: "hero-actions" },
      url ? h("button", { class: "primary", onclick: () => copy(url) }, fa ? "کپی لینک اشتراک" : "Copy subscription link") : null,
      me.role === "rep" && me.policy && me.policy.DenyRenew ? null : h("button", { onclick: () => renewForm([c]) }, fa ? "تمدید" : "Renew"),
      canQuick ? h("button", { onclick: () => quick({ AddDays: 30, Enable: true }, fa ? "۳۰ روز اضافه شد" : "30 days added") }, fa ? "+۳۰ روز" : "+30 days") : null,
      canQuick ? h("button", { onclick: () => quick({ AddGB: 10, Enable: true }, fa ? "۱۰ گیگ اضافه شد" : "10 GB added") }, fa ? "+۱۰ گیگ" : "+10 GB") : null,
      canQuick ? h("button", { class: "ghost", onclick: () => quick({ Enable: !c.Enabled }, c.Enabled ? (fa ? "غیرفعال شد" : "Disabled") : (fa ? "فعال شد" : "Enabled")) }, c.Enabled ? (fa ? "غیرفعال کردن" : "Disable") : (fa ? "فعال کردن" : "Enable")) : null,
      h("button", { class: "ghost", onclick: () => clientForm(c, inbounds, ctx) }, t("edit")),
      canQuick ? h("button", { class: "ghost", onclick: () => cloneClient(c) }, fa ? "کپی" : "Copy") : null,
      me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "ghost danger", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: c.Email }))) { await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`)); location.hash = "#/clients"; }
      } }, t("delete"))));
  return h("div", {},
    hero,
    h("div", { class: "detail-wide" }, usageCard(c.Email, days)),
    h("div", { class: "detail-cols" },
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
      h("span", { class: `badge proto-${l.kind}` }, l.kind === "hysteria2" ? "hy2" : l.kind === "awg" ? "AmneziaWG" : l.kind),
      h("span", { class: "cfgname", dir: "auto" }, l.name || "—"),
      h("span", { class: "spacer" }),
      h("button", { type: "button", class: "ghost", "aria-label": "QR", onclick: () => showQR(l.name || c.Email, l.link) }, "QR"),
      l.kind === "awg" ? h("button", { type: "button", class: "ghost", onclick: () => {
        const a = h("a", { href: URL.createObjectURL(new Blob([l.link], { type: "text/plain" })), download: `${c.Email}-${(l.name || "awg").replace(/[^A-Za-z0-9._-]+/g, "-")}.conf` });
        document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 2000);
      } }, ".conf") : null,
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
    period: "ریست دوره‌ای", off: "خاموش", day: "روزانه", week: "هفتگی", month: "ماهانه", year: "سالانه", next: "ریست بعدی", history: "سابقهٔ ریست", resetDone: "مصرف صفر شد", noHistory: "هنوز ریستی نشده.", saved: "ذخیره شد" },
  en: { ips: "Connected IPs", ipsHint: "Addresses nodes saw for this client in the last 30 days.", noIPs: "No addresses seen yet.", onlineNow: "now", reset: "Reset usage", resetAsk: "Reset {x}'s usage? The current total stays in the history.",
    period: "Periodic reset", off: "Off", day: "Daily", week: "Weekly", month: "Monthly", year: "Yearly", next: "Next reset", history: "Reset history", resetDone: "Usage reset", noHistory: "No resets yet.", saved: "Saved" },
};
const fmtDT = (u) => new Date(u * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" });

// A private address in an IP list is the far end of a tunnel (the Iran
// relay replaces the user's real address), not the user: label it.
function isTunnelIP(ip) {
  return /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.|fd|fc)/i.test(String(ip || ""));
}
function ipView(ip, node) {
  if (!isTunnelIP(ip)) return h("span", { class: "mono", dir: "ltr" }, ip);
  return h("span", { class: "badge tun", title: ip }, `${lang === "fa" ? "تانل" : "tunnel"}${node ? ` · ${node}` : ""}`);
}

function clientIPsCard(c, ips) {
  const L = CD[lang] || CD.en;
  const online = ips.filter((x) => x.online && !isTunnelIP(x.ip)).length;
  const tun = ips.some((x) => isTunnelIP(x.ip));
  return h("div", { class: "card form" },
    h("div", { class: "row" }, h("h2", {}, L.ips), online ? h("span", { class: "badge good" }, `${fmtNum(online)} ${L.onlineNow}`) : null,
      c.MaxIPs ? h("span", { class: "badge spacer" }, `≤ ${fmtNum(c.MaxIPs)}`) : null),
    h("p", { class: "small muted" }, L.ipsHint),
    tun ? h("p", { class: "small muted" }, lang === "fa" ? "«تانل» یعنی کاربر از راه تانل ایران وصل شده و IP واقعی‌اش به نود نمی‌رسد؛ این اتصال‌ها در محدودیت IP شمرده نمی‌شوند. برای محدود کردن آن‌ها از «حداکثر دستگاه» استفاده کنید." : "\"Tunnel\" means the user came through an Iran relay and their real IP never reaches the node; these connections do not count toward the IP limit. Use \"Max devices\" to limit them.") : null,
    ips.length ? h("table", { class: "dtable compact" }, h("tbody", {}, ips.slice(0, 30).map((x) => h("tr", {},
      h("td", {}, h("span", { class: `dot ${x.online ? "on" : ""}` }), " ", ipView(x.ip, x.node_id)),
      h("td", { class: "small muted" }, x.node_id),
      h("td", { class: "small muted" }, x.online ? L.onlineNow : fmtDT(x.last)))))) : h("p", { class: "muted small" }, L.noIPs));
}

function clientResetCard(c, resets, owner) {
  const L = CD[lang] || CD.en;
  const period = h("select", { disabled: !owner }, [["", L.off], ["day", L.day], ["week", L.week], ["month", L.month], ["year", L.year]].map(([v, l]) => h("option", { value: v, selected: (c.reset_period || "") === v }, l)));
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
  const list = (inbounds || []).filter((ib) => !["socks", "http", "dokodemo-door", "tunnel", "mtproto"].includes(ib.protocol));
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

// clientForm creates or edits a client. Everything PasarGuard/3x-ui offer is
// here, but the common path stays short: name, volume, term, inbounds, save.
// The rest (periodic reset, devices, auto-renew, groups, Telegram, keys) is
// under "Advanced". A live summary shows the result, and resellers see what
// it costs before saving. A new client ends on its link and QR.
function clientForm(c, inbounds, ctx = { reps: [] }) {
  inbounds = (inbounds || []).filter((ib) => !["socks", "http", "dokodemo-door", "tunnel", "mtproto"].includes(ib.protocol));
  const fa = lang === "fa";
  const isNew = !c, owner = me.role === "owner", rep = me.role === "rep";
  const v = c || { Email: "", Enabled: true, VolumeBytes: -1, ExpiresUnix: 0, DurationSeconds: 30 * 86400, MaxIPs: 2, Inbounds: [], ...(ctx.seed || {}) };
  const now = () => Math.floor(Date.now() / 1000);
  const T = (f, e) => (fa ? f : e);
  const seg = (opts, val, onch) => {
    const box = h("div", { class: "seg", role: "radiogroup" });
    const set = (k) => { box.dataset.v = k; for (const b of box.children) b.setAttribute("aria-checked", String(b.dataset.k === k)); onch && onch(k); };
    for (const [k, l] of opts) box.append(h("button", { type: "button", role: "radio", "data-k": k, onclick: () => { set(k); calc(); } }, l));
    set(val); return box;
  };
  const chipRow = (vals, fn, fmt) => h("div", { class: "chips qp" }, vals.map((x) => h("button", { type: "button", class: "chip", onclick: () => { fn(x); calc(); } }, fmt(x))));
  const rnd = () => { const a = "abcdefghjkmnpqrstuvwxyz23456789"; let r = ""; const b = crypto.getRandomValues(new Uint8Array(8)); for (const x of b) r += a[x % a.length]; return r; };

  // identity
  const name = h("input", { required: true, pattern: "[A-Za-z0-9][A-Za-z0-9._@+\\-]{0,127}", value: v.Email, dir: "ltr", autofocus: isNew, placeholder: "ali-30gb" });
  const dice = isNew ? h("button", { type: "button", class: "ghost icon-only", title: T("نام تصادفی", "Random name"), onclick: () => { name.value = (tplPrefix || "") + rnd(); calc(); } }, "🎲") : null;
  let tplPrefix = "";
  const status = seg([["on", T("فعال", "Active")], owner ? ["hold", T("در انتظار", "On hold")] : null, ["off", T("غیرفعال", "Disabled")]].filter(Boolean),
    !v.Enabled ? "off" : v.StartBy ? "hold" : "on");

  // volume
  const unl0 = v.VolumeBytes < 0 || v.Plan === "unlimited";
  const vol = h("input", { type: "number", min: "0", step: "0.5", dir: "ltr", value: unl0 ? 0 : +(v.VolumeBytes / 2 ** 30).toFixed(2) });
  const canUnl = !(rep && me.policy && !me.policy.AllowUnlimited);
  const volChips = chipRow([10, 20, 30, 50, 100, 200].concat(canUnl ? [0] : []), (x) => { vol.value = x; }, (x) => (x ? `${fmtNum(x)} GB` : "∞"));

  // term
  const daysLeft = v.ExpiresUnix ? Math.max(0, Math.round((v.ExpiresUnix - now()) / 86400)) : Math.round((v.DurationSeconds || 0) / 86400);
  const days = h("input", { type: "number", min: "0", max: "3650", dir: "ltr", value: daysLeft });
  const date = h("input", { type: "date", dir: "ltr" });
  const fromDays = () => { const d = +days.value || 0; date.value = d ? new Date((now() + d * 86400) * 1000).toISOString().slice(0, 10) : ""; };
  date.addEventListener("change", () => { days.value = date.value ? Math.max(1, Math.ceil((new Date(date.value + "T23:59:59").getTime() / 1000 - now()) / 86400)) : 0; calc(); });
  days.addEventListener("input", fromDays);
  const dateFa = h("span", { class: "small muted datefa" });
  const dayChips = chipRow([7, 30, 60, 90, 180, 365, 0], (x) => { days.value = x; fromDays(); }, (x) => (x ? (x % 30 === 0 ? T(`${fmtNum(x / 30)} ماه`, `${x / 30} mo`) : x === 365 ? T("۱ سال", "1 yr") : T(`${fmtNum(x)} روز`, `${x} d`)) : "∞"));
  const firstUse0 = isNew ? true : !v.ExpiresUnix && v.DurationSeconds > 0;
  const start = seg([["now", T("از همین الان", "From now")], ["first", T("از اولین اتصال", "First connection")]], firstUse0 ? "first" : "now");
  const holdDays = h("input", { type: "number", min: "1", max: "365", dir: "ltr", value: v.StartBy ? Math.max(1, Math.round((v.StartBy - now()) / 86400)) : 7 });

  // inbounds
  const ibChips = inbounds.map((ib) => {
    const on = isNew && !ctx.seed ? inbounds.length <= 3 : (v.Inbounds || []).includes(ib.tag);
    const b = h("button", { type: "button", class: "chip ib", "aria-pressed": String(on), "data-tag": ib.tag, title: `${ib.protocol} · ${ib.network}/${ib.security}` },
      h("span", { dir: "ltr" }, ib.tag), h("small", { class: "muted", dir: "ltr" }, ` ${ib.protocol}`));
    b.addEventListener("click", () => { b.setAttribute("aria-pressed", String(b.getAttribute("aria-pressed") !== "true")); calc(); });
    return b;
  });
  const allIb = h("button", { type: "button", class: "ghost small", onclick: () => { const all = ibChips.every((b) => b.getAttribute("aria-pressed") === "true"); ibChips.forEach((b) => b.setAttribute("aria-pressed", String(!all))); calc(); } }, T("همه", "All"));
  const shownIb = new Set(inbounds.map((ib) => ib.tag));
  const hiddenIb = (v.Inbounds || []).filter((t0) => !shownIb.has(t0)); // e.g. outside a reseller's list: kept as they are
  const pickedIb = () => [...hiddenIb, ...ibChips.filter((b) => b.getAttribute("aria-pressed") === "true").map((b) => b.dataset.tag)];

  // advanced
  const ips = h("input", { type: "number", min: "0", max: "1000", dir: "ltr", value: v.MaxIPs });
  const hwid = h("input", { type: "number", min: "0", max: "100", dir: "ltr", value: v.hwid_limit || 0 });
  // Daily cap (GB a day, 0 = none): the rest of the day the config stops once it is used.
  const dcap0 = v.daily_cap_bytes ? +(v.daily_cap_bytes / 2 ** 30).toFixed(2) : 0;
  const dcap = h("input", { type: "number", min: "0", step: "0.5", dir: "ltr", value: dcap0 || 0 });
  const reset = h("select", {}, [["", T("بدون ریست", "Never")], ["day", T("روزانه", "Daily")], ["week", T("هفتگی", "Weekly")], ["month", T("ماهانه", "Monthly")]]
    .map(([k, l]) => h("option", { value: k, selected: (v.reset_period || "") === k }, l)));
  const np0 = v.next_plan || null;
  const npOn = h("input", { type: "checkbox", checked: !!np0 });
  const npGB = h("input", { type: "number", min: "0", step: "0.5", dir: "ltr", value: np0 ? np0.gb : 30 });
  const npDays = h("input", { type: "number", min: "0", dir: "ltr", value: np0 ? np0.days : 30 });
  const npFirst = h("input", { type: "checkbox", checked: np0 ? !!np0.first_use : false });
  const tg = h("input", { type: "number", dir: "ltr", placeholder: "123456789" });
  const note = h("input", { value: v.Note || "", maxlength: "2000", placeholder: T("مثلاً @username یا شماره تماس", "e.g. @username or phone") });
  const uuid = h("input", { dir: "ltr", value: isNew ? "" : v.UUID || "", placeholder: T("خودکار", "auto"), pattern: "[0-9a-fA-F\\-]{36}" });
  const subTok = h("input", { dir: "ltr", value: isNew ? "" : v.SubToken || "", placeholder: T("خودکار", "auto"), pattern: "[A-Za-z0-9_\\-]{8,64}" });
  const rotate = !isNew && owner ? h("div", { class: "row" },
    h("button", { type: "button", class: "ghost small", onclick: async () => { if (await confirmAction(T("کلید اتصال عوض شود؟ کانفیگ‌های فعلی کاربر از کار می‌افتند.", "Replace the connection keys? The user's current configs stop working."))) { await guarded(() => api("POST", `/api/v2/admin/clients/${encodeURIComponent(v.Email)}/rotate`, { credentials: true })); toast(T("کلید عوض شد", "Keys replaced")); } } }, T("کلید جدید", "New keys")),
    h("button", { type: "button", class: "ghost small", onclick: async () => { if (await confirmAction(T("لینک ساب عوض شود؟ لینک قبلی از کار می‌افتد.", "Replace the subscription link? The old link stops working."))) { await guarded(() => api("POST", `/api/v2/admin/clients/${encodeURIComponent(v.Email)}/rotate`, { link: true })); toast(T("لینک عوض شد", "Link replaced")); } } }, T("لینک ساب جدید", "New sub link"))) : null;
  const repSel = owner ? h("select", {}, h("option", { value: "" }, t("ownerSelf"))) : null;
  const groupBox = owner ? h("div", { class: "chips" }) : null;
  let groups = [];
  if (owner) {
    Promise.all([ctx.reps && ctx.reps.length ? Promise.resolve(ctx.reps) : api("GET", "/api/v2/admin/reps").catch(() => []), api("GET", "/api/v2/admin/groups").catch(() => [])]).then(([reps, gs]) => {
      for (const r of reps || []) repSel.append(h("option", { value: r.ID, selected: v.RepID === r.ID }, r.Name || r.ID));
      groups = gs || [];
      for (const g of groups) {
        const on = !isNew && (g.Members || []).includes(v.Email);
        const b = h("button", { type: "button", class: "chip", "aria-pressed": String(on), "data-id": g.ID }, g.Name || g.ID);
        b.addEventListener("click", () => b.setAttribute("aria-pressed", String(b.getAttribute("aria-pressed") !== "true")));
        groupBox.append(b);
      }
      if (!groups.length) groupBox.append(h("span", { class: "muted small" }, T("گروهی ساخته نشده", "No groups yet")));
    });
    if (!isNew) api("GET", `/api/v2/admin/clients/${encodeURIComponent(v.Email)}/telegram`).then((r) => { if (r && r.chats && r.chats[0]) tg.value = r.chats[0]; }).catch(() => {});
  }

  // ready plans (saved templates) as one-click chips; hidden when there are none
  const tplBox = isNew ? h("div", { hidden: true }) : null;
  if (tplBox) {
    const sub = me && me.sub_seller;
    (sub ? api("GET", "/api/v2/shop/products?published").then((ps) => (ps || []).map((p) => ({ name: p.name, gb: p.plan === "unlimited" ? 0 : p.gb, days: p.days, first_use: p.first_use, max_ips: p.max_ips, inbounds: p.inbounds })))
      : api("GET", "/api/v2/admin/templates")).then((list) => {
      if (sub) {
        // A sub-seller sells only the plans its reseller defined.
        for (const x of [vol, days, ips]) x.readOnly = true;
        if (!(list || []).length) { tplBox.append(h("p", { class: "error" }, T("نماینده هنوز پلنی برای شما تعریف نکرده؛ از نماینده بخواهید در «فروشگاه» برایتان محصول بسازد.", "Your reseller has not defined any plans for you yet."))); tplBox.hidden = false; return; }
      }
      if (!(list || []).length) return;
      const chips = h("div", { class: "chips" });
      const apply = (tp, btn) => {
        for (const x of chips.children) x.setAttribute("aria-pressed", String(x === btn));
        vol.value = tp.gb || 0; days.value = tp.days || 0; fromDays(); start.querySelector(`[data-k="${tp.first_use ? "first" : "now"}"]`).click(); ips.value = tp.max_ips || 0;
        for (const b of ibChips) b.setAttribute("aria-pressed", String((tp.inbounds || []).includes(b.dataset.tag)));
        tplPrefix = tp.prefix || ""; if (tplPrefix && !name.value) name.value = tplPrefix;
        reset.value = tp.reset_period || ""; hwid.value = tp.hwid_limit || 0;
        calc();
      };
      for (const tp of list) {
        const btn = h("button", { type: "button", class: "chip", "aria-pressed": "false", title: [tp.gb ? `${tp.gb} GB` : "∞", tp.days ? `${tp.days} ${T("روز", "d")}` : ""].filter(Boolean).join(" · ") }, tp.name);
        btn.addEventListener("click", () => apply(tp, btn));
        chips.append(btn);
      }
      tplBox.append(h("span", { class: "lbl" }, sub ? T("پلن (تعریف‌شده توسط نماینده)", "Plan (set by your reseller)") : T("پلن آماده", "Ready plan")), chips);
      tplBox.hidden = false;
      if (sub && chips.firstChild) chips.firstChild.click();
    }).catch(() => {});
  }

  // live summary + reseller cost
  const sumBox = h("div", { class: "csum" });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save" }, isNew ? T("ساخت کلاینت", "Create client") : t("save"));
  const holdRow = h("div", { class: "two" }, h("label", {}, h("span", {}, T("مهلت شروع (روز)", "Must start within (days)")), holdDays),
    h("p", { class: "small muted", style: "align-self:end;margin:0 0 8px" }, T("اگر تا این مهلت وصل نشود، زمانش خودکار شروع می‌شود.", "If unused by then, the term starts anyway.")));
  const startRow = h("div", {}, h("span", { class: "lbl" }, T("شروع زمان", "Term starts")), start);
  const unitsFor = (d) => (d > 0 ? Math.ceil(d / 30) : 0);
  function calc() {
    const st = status.dataset.v, gb = +vol.value || 0, d = +days.value || 0;
    holdRow.hidden = st !== "hold"; startRow.hidden = st === "hold" || !d;
    const until = !d ? T("بدون انقضا", "no expiry") : st === "hold" || start.dataset.v === "first" ? T(`${fmtNum(d)} روز از اولین اتصال`, `${d} days from first use`)
      : new Intl.DateTimeFormat(fa ? "fa-IR-u-ca-persian" : "en-GB", { dateStyle: "medium" }).format(new Date((now() + d * 86400) * 1000));
    dateFa.textContent = d ? new Intl.DateTimeFormat(fa ? "fa-IR-u-ca-persian" : "en-GB", { dateStyle: "full" }).format(new Date((now() + d * 86400) * 1000)) : "";
    const n = pickedIb().length;
    const bits = [gb ? `${fmtNum(gb)} GB` : T("حجم نامحدود", "unlimited volume"), until, n ? T(`${fmtNum(n)} اینباند`, `${n} inbound${n > 1 ? "s" : ""}`) : null,
      +ips.value ? T(`${fmtNum(+ips.value)} IP`, `${ips.value} IP`) : null, +hwid.value ? T(`${fmtNum(+hwid.value)} دستگاه`, `${hwid.value} devices`) : null, reset.value ? T("ریست دوره‌ای", "periodic reset") : null].filter(Boolean);
    const kids = [h("div", { class: "csum-line" }, h("b", {}, T("نتیجه: ", "Result: ")), bits.join(" · "))];
    let short = false;
    if (rep) {
      const isUnl = gb === 0;
      const credit = me.volume_credit || 0, units = me.unlimited_units || 0;
      const prev = isNew ? 0 : (v.VolumeBytes > 0 ? v.VolumeBytes : 0);
      const costB = isUnl ? 0 : Math.max(0, Math.round(gb * 2 ** 30) - prev);
      const costU = isUnl ? Math.max(0, unitsFor(d) - (isNew ? 0 : unitsFor(daysLeft))) : 0;
      short = costB > credit || costU > units;
      kids.push(h("div", { class: `cost ${short ? "bad" : ""}` },
        h("div", {}, T("از اعتبار کم می‌شود: ", "Takes from credit: "), h("b", { dir: "ltr" }, [costB ? gbs(costB) : "", costU ? `${fmtNum(costU)} ${t("units")}` : ""].filter(Boolean).join(" + ") || "0")),
        h("div", {}, T("مانده بعد از ساخت: ", "Left after: "), h("b", { dir: "ltr" }, gbs(Math.max(0, credit - costB))), units || costU ? ` · ${fmtNum(Math.max(0, units - costU))} ${t("units")}` : ""),
        short ? h("div", {}, T("اعتبار کافی نیست.", "Not enough credit.")) : null));
    }
    if (!n) kids.push(h("div", { class: "small", style: "color:var(--warn)" }, T("هیچ اینباندی انتخاب نشده؛ کاربر کانفیگی نمی‌گیرد.", "No inbound selected; the user gets no configs.")));
    save.disabled = short;
    sumBox.replaceChildren(...kids);
  }
  [vol, ips, hwid, reset, days].forEach((x) => x.addEventListener("input", calc));
  reset.addEventListener("change", calc);

  save.addEventListener("click", async (e) => {
    e.preventDefault();
    err.textContent = "";
    if (!name.reportValidity() || (uuid.value && !uuid.reportValidity()) || (subTok.value && !subTok.reportValidity())) return;
    const st = status.dataset.v, d = +days.value || 0, gb = +vol.value || 0;
    const first = st === "hold" || start.dataset.v === "first";
    const body = {
      Enabled: st !== "off", MaxIPs: +ips.value || 0,
      VolumeBytes: gb > 0 ? Math.round(gb * 2 ** 30) : -1,
      ExpiresUnix: d && !first ? now() + d * 86400 : 0,
      DurationSeconds: d && first ? d * 86400 : 0,
      Inbounds: pickedIb(),
      Plan: gb > 0 ? "volume" : "unlimited",
    };
    if (!isNew && !first && v.ExpiresUnix && d === daysLeft) body.ExpiresUnix = v.ExpiresUnix;
    if (owner && uuid.value && uuid.value !== v.UUID) body.UUID = uuid.value;
    if (owner && subTok.value && subTok.value !== v.SubToken) body.SubToken = subTok.value;
    if (!isNew && owner) { body.UUID ||= v.UUID; body.SubToken ||= v.SubToken; }
    save.disabled = true;
    name.value = name.value.trim();
    const em = encodeURIComponent(name.value);
    try {
      // A changed name is a rename: history, usage and the sub link stay with the client.
      if (!isNew && name.value !== v.Email) {
        await api("POST", `/api/v2/admin/clients/${encodeURIComponent(v.Email)}/rename`, { Name: name.value });
        for (const g of groups || []) g.Members = (g.Members || []).map((m) => (m === v.Email ? name.value : m));
        const was = v.Email; v.Email = name.value;
        if (location.hash.includes(`e=${encodeURIComponent(was)}`)) history.replaceState(null, "", location.hash.replace(`e=${encodeURIComponent(was)}`, `e=${em}`));
      }
      await api("PUT", `/api/v2/admin/clients/${em}`, body);
      const extra = [];
      if ((note.value || "") !== (v.Note || "")) extra.push(api("PUT", `/api/v2/admin/clients/${em}/note`, { Note: note.value }));
      if (+hwid.value !== (v.hwid_limit || 0)) extra.push(api("PUT", `/api/v2/admin/clients/${em}/hwid-limit`, { limit: +hwid.value || 0 }));
      if ((+dcap.value || 0) !== dcap0) extra.push(api("PUT", `/api/v2/admin/clients/${em}/daily-cap`, { gb: Math.max(0, +dcap.value || 0) }));
      if (owner) {
        if (reset.value !== (v.reset_period || "")) extra.push(api("PUT", `/api/v2/admin/clients/${em}/reset-period`, { period: reset.value }));
        const np = npOn.checked ? { gb: +npGB.value || 0, days: +npDays.value || 0, first_use: npFirst.checked } : null;
        if (JSON.stringify(np) !== JSON.stringify(np0 ? { gb: np0.gb, days: np0.days, first_use: !!np0.first_use } : null)) extra.push(api("PUT", `/api/v2/admin/clients/${em}/next-plan`, { plan: np }));
        if (st === "hold") extra.push(api("PUT", `/api/v2/admin/clients/${em}/on-hold`, { Days: +holdDays.value || 7 }));
        else if (v.StartBy) extra.push(api("PUT", `/api/v2/admin/clients/${em}/on-hold`, { Days: 0 }));
        if (tg.value) extra.push(api("PUT", `/api/v2/admin/clients/${em}/telegram`, { chat: +tg.value }));
        if (repSel.value !== (v.RepID || "")) extra.push(api("POST", `/api/v2/admin/clients/${em}/transfer`, { RepID: repSel.value }));
        for (const b of groupBox.querySelectorAll("button[data-id]")) {
          const g = groups.find((x) => x.ID === b.dataset.id); if (!g) continue;
          const want = b.getAttribute("aria-pressed") === "true", has = (g.Members || []).includes(name.value);
          if (want !== has) extra.push(api("PUT", `/api/v2/admin/groups/${encodeURIComponent(g.ID)}`, { Name: g.Name, Members: want ? [...(g.Members || []), name.value] : (g.Members || []).filter((m) => m !== name.value) }));
        }
      }
      const res = await Promise.allSettled(extra);
      const bad = res.filter((r) => r.status === "rejected");
      if (bad.length) toast(T(`ذخیره شد، ولی ${fmtNum(bad.length)} تنظیم اعمال نشد: ${bad[0].reason.message}`, `Saved, but ${bad.length} setting(s) failed: ${bad[0].reason.message}`));
      if (!isNew) { document.getElementById("modal").close("save"); if (!bad.length) toast(t("saved")); route(); return; }
      created(name.value);
    } catch (x) { err.textContent = x.message; save.disabled = false; }
  });

  // after creating: the link, a QR and "another like this"
  async function created(email) {
    const list = await api("GET", "/api/v2/admin/clients").catch(() => []);
    const cl = (list || []).find((x) => x.Email === email) || { Email: email };
    const link = cl.SubURL || "";
    const copy = h("button", { class: "primary", type: "button", onclick: async () => { try { await navigator.clipboard.writeText(link); toast(T("لینک کپی شد", "Link copied")); } catch {} } }, T("کپی لینک ساب", "Copy sub link"));
    const again = h("button", { type: "button", onclick: () => { const st = status.dataset.v, gb = +vol.value || 0, dd = +days.value || 0, first = st === "hold" || start.dataset.v === "first";
      clientForm(null, inbounds, { ...ctx, seed: { VolumeBytes: gb ? Math.round(gb * 2 ** 30) : -1, DurationSeconds: dd && first ? dd * 86400 : 0, ExpiresUnix: dd && !first ? now() + dd * 86400 : 0, MaxIPs: +ips.value || 0, Inbounds: pickedIb(), hwid_limit: +hwid.value || 0, reset_period: reset.value } }); } }, T("یکی دیگر مثل همین", "Another like this"));
    const open = h("button", { type: "button", onclick: () => { document.getElementById("modal").close(); location.hash = `#/client?e=${encodeURIComponent(email)}`; } }, T("صفحهٔ کلاینت", "Open client"));
    openModal(T(`ساخته شد · ${email}`, `Created · ${email}`), h("div", { class: "form created" },
      h("img", { class: "qr", alt: "QR", src: `/api/v2/admin/clients/${encodeURIComponent(email)}/qr.png`, onerror: (e) => e.target.remove() }),
      link ? h("code", {}, link) : null,
      h("p", { class: "small muted center" }, T("این لینک را برای کاربر بفرستید؛ همهٔ کانفیگ‌ها و تغییرات بعدی از همین لینک به او می‌رسد.", "Send this link to the user; every config and later change reaches them through it.")),
      h("div", { class: "row" }, again, open)), [copy]);
    route();
  }

  const adv = h("details", { class: "adv" }, h("summary", {}, T("تنظیمات پیشرفته", "Advanced")),
    h("div", { class: "form" },
      h("div", { class: "three" },
        h("label", {}, h("span", {}, T("حداکثر IP هم‌زمان", "Max concurrent IPs")), ips),
        h("label", {}, h("span", {}, T("حداکثر دستگاه (HWID)", "Max devices (HWID)")), hwid),
        owner ? h("label", {}, h("span", {}, T("ریست دوره‌ای حجم", "Periodic volume reset")), reset) : null),
      h("label", {}, h("span", {}, T("سقف مصرف روزانه (گیگ، ۰ = بی‌سقف)", "Daily cap (GB, 0 = none)")), dcap,
        h("span", { class: "small muted" }, v.daily_cap_bytes ? T(`امروز: ${bytes(v.daily_used || 0)} از ${bytes(v.daily_cap_bytes)}`, `Today: ${bytes(v.daily_used || 0)} of ${bytes(v.daily_cap_bytes)}`)
          : T("با رسیدن به سقف، کانفیگ تا شروع روز بعد (۳:۳۰ بامداد) قطع می‌شود.", "When the cap is reached the config stops until the next day (03:30 Tehran)."))),
      owner ? h("fieldset", { class: "np" }, h("label", { class: "check" }, npOn, T("تمدید خودکار با پلن بعدی وقتی حجم یا زمان تمام شد", "Auto-renew with a next plan when volume or time runs out")),
        h("div", { class: "three" }, h("label", {}, h("span", {}, T("حجم بعدی (گیگ، ۰ = نامحدود)", "Next volume (GB, 0 = ∞)")), npGB), h("label", {}, h("span", {}, T("روز بعدی", "Next days")), npDays),
          h("label", { class: "check", style: "align-self:end" }, npFirst, T("از اولین اتصال", "From first use")))) : null,
      owner ? h("div", { class: "two" }, h("label", {}, h("span", {}, T("مالک (نماینده)", "Owner (reseller)")), repSel), h("label", {}, h("span", {}, T("آیدی عددی تلگرام کاربر", "User's Telegram ID")), tg)) : null,
      owner ? h("div", {}, h("span", { class: "lbl" }, T("گروه‌ها", "Groups")), groupBox) : null,
      h("label", {}, h("span", {}, t("contact")), note),
      owner ? h("div", { class: "two" }, h("label", {}, h("span", {}, "UUID"), uuid), h("label", {}, h("span", {}, T("توکن لینک ساب", "Sub link token")), subTok)) : null,
      rotate));

  const body = h("div", { class: "form cform" },
    tplBox,
    h("div", { class: "two" },
      h("label", {}, h("span", {}, t("name")), h("div", { class: "row nowrap" }, name, dice)),
      h("div", {}, h("span", { class: "lbl" }, T("وضعیت", "Status")), status)),
    rep ? h("p", { class: "small muted", style: "margin:0" }, t("unitsHint")) : null,
    h("div", { class: "two" },
      h("div", {}, h("label", {}, h("span", {}, T("حجم (گیگ) — ۰ یعنی نامحدود", "Volume (GB) — 0 = unlimited")), vol), volChips),
      h("div", {}, h("div", { class: "row nowrap tight2" }, h("label", { style: "flex:1" }, h("span", {}, T("مدت (روز)", "Days")), days), h("label", { style: "flex:1.3" }, h("span", {}, T("یا تاریخ انقضا", "or expiry date")), date)), dateFa, dayChips)),
    startRow, holdRow,
    h("div", {}, h("div", { class: "row" }, h("span", { class: "lbl" }, t("assignInbounds")), inbounds.length > 1 ? h("span", { class: "spacer" }, allIb) : null),
      h("div", { class: "chips" }, ibChips.length ? ibChips : h("span", { class: "muted small" }, T("اینباندی نیست", "No inbounds")))),
    adv, sumBox, err);
  fromDays(); calc();
  const d = openModal(isNew ? T("کلاینت جدید", "New client") : `${t("edit")} · ${v.Email}`, body, [save]);
  d.classList.add("wide");
}

async function viewInbounds() {
  const [list, nodes, presets, certs] = await Promise.all([api("GET", "/api/v2/admin/inbounds"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/presets"), api("GET", "/api/v2/admin/certs")]);
  const acme = await api("GET", "/api/v2/admin/acme").catch(() => ({}));
  acme.settings ||= {}; acme.certs ||= [];
  const fa = lang === "fa";
  const ctx = { nodes, presets, certs };
  const family = (p) => ({ vless: "v", vmess: "v", trojan: "t", shadowsocks: "s", hysteria: "q", hysteria2: "q", tuic: "q", amneziawg: "w", wireguard: "w", mtproto: "m" }[p] || "p");
  const resetUsage = async (ib) => {
    if (!(await confirmAction(fa ? `مصرف همهٔ کاربرهای ${ib.tag} صفر شود؟ حجم کاملشان دوباره در دسترس قرار می‌گیرد.` : `Zero the usage of every client on ${ib.tag}? Their full volume becomes available again.`))) return;
    const r = await guarded(() => api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}/reset-usage`));
    toast(fa ? `مصرف ${fmtNum(r.reset)} کاربر صفر شد` : `${fmtNum(r.reset)} clients reset`); route();
  };
  // After an outage: add days and/or volume to everyone on this inbound.
  const compensate = async (ib) => {
    const all = await api("GET", "/api/v2/admin/clients").catch(() => []);
    const emails = all.filter((c) => (c.Inbounds || []).includes(ib.tag)).map((c) => c.Email);
    const days = h("input", { type: "number", min: "0", value: "1", dir: "ltr" }), gb = h("input", { type: "number", min: "0", step: "0.5", value: "0", dir: "ltr" });
    const go = h("button", { class: "primary", onclick: async (e) => {
      e.preventDefault();
      const r = await guarded(() => api("POST", "/api/v2/admin/clients/bulk", { Emails: emails, AddDays: +days.value || 0, AddGB: +gb.value || 0 }));
      d.close(); toast(fa ? `${fmtNum(r.changed ?? emails.length)} کاربر جبران شد` : `${r.changed ?? emails.length} clients compensated`);
    } }, fa ? "اعمال" : "Apply");
    const d = openModal(fa ? `جبران برای کاربرهای ${ib.tag}` : `Compensate ${ib.tag} clients`, h("div", { class: "form" },
      h("p", { class: "small muted" }, fa ? `به ${fmtNum(emails.length)} کاربر این اینباند اضافه می‌شود. برای سرویس‌های بدون انقضا روز اثری ندارد و برای حجم نامحدود گیگ اثری ندارد.` : `Added to the ${emails.length} clients of this inbound. Days do nothing for services without expiry, GB nothing for unlimited volume.`),
      h("div", { class: "two" }, h("label", {}, h("span", {}, fa ? "روز اضافه" : "Extra days"), days), h("label", {}, h("span", {}, fa ? "گیگ اضافه" : "Extra GB"), gb))), emails.length ? [go] : []);
  };
  const toggle = async (ib) => { await guarded(() => api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}/enabled`, { Enabled: !ib.enabled })); route(); };
  const cardOf = (ib) => {
    let meta = {};
    try { meta = typeof ib.meta === "string" ? JSON.parse(ib.meta) : ib.meta || {}; } catch {}
    const plain = ["socks", "http", "dokodemo-door", "tunnel", "mtproto"].includes(ib.protocol);
    const stat = (v, l) => h("div", { class: "ib-stat" }, h("b", {}, v), h("span", {}, l));
    const sw = h("input", { type: "checkbox", role: "switch", class: "switch", checked: !!ib.enabled, "aria-label": t(ib.enabled ? "disable" : "enable") });
    sw.addEventListener("change", () => toggle(ib));
    return h("article", { class: `card ib-card fam-${family(ib.protocol)} ${ib.enabled ? "" : "off"}`, "data-q": `${ib.tag} ${ib.protocol} ${ib.network} ${ib.security} ${(ib.nodes || []).join(" ")}`.toLowerCase() },
      h("header", { class: "ib-head" },
        h("span", { class: "ib-proto", dir: "ltr" }, ib.protocol),
        h("div", { class: "ib-title" }, h("h3", { dir: "ltr" }, ib.tag), h("span", { class: "muted small", dir: "ltr" }, [ib.port ? `:${ib.port}` : "", ib.network, ib.security].filter((x) => x && x !== "none").join(" · "))),
        sw,
        iconBtn("more", ux("more"), (e) => popMenu(e.currentTarget, (m) => {
          const item = (ic, label, fn, cls = "") => h("button", { class: `pm-item ${cls}`, onclick: () => { m.remove(); fn(); } }, icon(ic), label);
          m.append(
            item("settings", ie("title"), () => inboundEditor(ib.tag, nodes, certs)),
            item("copy", ie("clone"), () => cloneInbound(ib)),
            plain ? "" : item("refresh", fa ? "صفر کردن مصرف کاربرها" : "Reset client usage", () => resetUsage(ib)),
            plain ? "" : item("clients", fa ? "جبران برای کاربرها (+روز / +گیگ)" : "Compensate clients (+days / +GB)", () => compensate(ib)),
            item("trash", t("delete"), async () => {
              if (await confirmAction(t("confirmDelete", { x: ib.tag }))) { await guarded(() => api("DELETE", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}`)); route(); }
            }, "bad"));
        }))),
      h("div", { class: "ib-stats" },
        plain ? null : stat(fmtNum(ib.clients || 0), t("clients")),
        stat(ltr(bytes(ib.traffic_today || 0)), t("trafficToday")),
        stat(ltr(bytes(ib.traffic_total || 0)), t("trafficTotal"))),
      plainLine(ib, meta, nodes),
      ib.speed_kbps ? h("div", { class: "row small" }, h("span", { class: "badge" }, `⚡ ${fa ? "سقف سرعت مشترک" : "Shared speed cap"}: ${fmtNum(+(ib.speed_kbps / 1000).toFixed(1))} Mbps`)) : null,
      ib.cap_bytes || ib.ends_unix ? h("div", { class: "row small" },
        ib.cap_bytes ? meter(fa ? "سقف ترافیک" : "Traffic cap", (100 * (ib.cap_used || 0)) / ib.cap_bytes, ltr(`${bytes(ib.cap_used || 0)} / ${bytes(ib.cap_bytes)}`)) : null,
        ib.ends_unix ? h("span", { class: "badge" }, `${fa ? "پایان" : "Ends"}: ${new Date(ib.ends_unix * 1000).toLocaleDateString(fa ? "fa-IR" : "en-GB")}`) : null) : null,
      h("footer", { class: "ib-foot" },
        h("div", { class: "ib-nodes" }, (ib.nodes || []).length ? ib.nodes.map((n) => h("span", { class: "chip" }, h("span", { class: `dot ${(nodes || []).find((x) => x.ID === n)?.online ? "on" : ""}` }), n)) : h("span", { class: "muted small" }, fa ? "روی هیچ نودی نیست" : "Not on any node")),
        h("button", { class: "ghost", onclick: () => inboundStudio(ib.tag, ctx) }, t("nodesRoutes"))));
  };
  const all = list || [];
  const grid = h("div", { class: "ib-grid" }, all.length ? all.map(cardOf) : emptyState(fa ? "هنوز اینباندی نساخته‌اید" : "No inbounds yet", fa ? "با یک الگوی آماده (Reality، Hysteria2، TUIC، AmneziaWG و…) در چند ثانیه شروع کنید." : "Start from a ready preset (Reality, Hysteria2, TUIC, AmneziaWG…) in seconds.", t("newInbound"), () => inboundStudio(null, ctx)));
  const q = h("input", { type: "search", placeholder: fa ? "جستجوی تگ، پروتکل یا نود…" : "Search tag, protocol or node…" });
  q.addEventListener("input", () => { const f = q.value.trim().toLowerCase(); grid.querySelectorAll(".ib-card").forEach((c) => { c.hidden = !!f && !c.dataset.q.includes(f); }); });
  const on = all.filter((x) => x.enabled).length;
  const certRow = (c, extra, actions) => h("div", { class: "cert-row" },
    h("div", { class: "cert-main" }, h("b", { dir: "ltr" }, c.id), h("span", { class: "muted small", dir: "ltr" }, c.domains.join(", ")), extra),
    (c.not_after === undefined || c.not_after) && c.days_left !== undefined ? h("span", { class: `badge ${c.days_left < 7 ? "bad" : c.days_left < 21 ? "warn" : "good"}` }, t("daysLeft", { n: fmtNum(c.days_left) })) : null,
    actions);
  if (acme.certs.some((c) => c.status === "issuing" || c.status === "pending")) {
    const seq = routeSeq;
    setTimeout(() => { if (seq === routeSeq) route(); }, 4000);
  }
  return h("div", {},
    h("div", { class: "row sticky-head" }, h("h1", {}, t("inbounds")), h("span", { class: "badge" }, fa ? `${fmtNum(on)} فعال از ${fmtNum(all.length)}` : `${fmtNum(on)} of ${fmtNum(all.length)} on`),
      h("button", { class: "primary spacer", onclick: () => inboundStudio(null, ctx) }, t("newInbound"))),
    all.length > 3 ? h("div", { class: "row filter-bar" }, q) : null,
    grid,
    h("section", { class: "card cert-box" },
      h("div", { class: "row" }, h("h2", {}, t("certs")), h("button", { class: "ghost spacer", onclick: () => certForm() }, t("addCert"))),
      certs.length ? certs.map((c) => certRow(c, c.used_by.length ? h("span", { class: "small muted" }, `${t("usedBy")}: ${c.used_by.join(", ")}`) : null,
        h("div", { class: "row" }, h("button", { class: "ghost", onclick: () => certForm(c.id) }, t("edit")),
          iconBtn("trash", t("delete"), async () => { if (await confirmAction(t("confirmDelete", { x: c.id }))) { await guarded(() => api("DELETE", `/api/v2/admin/certs/${encodeURIComponent(c.id)}`)); route(); } }))))
        : h("p", { class: "muted small" }, t("empty"))),
    h("section", { class: "card cert-box" },
      h("div", { class: "row" }, h("h2", {}, t("acmeTitle")),
        h("button", { class: "ghost spacer", onclick: () => acmeSettingsForm(acme.settings) }, t("acmeSettings")),
        h("button", { class: "primary", onclick: () => acmeForm(nodes, acme.settings) }, t("freeCert"))),
      acme.certs.length ? acme.certs.map((c) => certRow(c,
        h("span", { class: "small muted" }, h("span", { class: `badge ${c.status === "ok" ? "good" : c.status === "failed" ? "bad" : "warn"}` }, t(`st_${c.status}`)), " ", c.method === "dns_cf" ? t("acmeDNS") : `${t("acmeHTTP")} · ${c.node_id}`,
          c.last_error ? h("span", { style: "color:var(--bad);display:block" }, c.last_error) : null),
        h("div", { class: "row" },
          h("button", { class: "ghost", disabled: c.status === "issuing", onclick: async () => { await guarded(() => api("POST", `/api/v2/admin/acme/certs/${encodeURIComponent(c.id)}/renew`)); setTimeout(route, 800); } }, t("renew")),
          iconBtn("trash", t("delete"), async () => { if (await confirmAction(t("confirmDelete", { x: c.id }))) { await guarded(() => api("DELETE", `/api/v2/admin/acme/certs/${encodeURIComponent(c.id)}`)); route(); } }))))
        : h("p", { class: "muted small" }, t("acmeEmpty"))));
}

// SOCKS / HTTP proxy link, or the port-forward target, on the inbound card.
function plainLine(ib, meta, nodes) {
  if (meta.mtproto_secret) {
    const node = (nodes || []).find((n) => n.ID === (ib.nodes || [])[0]);
    const link = `https://t.me/proxy?server=${encodeURIComponent(node?.PublicIP || "SERVER-IP")}&port=${ib.port}&secret=${meta.mtproto_secret}`;
    return h("div", { class: "row small" }, h("span", { class: "badge" }, "Telegram"), h("code", { dir: "ltr", style: "flex:1;overflow:hidden;text-overflow:ellipsis" }, link),
      h("button", { type: "button", class: "ghost", onclick: () => copy(link) }, t("copy")), h("button", { type: "button", class: "ghost", onclick: () => showQR(ib.tag, link) }, "QR"));
  }
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
  const idf = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", placeholder: "vpn-cert", autofocus: true, dir: "ltr" });
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
    dns: "سرورهای DNS (هر خط یکی، خالی = DNS سیستم نود)", outbounds: "خروجی‌ها", addWarp: "+ WARP رایگان", addCustom: "+ خروجی دلخواه (JSON)", addLink: "+ از لینک", linkTitle: "خروجی از لینک اشتراک", linkHelp: "لینک vless، vmess، trojan، ss یا socks یک سرور دیگر را بچسبانید تا ترافیک از آن عبور کند (زنجیره).", linkOk: "خروجی اضافه شد؛ ذخیره را بزنید", tag: "شناسه",
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
    dns: "DNS servers (one per line, empty = node's system DNS)", outbounds: "Outbounds", addWarp: "+ Free WARP", addCustom: "+ Custom outbound (JSON)", addLink: "+ From a link", linkTitle: "Outbound from a share link", linkHelp: "Paste a vless, vmess, trojan, ss or socks link of another server to chain traffic through it.", linkOk: "Outbound added; press Save", tag: "Tag",
    allNodes: "All nodes", nodes: "Nodes", balancers: "Balancers", addBal: "+ Balancer", strategy: "Strategy", rules: "Rules", addRule: "+ Rule",
    rulesHint: "Checked in order; the first match wins. Your rules run after the safe defaults and before ads and Iran.",
    tpl: "Templates", tplAI: "AI sites via WARP", tplMail: "Block mail ports (spam)", tplSpeed: "Speed-test sites direct",
    domains: "Domains (domain:, full:, keyword:, regexp:, geosite:)", ips: "IP / CIDR / geoip:", ports: "Ports (e.g. 25,465 or 1000-2000)", protocols: "Protocol",
    network: "Network", users: "Users (email)", inbounds: "Inbounds", to: "Outbound", remark: "Remark", enabled: "Enabled", del: "Delete", up: "Up", down: "Down",
    geo: "Geo files", geoUpdate: "Update now", geoMissing: "These nodes have no geo files yet; geoip/geosite rules are not applied on them:",
    geoNeed: "Needed for ads, Iran and geo rules. Updated weekly.", any: "any", none: "none", edit: "Edit", warpOk: "WARP account created", empty: "Nothing yet",
    unsaved: "Unsaved changes", adv: "Advanced Xray config", advHint: "Extra sections merged into every node's config: log, policy, dns, observatory, fakedns, reverse, metrics. Checked with Xray before it is saved.", advSave: "Save advanced config", view: "Node's final config", show: "Show" },
};

// flagOf turns a two-letter country code into its flag.
const flagOf = (cc) => (/^[A-Za-z]{2}$/.test(cc || "") ? String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65)) : "🌐");
function countryName(cc) {
  try { return new Intl.DisplayNames([lang === "fa" ? "fa" : "en"], { type: "region" }).of(cc.toUpperCase()); } catch { return cc; }
}

// viewRouting: the everyday choices per node (WARP on its direct and tunnel
// path, with a test that shows where WARP comes out), a few switches for
// everyone, and the full editor folded away under "Advanced". Every change
// here is saved at once.
async function viewRouting() {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const root = h("div", { class: "view routing-simple" });
  const [data, nodes] = await Promise.all([api("GET", "/api/v2/admin/routing"), api("GET", "/api/v2/admin/nodes")]);
  let r = data.routing;
  const paths = data.paths || {};
  const MODES = [["", T("خاموش", "Off")], ["sites", T("سایت‌های تحریمی", "Sanctioned sites")], ["all", T("همهٔ ترافیک", "All traffic")]];
  const results = {}; // node id -> last WARP test result
  const errBox = h("div", { class: "error", role: "alert" });
  const say = (m) => { errBox.textContent = m || ""; };
  const warpFail = (e) => /deadline|timeout|no answer|refused|unreachable/i.test(e || "") ? T("این سرور به WARP دسترسی ندارد (دیتاسنتر یا کلادفلر مسیر را بسته است).", "This server cannot reach WARP (the datacenter or Cloudflare blocks the path).") : (e || T("تست ناموفق بود.", "The test failed."));

  // runTest asks the node to try WARP and waits for the answer.
  async function runTest(id) {
    const { id: task } = await api("POST", "/api/v2/admin/routing/warp-test", { node: id });
    for (let i = 0; i < 30; i++) {
      await new Promise((ok) => setTimeout(ok, 2000));
      const tk = await api("GET", `/api/v2/admin/tasks/${task}`);
      if (tk.status === "done" || tk.status === "failed") return tk.result || { ok: false, error: tk.error || "" };
    }
    return { ok: false, error: "timeout" };
  }
  const resultView = (res) => {
    if (!res) return h("span", { class: "small muted" }, T("هنوز تست نشده", "Not tested yet"));
    if (res.busy) return h("span", { class: "small muted" }, T("در حال تست… (تا یک دقیقه)", "Testing… (up to a minute)"));
    if (!res.ok) return h("span", { class: "badge bad" }, "✕ ", warpFail(res.error));
    return h("span", { class: "warp-ok" }, h("span", { class: "warp-flag", "aria-hidden": "true" }, flagOf(res.loc)),
      h("b", {}, res.loc ? countryName(res.loc) : T("وصل", "Connected")), h("span", { class: "badge good" }, `${res.ms} ms`),
      res.ip ? h("span", { class: "badge", dir: "ltr" }, res.ip) : null, res.warp && res.warp !== "off" ? h("span", { class: "badge good" }, "WARP") : null);
  };

  function nodeCard(n) {
    const p = paths[n.ID] || { direct: true, tunnel: false };
    const cur = (r.warp || {})[n.ID] || {};
    const out = h("div", { class: "warp-result", role: "status" }, resultView(results[n.ID]));
    const testBtn = h("button", { type: "button", class: "ghost", disabled: !n.online }, T("تست WARP", "Test WARP"));
    const test = async () => {
      testBtn.disabled = true; results[n.ID] = { busy: true }; fill(out, resultView(results[n.ID]));
      try { results[n.ID] = await runTest(n.ID); } catch (x) { results[n.ID] = { ok: false, error: x.message }; }
      fill(out, resultView(results[n.ID])); testBtn.disabled = !n.online;
      return results[n.ID];
    };
    testBtn.onclick = test;
    const pathRow = (path, label) => {
      const state = h("span", { class: "small muted", role: "status" });
      const btns = MODES.map(([v, l]) => h("button", { type: "button", class: "seg", disabled: !n.online, "aria-pressed": String(v === (cur[path] || "")), onclick: () => pick(v) }, l));
      const mark = (v) => btns.forEach((b, i) => b.setAttribute("aria-pressed", String(MODES[i][0] === v)));
      const lock = (on) => btns.forEach((b) => { b.disabled = on || !n.online; });
      async function pick(want) {
        const before = cur[path] || "";
        if (want === before) return;
        say(""); lock(true); mark(want);
        try {
          if (want) { // never send customers into a WARP that does not answer
            state.textContent = T("اول WARP این نود تست می‌شود…", "Testing this node's WARP first…");
            const res = await test();
            if (!res.ok) { mark(before); state.textContent = ""; say(`${n.Name || n.ID}: ${warpFail(res.error)} ${T("تغییری ذخیره نشد.", "Nothing was changed.")}`); return; }
          }
          state.textContent = T("در حال ذخیره…", "Saving…");
          r = (await api("PUT", "/api/v2/admin/routing/warp-mode", { node: n.ID, path, mode: want })).routing;
          cur[path] = want; state.textContent = T("✓ ذخیره و برای نود فرستاده شد", "✓ Saved and sent to the node");
        } catch (x) { mark(before); state.textContent = ""; say(x.message); }
        finally { lock(false); }
      }
      return h("div", { class: "path-row" }, h("span", { class: `path-name ${path}` }, label, " — WARP"),
        h("div", { class: "segs", role: "group", "aria-label": `${n.Name || n.ID} · ${label} · WARP` }, btns), state);
    };
    return h("section", { class: `card node-route ${n.online ? "" : "off"}` },
      h("div", { class: "head" }, h("span", { class: "name" }, n.Name || n.ID), h("span", { class: "badge", dir: "ltr" }, n.ID),
        n.online ? null : h("span", { class: "badge bad" }, T("آفلاین", "Offline")), h("span", { class: "spacer" }), testBtn),
      out,
      p.direct ? pathRow("direct", T("مستقیم", "Direct")) : null,
      p.tunnel ? pathRow("tunnel", T("تانل", "Tunnel")) : null,
      !p.direct && !p.tunnel ? h("p", { class: "small muted" }, T("این نود هنوز در هیچ اینباندی نیست.", "This node is not in any inbound yet.")) : null);
  }

  // The switches for everyone: saved as a whole, at once.
  const saveAll = async (note) => {
    say("");
    try { await api("PUT", "/api/v2/admin/routing", r); toast(note || t("saved")); return true; }
    catch (x) { say(x.message); return false; }
  };
  const sw = (key, label, hint) => {
    const c = h("input", { type: "checkbox", checked: !!r[key] });
    c.onchange = async () => { const was = r[key]; r[key] = c.checked; if (!(await saveAll())) { r[key] = was; c.checked = !!was; } };
    return h("label", { class: "check" }, c, h("span", {}, label, hint ? h("span", { class: "small muted" }, " — ", hint) : null));
  };
  const siteBox = (key, label, hint) => {
    const list = h("div", { class: "chips" });
    const inp = h("input", { dir: "ltr", placeholder: "example.com", autocapitalize: "off", spellcheck: "false", "aria-label": label });
    const drawList = () => fill(list, (r[key] || []).map((d) => h("span", { class: "chip" }, h("span", { dir: "ltr" }, d),
      h("button", { type: "button", class: "ghost", "aria-label": T(`حذف ${d}`, `Remove ${d}`), onclick: async () => { const was = r[key]; r[key] = was.filter((x) => x !== d); if (await saveAll()) drawList(); else r[key] = was; } }, "✕"))));
    const add = async () => {
      const v = inp.value.trim();
      if (!v) return;
      const was = r[key] || []; r[key] = [...was, v];
      if (await saveAll()) { r = (await api("GET", "/api/v2/admin/routing")).routing; inp.value = ""; drawList(); } else r[key] = was;
    };
    inp.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } };
    drawList();
    return h("div", { class: "form" }, h("label", {}, h("span", {}, label), h("div", { class: "row tight" }, inp, h("button", { type: "button", onclick: add }, T("افزودن", "Add"))),
      hint ? h("span", { class: "small muted" }, hint) : null), list);
  };
  const iranSel = h("select", {}, [["", T("مثل بقیهٔ سایت‌ها", "Like any other site")], ["block", T("مسدود (پیشنهاد برای جلوگیری از شناسایی سرور)", "Blocked (hides the server from Iranian sites)")]]
    .concat((r.outbounds || []).filter((o) => r.iran === o.tag).map((o) => [o.tag, o.tag])).map(([v, l]) => h("option", { value: v, selected: v === (r.iran || "") }, l)));
  iranSel.onchange = async () => { const was = r.iran; r.iran = iranSel.value; if (!(await saveAll())) { r.iran = was; iranSel.value = was || ""; } };

  const adv = h("details", { class: "card adv-routing" }, h("summary", {}, h("b", {}, T("⚙️ پیشرفته", "⚙️ Advanced")),
    h("span", { class: "small muted" }, " — ", T("قانون‌های دستی، خروجی‌های دیگر، بالانسر، DNS و فایل‌های جغرافیایی", "hand-written rules, other outbounds, balancers, DNS and geo files"))));
  adv.addEventListener("toggle", async () => {
    if (!adv.open) { [...adv.children].slice(1).forEach((c) => c.remove()); route(); return; } // back from the editor: show what it saved
    adv.append(h("p", { class: "small muted" }, T("تغییرهای این بخش با دکمهٔ «ذخیره»ی بالای آن اعمال می‌شوند.", "Changes in this part are applied with its own Save button.")), await viewRoutingAdvanced());
  });

  const sorted = [...nodes].sort((a, b) => Number(b.online) - Number(a.online) || (a.Name || a.ID).localeCompare(b.Name || b.ID));
  fill(root,
    h("div", { class: "row" }, h("h1", {}, T("مسیریابی", "Routing"))),
    h("p", { class: "small muted" }, T("برای روشن کردن WARP روی هر نود، در ردیف «مستقیم» یا «تانل» یکی از دکمه‌های «سایت‌های تحریمی» یا «همهٔ ترافیک» را بزنید. همان لحظه ذخیره و برای نود فرستاده می‌شود.",
      "Choose, per node, where the traffic of its direct and of its tunnel path leaves. Every change is saved and sent to the node at once.")),
    errBox,
    h("div", { class: "node-routes" }, sorted.map(nodeCard)),
    h("section", { class: "card form" }, h("h2", {}, T("برای همهٔ نودها", "For every node")),
      sw("block_torrent", T("مسدود کردن تورنت", "Block BitTorrent"), T("جلوی شکایت دیتاسنتر و تعلیق سرور را می‌گیرد", "avoids abuse complaints")),
      sw("block_ads", T("مسدود کردن تبلیغات", "Block ads")),
      h("label", {}, h("span", {}, T("سایت‌های ایرانی", "Iranian sites")), iranSel),
      siteBox("warp_sites", T("سایت‌های بیشتر برای «فقط سایت‌های تحریمی»", "More sites for “only sanctioned sites”"),
        T(`از قبل در فهرست: ${(data.warp_default_sites || []).join("، ")}`, `Already listed: ${(data.warp_default_sites || []).join(", ")}`)),
      siteBox("block_sites", T("سایت‌های مسدود", "Blocked sites"), T("هیچ مشتری‌ای به این سایت‌ها نمی‌رسد.", "No customer reaches these sites."))),
    adv);
  return root;
}

async function viewRoutingAdvanced() {
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
  function addLink() {
    const inp = h("textarea", { rows: "3", dir: "ltr", placeholder: "vless://…" });
    const msg = h("p", { class: "error small" });
    const go = h("button", { type: "button", class: "primary", onclick: async () => {
      msg.textContent = "";
      try {
        const o = await api("POST", "/api/v2/admin/routing/from-link", { link: inp.value });
        o.tag = uniq(o.tag);
        o.config = { ...(typeof o.config === "string" ? JSON.parse(o.config) : o.config), tag: o.tag };
        r.outbounds.push(o);
        d.close(); toast(L.linkOk); touch(); draw();
      } catch (x) { msg.textContent = x.message; }
    } }, t("save"));
    const d = openModal(L.linkTitle, h("div", { class: "form" }, h("p", { class: "small muted" }, L.linkHelp), inp, msg), [go]);
    inp.focus();
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
      h("div", { class: "row section" }, h("h2", {}, L.outbounds), h("button", { class: "spacer", onclick: addWarp }, L.addWarp), h("button", { class: "ghost", onclick: addLink }, L.addLink), h("button", { class: "ghost", onclick: addCustom }, L.addCustom)),
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
// The preset the HUB builds for the studio's choice.
function profilePreset(p) {
  if (p.kit) return "template";
  return { fallback: "vless-tls-fallback" }[p.kind] || p.kind;
}
function profileLabel(p) {
  if (!p.kit) return profilePreset(p);
  const t2 = p.kit.state().tpl, s = t2.streamSettings || {};
  return [String(t2.protocol).toUpperCase(), s.network, s.security, t2.settings?.flow ? "Vision" : ""].filter(Boolean).join(" · ");
}

// Words of the inbound builder.
const BUILDER = {
  fa: {
    kinds: [["vless", "VLESS", "REALITY، TLS، CDN"], ["vmess", "VMess", "سازگار با برنامه‌های قدیمی"], ["trojan", "Trojan", "REALITY یا TLS"], ["shadowsocks", "Shadowsocks", "2022 یا کلاسیک"],
      ["hysteria2", "Hysteria2", "QUIC، سریع روی شبکهٔ بد"], ["tuic", "TUIC v5", "QUIC با کنترل ازدحام"], ["amneziawg", "AmneziaWG", "وایرگارد ضد DPI"], ["wireguard", "WireGuard", "VPN کامل"],
      ["mtproto", "MTProto", "پروکسی تلگرام"], ["socks", "SOCKS5", "پروکسی با رمز"], ["http", "HTTP", "پروکسی با رمز"], ["dokodemo", "انتقال پورت", "dokodemo-door / تانل"], ["fallback", "چندتایی روی یک پورت", "TLS + فال‌بک"]],
    quick: "قالب آماده:",
    templates: [["Reality + Vision (پیشنهادی)", { protocol: "vless", network: "tcp", security: "reality", vision: true }], ["Reality + gRPC", { protocol: "vless", network: "grpc", security: "reality" }],
      ["Reality + XHTTP", { protocol: "vless", network: "xhttp", security: "reality" }], ["CDN: WS + TLS", { protocol: "vless", network: "ws", security: "tls" }],
      ["CDN: XHTTP + TLS", { protocol: "vless", network: "xhttp", security: "tls" }], ["پشت CDN/تانل: WS بدون TLS", { protocol: "vless", network: "ws", security: "none" }]],
    protocol: "پروتکل", transport: "انتقال", security: "امنیت", none: "بدون",
    fixedTls: "REALITY با این انتخاب کار نمی‌کند؛ امنیت به TLS تغییر کرد.", fixedVless: "REALITY با VMess کار نمی‌کند؛ پروتکل به VLESS تغییر کرد.", fixedTcp: "REALITY فقط روی TCP، gRPC و XHTTP است؛ انتقال به TCP تغییر کرد.",
    noRealityVmess: "REALITY با VMess کار نمی‌کند", noRealityNet: "REALITY فقط روی TCP، gRPC و XHTTP کار می‌کند",
    hint: (s) => s.security === "reality" ? (s.network === "tcp" ? "بهترین انتخاب برای اتصال مستقیم؛ شبیه سایت واقعی دیده می‌شود." : s.network === "grpc" ? "برای شبکه‌هایی که TCP ساده را می‌بندند؛ multiMode سریع‌ترش می‌کند." : "XHTTP با REALITY؛ مقاوم در برابر فیلتر و قابل عبور از برخی CDNها.")
      : s.security === "tls" ? (["ws", "httpupgrade", "xhttp", "grpc"].includes(s.network) ? "مناسب پشت CDN (مثلاً Cloudflare) با دامنهٔ خودتان." : "TLS مستقیم با گواهی دامنهٔ خودتان.")
      : "بدون رمزنگاری لایهٔ انتقال؛ فقط پشت CDN با TLS یا داخل تانل استفاده کنید.",
    service: "نام سرویس gRPC", multi: "multiMode (چند جریان، سریع‌تر)", path: "مسیر (path)", host: "Host", hostHint: "برای CDN: دامنه‌ای که روی CDN است؛ خالی = هر چیزی.", auto: "خالی = تصادفی",
    xmode: "حالت XHTTP", xmodeHint: "auto معمولاً بهترین است؛ packet-up برای CDNهایی که استریم را قطع می‌کنند.",
    realitySni: "سایت هدف (SNI)", realitySniHint: "یک سایت خارجی معتبر با TLS 1.3 و HTTP/2؛ «پیدا کردن SNI» بهترین را از روی نود می‌سنجد.",
    fp: "اثر انگشت مرورگر (fingerprint)", dest: "مقصد (dest)", names: "نام‌های دیگر (اختیاری)", namesHint: "نام‌های دیگری از همان سایت که کلاینت می‌تواند بفرستد؛ با کاما جدا کنید.",
    domain: "دامنه (SNI)", selfHint: "بدون گواهی، یک گواهی خودامضا ساخته می‌شود و کلاینت‌ها بررسی گواهی را رد می‌کنند (رمزنگاری برقرار است).",
    noneWarn: "بدون امنیت فقط وقتی امن است که پشت CDN با TLS یا داخل تانل رمزدار باشد.", vision: "Vision (xtls-rprx-vision) — توصیه می‌شود",
    fakeTls: "دامنهٔ fake TLS", target: "مقصد (host:port)", subnet: "زیرشبکهٔ تانل (اختیاری)",
    info: { ss: "Shadowsocks 2022 با AES-128-GCM؛ بدون لایهٔ TLS، مناسب تانل‌ها و کلاینت‌هایی که REALITY ندارند.", hysteria2: "Hysteria2 روی QUIC (پورت UDP)؛ روی شبکه‌های پرافت سریع است.",
      tuic: "TUIC v5 روی QUIC (پورت UDP)، کنار Xray داخل عامل نود (v2.7 به بالا).", amneziawg: "AmneziaWG: وایرگارد با بسته‌های اضافه که DPI نمی‌شناسد (عامل v2.6 به بالا). کاربرها فایل ‎.conf می‌گیرند.",
      wireguard: "WireGuard ساده (UDP).", mtproto: "پروکسی تلگرام با fake TLS؛ لینک t.me روی کارت اینباند می‌آید.", proxy: "پروکسی با نام کاربری و رمز تصادفی؛ هرگز پروکسی باز نیست.",
      dokodemo: "هر چیزی که به این پورت برسد به مقصد فرستاده می‌شود؛ کاربر ندارد.", fallback: "یک پورت TLS برای VLESS Vision و سه مسیر WebSocket (VLESS، VMess، Trojan)؛ گواهی دامنه لازم است." },
  },
  en: {
    kinds: [["vless", "VLESS", "REALITY, TLS, CDN"], ["vmess", "VMess", "for older apps"], ["trojan", "Trojan", "REALITY or TLS"], ["shadowsocks", "Shadowsocks", "2022 or classic"],
      ["hysteria2", "Hysteria2", "QUIC, fast on bad links"], ["tuic", "TUIC v5", "QUIC with congestion control"], ["amneziawg", "AmneziaWG", "WireGuard against DPI"], ["wireguard", "WireGuard", "full VPN"],
      ["mtproto", "MTProto", "Telegram proxy"], ["socks", "SOCKS5", "proxy with a password"], ["http", "HTTP", "proxy with a password"], ["dokodemo", "Port forward", "dokodemo-door / tunnel"], ["fallback", "Several on one port", "TLS + fallbacks"]],
    quick: "Templates:",
    templates: [["Reality + Vision (recommended)", { protocol: "vless", network: "tcp", security: "reality", vision: true }], ["Reality + gRPC", { protocol: "vless", network: "grpc", security: "reality" }],
      ["Reality + XHTTP", { protocol: "vless", network: "xhttp", security: "reality" }], ["CDN: WS + TLS", { protocol: "vless", network: "ws", security: "tls" }],
      ["CDN: XHTTP + TLS", { protocol: "vless", network: "xhttp", security: "tls" }], ["Behind CDN/tunnel: WS, no TLS", { protocol: "vless", network: "ws", security: "none" }]],
    protocol: "Protocol", transport: "Transport", security: "Security", none: "None",
    fixedTls: "REALITY does not work with this choice; security switched to TLS.", fixedVless: "REALITY does not work with VMess; protocol switched to VLESS.", fixedTcp: "REALITY works only over TCP, gRPC and XHTTP; transport switched to TCP.",
    noRealityVmess: "REALITY does not work with VMess", noRealityNet: "REALITY works only over TCP, gRPC and XHTTP",
    hint: (s) => s.security === "reality" ? (s.network === "tcp" ? "Best for direct connections; looks like a real website." : s.network === "grpc" ? "For networks that block plain TCP; multiMode makes it faster." : "XHTTP with REALITY; resists filtering and passes some CDNs.")
      : s.security === "tls" ? (["ws", "httpupgrade", "xhttp", "grpc"].includes(s.network) ? "Good behind a CDN (e.g. Cloudflare) with your own domain." : "Direct TLS with your domain's certificate.")
      : "No transport encryption; use only behind a TLS CDN or inside a tunnel.",
    service: "gRPC service name", multi: "multiMode (several streams, faster)", path: "Path", host: "Host", hostHint: "For a CDN: the domain on the CDN; empty = anything.", auto: "empty = random",
    xmode: "XHTTP mode", xmodeHint: "auto is usually best; packet-up for CDNs that cut streams.",
    realitySni: "Target site (SNI)", realitySniHint: "A real foreign site with TLS 1.3 and HTTP/2; \"Find SNI\" measures the best from the node.",
    fp: "Browser fingerprint", dest: "Destination (dest)", names: "Other names (optional)", namesHint: "Other names of the same site clients may send; comma separated.",
    domain: "Domain (SNI)", selfHint: "Without a certificate a self-signed one is made and clients skip certificate checks (still encrypted).",
    noneWarn: "No security is safe only behind a TLS CDN or inside an encrypted tunnel.", vision: "Vision (xtls-rprx-vision) — recommended",
    fakeTls: "Fake-TLS domain", target: "Target (host:port)", subnet: "Tunnel subnet (optional)",
    info: { ss: "Shadowsocks 2022 with AES-128-GCM; no TLS layer, good for tunnels and clients without REALITY.", hysteria2: "Hysteria2 over QUIC (UDP port); fast on lossy networks.",
      tuic: "TUIC v5 over QUIC (UDP port), run beside Xray by the node agent (v2.7+).", amneziawg: "AmneziaWG: WireGuard with junk packets DPI does not recognise (agent v2.6+). Clients get a .conf file.",
      wireguard: "Plain WireGuard (UDP).", mtproto: "Telegram proxy with fake TLS; the t.me link shows on the inbound card.", proxy: "A proxy with a random username and password; never an open proxy.",
      dokodemo: "Everything reaching this port goes to the target; no users.", fallback: "One TLS port for VLESS Vision and three WebSocket paths (VLESS, VMess, Trojan); needs a domain certificate." },
  },
};

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
  const defPort = () => +(st.profile.kit?.state().tpl.port) || 443;
  const newHost = (path, node) => ({ path, enabled: true, remark: "", address: path === "direct" ? (node?.PublicIP || "") : "", port: defPort(),
    sni: "", host: "", path_override: "", alpn: "", fingerprint: "", allow_insecure: false, exclude: [], fragment: false, mux: 0, _adv: false });
  const newRoute = (node) => ({ listen_ip: "", direct_port: defPort(), tunnel_port: 0, hosts: [newHost("direct", node)] });
  if (!isNew) {
    const [routes, swaps] = await Promise.all([api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`), api("GET", `/api/v2/admin/swaps?inbound=${encodeURIComponent(tag)}`)]);
    for (const r of routes) st.routes[r.node_id] = { ...r, hosts: r.hosts.map((x) => ({ ...x, exclude: x.exclude || [], _adv: !!(x.sni || x.host || x.path_override || x.alpn || x.fingerprint || x.allow_insecure || x.exclude?.length || x.fragment || x.mux) })) };
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
    const p = st.profile, fa = lang === "fa";
    if (TEMPLATE_KINDS.includes(p.kind)) {
      if (!p.kit) { err.textContent = fa ? "صبر کنید تا کلیدها آماده شوند." : "Wait for the keys."; return false; }
      const t2 = p.kit.state().tpl, s2 = t2.streamSettings || {};
      if (s2.security === "reality" && !(s2.realitySettings?.serverNames || []).length) { err.textContent = fa ? "برای REALITY سایت هدف (serverNames) لازم است." : "REALITY needs a target name (serverNames)."; return false; }
      if (t2.protocol === "dokodemo-door" && !(t2.settings?.address && t2.settings?.port)) { err.textContent = fa ? "آدرس و پورت مقصد را وارد کنید." : "Enter the target address and port."; return false; }
      if (["socks", "http"].includes(t2.protocol) && !(t2.settings?.accounts?.[0]?.user && t2.settings.accounts[0].pass)) { err.textContent = fa ? "نام کاربری و رمز لازم است." : "Username and password are required."; return false; }
    }
    if (p.kind === "fallback" && !p.cert) { err.textContent = t("needCert"); return false; }
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
    body.replaceChildren(...[drawProfile, drawNodes, drawSwaps, drawReview][st.step]().filter((x) => x != null));
  }

  // Step 1. Xray protocols (VLESS, VMess, Trojan, Shadowsocks, SOCKS, HTTP,
  // port forward) open the full inbound form — the same one used to edit,
  // in 3x-ui's order — on a draft the HUB prepares with fresh keys. The
  // others (QUIC, WireGuard kinds, MTProto, fallback) are built by the HUB.
  const TEMPLATE_KINDS = ["vless", "vmess", "trojan", "shadowsocks", "socks", "http", "dokodemo"];
  async function loadDraft(kind, choice = {}) {
    const p = st.profile;
    p.kind = kind; p.draft = null; p.kit = null; err.textContent = "";
    if (!TEMPLATE_KINDS.includes(kind)) { draw(); return; }
    draw();
    const req = { vless: { preset: "custom", stream: { protocol: "vless", network: "tcp", security: "reality", vision: true } }, trojan: { preset: "custom", stream: { protocol: "trojan", network: "tcp", security: "reality" } },
      vmess: { preset: "custom", stream: { protocol: "vmess", network: "ws", security: "none" } }, shadowsocks: { preset: "ss2022" }, socks: { preset: "socks" }, http: { preset: "http" }, dokodemo: { preset: "dokodemo" } }[kind];
    if (req.stream && Object.keys(choice).length) Object.assign(req.stream, { vision: false }, choice);
    try {
      const d = await api("POST", "/api/v2/admin/inbounds/draft", { ...req, sni: "www.speedtest.net", port: p.port || 443, remark: p.remark || "Dark VPN" });
      const tpl = typeof d.template === "string" ? JSON.parse(d.template) : d.template, meta = typeof d.meta === "string" ? JSON.parse(d.meta) : d.meta;
      meta.remark ||= p.remark || "Dark VPN";
      p.lim = { cap: 0, ends: "", speed: 0 };
      p.draft = { tpl, meta, enabled: true };
      p.kit = inboundFormKit({ tpl, meta, enabled: true, lim: p.lim, view: {}, routed: false, certList: ctx.certs || [], err, draw: () => draw(), create: true, nodes });
    } catch (x) { err.textContent = x.message; }
    draw();
  }

  function drawProfile() {
    const p = st.profile;
    const fa = lang === "fa";
    const B = BUILDER[fa ? "fa" : "en"];
    if (!p.kind) { loadDraft("vless"); p.kind = "vless"; }
    const kids = [];
    kids.push(h("div", { class: "two" }, field(t("tag"), bind(p, "tag", { required: true, placeholder: "de-reality", autofocus: true })),
      p.kit ? h("span") : field(t("remark"), bind(p, "remark"))));
    kids.push(h("div", { class: "kind-grid" }, B.kinds.map(([k, title, sub]) => h("button", { type: "button", class: `kind ${p.kind === k ? "on" : ""}`, "aria-pressed": String(p.kind === k),
      onclick: () => { if (k === "proxy") p.proxy ||= "socks"; loadDraft(k); } }, h("b", {}, title), h("small", {}, sub)))));
    if (["vless", "trojan", "vmess"].includes(p.kind)) kids.push(h("div", { class: "tpl-row" }, h("span", { class: "muted small" }, B.quick), B.templates.map(([label, x]) =>
      h("button", { type: "button", class: "chip", onclick: () => loadDraft(x.protocol, x) }, label))));
    if (TEMPLATE_KINDS.includes(p.kind)) {
      if (!p.kit) kids.push(h("p", { class: "muted small" }, fa ? "در حال آماده‌سازی کلیدها…" : "Preparing keys…"));
      else {
        const s0 = p.kit.state().tpl;
        const ss = s0.streamSettings || {};
        if (["vless", "vmess", "trojan"].includes(s0.protocol)) kids.push(h("p", { class: "b-sum" }, B.hint({ security: ss.security || "none", network: ss.network || "tcp" })));
        kids.push(...p.kit.drawForm());
      }
      return kids;
    }
    // HUB-built kinds.
    const info = B.info[p.kind];
    if (info) kids.push(h("p", { class: "small muted" }, info));
    const certSel = () => {
      const el = h("select", {}, h("option", { value: "" }, t("selfSigned")), (ctx.certs || []).map((c) => h("option", { value: c.id, selected: c.id === p.cert }, `${c.id} · ${c.domains.join(", ")}`)));
      el.addEventListener("change", () => { p.cert = el.value; const c = (ctx.certs || []).find((x) => x.id === p.cert); if (c?.domains[0]) p.sni = c.domains[0]; draw(); });
      return el;
    };
    if (["hysteria2", "tuic", "fallback"].includes(p.kind)) kids.push(field(t("certFor"), certSel()), field(B.domain, bind(p, "sni", { dir: "ltr" })));
    if (p.kind === "mtproto") kids.push(field(B.fakeTls, bind(p, "sni", { dir: "ltr", placeholder: "www.google.com" })));
    if (p.kind === "wireguard" || p.kind === "amneziawg") kids.push(field(B.subnet, bind(p, "path", { dir: "ltr", placeholder: p.kind === "wireguard" ? "10.66.0.0/16" : "10.67.0.0/16" })));
    return kids;
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
        h("div", { class: "row small" },
          h("label", { class: "check" }, (() => { const c = h("input", { type: "checkbox", checked: !!hst.fragment }); c.addEventListener("change", () => { hst.fragment = c.checked; }); return c; })(), lang === "fa" ? "Fragment (فقط پروفایل Xray JSON)" : "Fragment (Xray JSON profile only)"),
          h("label", {}, h("span", {}, lang === "fa" ? "Mux (همزمانی، ۰ = خاموش؛ نه با Vision)" : "Mux (concurrency, 0 = off; not with Vision)"), bind(hst, "mux", { type: "number", min: "0", max: "128", style: "max-inline-size:110px" }, true))),
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
    return [isNew ? h("p", {}, h("strong", {}, st.profile.tag), h("span", { class: "muted", dir: "ltr" }, ` · ${profileLabel(st.profile)}`)) : null, h("div", { class: "review" }, lines)];
  }

  async function save() {
    err.textContent = "";
    const routes = Object.entries(st.routes).map(([node_id, r]) => ({ node_id, listen_ip: (r.listen_ip || "").trim(), direct_port: +r.direct_port, tunnel_port: +r.tunnel_port || 0,
      hosts: r.hosts.map(({ _adv, ...x }) => ({ ...x, port: +x.port })) }));
    const theTag = isNew ? st.profile.tag : tag;
    try {
      if (isNew) {
        const p = st.profile;
        const preset = profilePreset(p);
        const ks = p.kit ? p.kit.state() : null;
        if (ks) { const s3 = ks.tpl.streamSettings || {}; if (s3.security === "reality" && s3.realitySettings) ks.meta.short_id = (s3.realitySettings.shortIds || [])[0] || ks.meta.short_id; }
        const usesCert = ["hysteria2", "tuic", "vless-tls-fallback"].includes(preset);
        await api("POST", "/api/v2/admin/inbounds/compose", { tag: p.tag, preset, template: ks ? ks.tpl : undefined, meta: ks ? ks.meta : undefined, sni: (p.sni || "").trim(), path: (p.path || "").trim(), remark: ks ? (ks.meta.remark || "") : (p.remark || "").trim(), cert: usesCert ? p.cert : "", routes });
        if (ks && (+p.lim.cap || p.lim.ends)) await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(p.tag)}/limits`, { cap_gb: +p.lim.cap || 0, ends_unix: p.lim.ends ? Math.floor(new Date(p.lim.ends + "T23:59:59").getTime() / 1000) : 0 });
        if (ks && +p.lim.speed) await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(p.tag)}/speed`, { kbps: Math.round(+p.lim.speed * 1000) });
        if (ks && !ks.enabled) await api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(p.tag)}/enabled`, { Enabled: false });
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
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", value: `${ib.tag}-swap${list.length ? list.length + 1 : ""}` });
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
  const tag = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", value: `${ib.tag}-2`, dir: "ltr", autofocus: true });
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

// The inbound form, shared by "new inbound" and "edit inbound": general,
// protocol, transport, security, socket options, sniffing and fallbacks.
function inboundFormKit(o) {
  let tpl = o.tpl, meta = o.meta, enabled = o.enabled;
  const { lim, view, routed, certList, err } = o;
  const draw = () => o.draw();
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
  // Segmented choice, the same control as the inbound builder.
  const segPick = (opts, get, set, onChange) => h("div", { class: "seg", role: "radiogroup" }, opts.map(([v, l]) => h("button", { type: "button", role: "radio",
    "aria-checked": String(String(get() ?? "") === v), class: String(get() ?? "") === v ? "on" : "", onclick: () => { if (String(get() ?? "") === v) return; set(v); if (onChange) onChange(); } }, l)));
  // "Name: value" lines <-> a headers object.
  const hdrs = (o2, key) => {
    const el = h("textarea", { rows: "2", dir: "ltr", spellcheck: "false", placeholder: "User-Agent: Mozilla/5.0" }, Object.entries(o2[key] || {}).map(([k, v]) => `${k}: ${v}`).join("\n"));
    el.addEventListener("input", () => { const m = {}; for (const l of el.value.split("\n")) { const i = l.indexOf(":"); if (i > 0) m[l.slice(0, i).trim()] = l.slice(i + 1).trim(); } if (Object.keys(m).length) o2[key] = m; else delete o2[key]; });
    return el;
  };
  const num = (o2, key, attrs = {}) => input(() => o2[key], (v) => { if (v === undefined || v === "" || Number.isNaN(v)) delete o2[key]; else o2[key] = v; }, { type: "number", dir: "ltr", ...attrs });
  const opt = (o2, key, label) => check(() => o2[key], (v) => { if (v) o2[key] = true; else delete o2[key]; }, label);
  const section = (title, ...kids) => h("fieldset", { class: "ie-sec" }, h("legend", {}, title), ...kids);
  const keys = (kind, extra = {}) => api("POST", "/api/v2/admin/keys", { kind, ...extra });
  const randB64 = (n) => { const b = new Uint8Array(n); crypto.getRandomValues(b); return btoa(String.fromCharCode(...b)); };

  let jsonTpl, jsonMeta;
  function drawJSON() {
    jsonTpl = h("textarea", { class: "code", rows: "18", spellcheck: "false", dir: "ltr" }, JSON.stringify(tpl, null, 2));
    jsonMeta = h("textarea", { class: "code", rows: "8", spellcheck: "false", dir: "ltr" }, JSON.stringify(meta, null, 2));
    return [h("p", { class: "small muted" }, ie("jsonHint")), field(t("template"), jsonTpl), field(t("metaJson"), jsonMeta)];
  }

  function drawForm() {
    const proto = tpl.protocol;
    const settings = obj(tpl, "settings");
    const hasStream = ["vless", "vmess", "trojan", "hysteria"].includes(proto);
    const out = [];
    // General
    const stx = tpl.streamSettings || {};
    out.push(h("p", { class: "b-sum" }, h("b", { dir: "ltr" }, [String(proto).toUpperCase(), hasStream && proto !== "hysteria" ? (stx.network || "tcp") : "", hasStream ? (proto === "hysteria" ? "TLS" : (stx.security || "none").toUpperCase()) : "", settings.flow ? "Vision" : ""].filter(Boolean).join(" · ")),
      o.create ? (lang === "fa" ? " — پورت این‌جا پورت پیش‌فرض نودهاست؛ در قدم بعد برای هر نود جدا قابل تغییر است." : " — the port here is the default for nodes; the next step can change it per node.")
        : lang === "fa" ? " — پروتکل بعد از ساخت عوض نمی‌شود (لینک کاربرها به آن بسته است)؛ انتقال و امنیت را همین‌جا عوض کنید." : " — the protocol cannot change after creation (client links depend on it); change transport and security here."));
    out.push(section(ie("general"),
      h("div", { class: "two" },
        field(ie("protocol"), h("input", { value: proto, disabled: true, dir: "ltr" })),
        field(ie("remark"), input(() => meta.remark, (v) => { meta.remark = v; }))),
      h("div", { class: "two" },
        field(ie("listen"), input(() => tpl.listen, (v) => { tpl.listen = v || undefined; }, { placeholder: "0.0.0.0", dir: "ltr" })),
        field(ie("port"), input(() => tpl.port, (v) => { tpl.port = v; }, { type: "number", min: "1", max: "65535", dir: "ltr", disabled: routed }), routed ? ie("portRouted") : null)),
      check(() => enabled, (v) => { enabled = v; }, ie("enabled")),
      h("div", { class: "two" },
        field(lang === "fa" ? "سقف ترافیک کل اینباند (گیگ، ۰ = ندارد)" : "Inbound traffic cap (GB, 0 = none)", input(() => lim.cap, (v) => { lim.cap = v || 0; }, { type: "number", min: "0", step: "1", dir: "ltr" }),
          view.cap_bytes ? `${bytes(view.cap_used || 0)} / ${bytes(view.cap_bytes)}` : (lang === "fa" ? "از لحظهٔ ذخیره شمرده می‌شود" : "Counted from when you save")),
        field(lang === "fa" ? "تاریخ پایان (خالی = ندارد)" : "End date (empty = none)", input(() => lim.ends, (v) => { lim.ends = v; }, { type: "date", dir: "ltr" }),
          lang === "fa" ? "بعد از سقف یا تاریخ، اینباند خاموش می‌شود و به مالک‌ها خبر داده می‌شود" : "Past the cap or date the inbound turns off and owners are told")),
      me.role === "owner" ? field(lang === "fa" ? "سقف سرعت مشترک (مگابیت، ۰ = بدون محدودیت)" : "Shared speed cap (Mbit/s, 0 = none)",
        input(() => lim.speed, (v) => { lim.speed = v || 0; }, { type: "number", min: "0", step: "0.5", dir: "ltr" }),
        lang === "fa" ? "همهٔ کاربرهای این اینباند روی هر سرور همین سرعت را بین خودشان تقسیم می‌کنند (مستقیم و تانل با هم)" : "Everyone on this inbound shares this speed on each server (direct and tunnel together)") : null));
    // Protocol
    if (proto === "vless") out.push(drawVless(settings));
    if (proto === "shadowsocks") out.push(drawSS(settings));
    if (proto === "dokodemo-door" || proto === "tunnel") out.push(drawDoko(settings));
    if (proto === "socks" || proto === "http" || proto === "mixed") out.push(drawProxy(settings, proto));
    if (proto === "wireguard") out.push(section(ie("protocol"), field(ie("mtu"), input(() => meta.mtu, (v) => { meta.mtu = v; }, { type: "number", min: "1280", max: "1500", placeholder: "1420", dir: "ltr" }))));
    if (hasStream && proto !== "hysteria") out.push(drawTransport());
    if (hasStream) out.push(drawSecurity());
    if (["vless", "trojan"].includes(proto) && ["tcp", "raw"].includes(tpl.streamSettings?.network || "tcp") && (tpl.streamSettings?.security || "none") !== "reality") out.push(drawFallbacks(settings));
    if (!["wireguard", "hysteria"].includes(proto)) out.push(drawSockopt());
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

  function drawFallbacks(settings) {
    const FA = lang === "fa";
    const list = Array.isArray(settings.fallbacks) ? settings.fallbacks : [];
    const box = h("div", { class: "form" });
    const commit = () => { if (list.length) settings.fallbacks = list; else delete settings.fallbacks; };
    const redrawFb = () => box.replaceChildren(...list.map((f, i) => h("div", { class: "fb-row" },
      input(() => f.name, (v) => { if (v) f.name = v; else delete f.name; }, { dir: "ltr", placeholder: "SNI" }),
      input(() => f.alpn, (v) => { if (v) f.alpn = v; else delete f.alpn; }, { dir: "ltr", placeholder: "ALPN" }),
      input(() => f.path, (v) => { if (v) f.path = v; else delete f.path; }, { dir: "ltr", placeholder: "/path" }),
      input(() => f.dest, (v) => { f.dest = /^\d+$/.test(v) ? +v : v; }, { dir: "ltr", placeholder: "8080 / 127.0.0.1:80" }),
      select([["0", "xver 0"], ["1", "xver 1"], ["2", "xver 2"]], () => String(f.xver || 0), (v) => { if (+v) f.xver = +v; else delete f.xver; }),
      h("button", { type: "button", class: "ghost danger", onclick: () => { list.splice(i, 1); commit(); redrawFb(); } }, "✕"))), list.length ? null : h("p", { class: "small muted" }, FA ? "فال‌بکی تعریف نشده." : "No fallbacks."));
    redrawFb();
    return h("details", { class: "ie-sec adv-box", open: list.length > 0 }, h("summary", {}, FA ? "فال‌بک‌ها (Fallbacks)" : "Fallbacks"),
      h("p", { class: "small muted" }, FA ? "اتصال‌هایی که این پروتکل نیستند بر اساس SNI، ALPN یا مسیر به مقصد دیگری (مثلاً یک وب‌سایت یا اینباند دیگر) فرستاده می‌شوند." : "Connections that are not this protocol go to another destination (a website or another inbound) by SNI, ALPN or path."),
      box, h("button", { type: "button", class: "ghost", onclick: () => { list.push({ dest: 80 }); commit(); redrawFb(); } }, FA ? "+ افزودن فال‌بک" : "+ Add fallback"));
  }

  function drawDoko(settings) {
    const FA = lang === "fa";
    return section(ie("protocol"),
      h("div", { class: "two" }, field(FA ? "آدرس مقصد" : "Target address", input(() => settings.address, (v) => { settings.address = v; meta.target = `${v}:${settings.port || ""}`; }, { dir: "ltr", placeholder: "1.2.3.4" })),
        field(FA ? "پورت مقصد" : "Target port", input(() => settings.port, (v) => { settings.port = v; meta.target = `${settings.address || ""}:${v}`; }, { type: "number", min: "1", max: "65535", dir: "ltr" }))),
      h("div", { class: "two" }, field(FA ? "شبکه" : "Network", select([["tcp,udp", "TCP + UDP"], ["tcp", "TCP"], ["udp", "UDP"]], () => settings.network || "tcp,udp", (v) => { settings.network = v; })),
        h("div", {}, opt(settings, "followRedirect", "followRedirect"))));
  }

  function drawProxy(settings, proto) {
    const FA = lang === "fa";
    const acc = (settings.accounts && settings.accounts[0]) || (settings.accounts = [{ user: meta.proxy_user || "", pass: meta.proxy_pass || "" }])[0];
    const kids = [h("div", { class: "two" },
      field(FA ? "نام کاربری" : "Username", input(() => acc.user, (v) => { acc.user = v; meta.proxy_user = v; }, { dir: "ltr" })),
      field(FA ? "رمز" : "Password", input(() => acc.pass, (v) => { acc.pass = v; meta.proxy_pass = v; }, { dir: "ltr" })))];
    if (proto !== "http") kids.push(h("div", { class: "two" }, h("div", {}, opt(settings, "udp", "UDP")), field(FA ? "IP برای UDP (اختیاری)" : "UDP address (optional)", input(() => settings.ip, (v) => { if (v) settings.ip = v; else delete settings.ip; }, { dir: "ltr", placeholder: "127.0.0.1" }))));
    if (proto === "http") kids.push(opt(settings, "allowTransparent", "allowTransparent"));
    kids.push(h("p", { class: "small muted" }, FA ? "پروکسی بدون رمز ساخته نمی‌شود (پروکسی باز سوءاستفاده می‌شود)." : "Never without a password (open proxies get abused)."));
    return section(ie("protocol"), ...kids);
  }

  function drawSS(settings) {
    const sizes = { "2022-blake3-aes-128-gcm": 16, "2022-blake3-aes-256-gcm": 32, "2022-blake3-chacha20-poly1305": 32, "aes-128-gcm": 0, "aes-256-gcm": 0, "chacha20-poly1305": 0, "xchacha20-poly1305": 0 };
    const key = input(() => settings.password, (v) => { settings.password = v; }, { dir: "ltr" });
    const meth = select(Object.keys(sizes).map((m) => [m, m + (sizes[m] ? "" : lang === "fa" ? " (کلاسیک)" : " (classic)")]), () => settings.method, (v) => { settings.method = v; if (sizes[v]) { settings.password = randB64(sizes[v]); key.value = settings.password; } else delete settings.password; draw(); });
    const net2 = field(lang === "fa" ? "شبکه" : "Network", select([["tcp,udp", "TCP + UDP"], ["tcp", "TCP"], ["udp", "UDP"]], () => settings.network || "tcp,udp", (v) => { settings.network = v; }));
    if (!sizes[settings.method]) return section(ie("protocol"), field(ie("method"), meth), net2,
      h("p", { class: "small muted" }, lang === "fa" ? "روش کلاسیک: هر کاربر رمز خودش را دارد. برای برنامه‌های قدیمی؛ روش‌های 2022 امن‌ترند." : "Classic method: each client has its own password. For older apps; 2022 methods are safer."));
    return section(ie("protocol"), h("p", { class: "small muted" }, ie("ssWarn")),
      field(ie("method"), meth),
      field(ie("ssKey"), h("div", { class: "row tight" }, key, h("button", { type: "button", class: "ghost", onclick: () => { settings.password = randB64(sizes[settings.method] || 32); key.value = settings.password; } }, ie("newKey")))), net2);
  }

  function drawTransport() {
    const st = obj(tpl, "streamSettings");
    const net = st.network || "tcp";
    const kids = [];
    const netSel = segPick([["tcp", "TCP"], ["kcp", "mKCP"], ["ws", "WebSocket"], ["grpc", "gRPC"], ["httpupgrade", "HTTPUpgrade"], ["xhttp", "XHTTP"]], () => (net === "splithttp" ? "xhttp" : net), (v) => {
      for (const k of ["tcpSettings", "rawSettings", "kcpSettings", "wsSettings", "grpcSettings", "httpupgradeSettings", "xhttpSettings", "splithttpSettings"]) delete st[k];
      if (v === "kcp") st.kcpSettings = { mtu: 1350, tti: 20, uplinkCapacity: 5, downlinkCapacity: 20, congestion: false, readBufferSize: 2, writeBufferSize: 2, header: { type: "none" } };
      st.network = v;
      if (v === "ws") st.wsSettings = { path: "/" + Math.random().toString(36).slice(2, 8) };
      if (v === "grpc") st.grpcSettings = { serviceName: Math.random().toString(36).slice(2, 8) };
      if (v === "httpupgrade") st.httpupgradeSettings = { path: "/" + Math.random().toString(36).slice(2, 8) };
      if (v === "xhttp") st.xhttpSettings = { path: "/" + Math.random().toString(36).slice(2, 8), mode: "auto" };
      if (st.security === "reality" && !["tcp", "grpc", "xhttp"].includes(v)) {
        // REALITY cannot run here: switch to TLS (as the builder did) and say so.
        const name = (st.realitySettings?.serverNames || [])[0] || "";
        delete st.realitySettings; delete meta.public_key; delete meta.short_id; delete meta.spider_x;
        st.security = "tls";
        st.tlsSettings = { serverName: certList[0]?.domains?.[0] || name, alpn: v === "ws" || v === "httpupgrade" ? ["http/1.1"] : ["h2", "http/1.1"], certificates: certList[0] ? [{ dark_cert: certList[0].id }] : [] };
        err.textContent = lang === "fa" ? "REALITY روی این انتقال کار نمی‌کند؛ امنیت به TLS تغییر کرد (گواهی را در بخش امنیت انتخاب کنید)." : "REALITY does not work over this transport; security switched to TLS (pick the certificate under Security).";
      }
      if (v !== "tcp" && tpl.settings?.flow) { delete tpl.settings.flow; err.textContent = ie("visionNeeds"); }
    }, draw);
    kids.push(field(ie("network"), netSel));
    const FA = lang === "fa";
    if (net === "tcp" || net === "raw") {
      const o2 = obj(st, net === "raw" ? "rawSettings" : "tcpSettings");
      const hd = o2.header || {};
      kids.push(field(FA ? "استتار HTTP" : "HTTP obfuscation", segPick([["none", FA ? "خاموش" : "Off"], ["http", "HTTP"]], () => hd.type || "none", (v) => {
        if (v === "http") o2.header = { type: "http", request: { path: ["/"], headers: { Host: [] } } }; else { delete o2.header; if (!Object.keys(o2).length) delete st[net === "raw" ? "rawSettings" : "tcpSettings"]; }
      }, draw)));
      if (hd.type === "http") {
        const rq = hd.request || (hd.request = {});
        const hs = rq.headers || (rq.headers = {});
        kids.push(h("div", { class: "two" },
          field(FA ? "مسیرها (هر خط یکی)" : "Paths (one per line)", lines(() => rq.path, (v) => { rq.path = v.length ? v : ["/"]; })),
          field("Host", lines(() => hs.Host, (v) => { hs.Host = v; }, { placeholder: "www.example.com" }))));
      }
    }
    if (net === "kcp") {
      const o2 = obj(st, "kcpSettings");
      const hd = o2.header || (o2.header = { type: "none" });
      kids.push(h("div", { class: "three" }, field("MTU", num(o2, "mtu", { min: "576", max: "1460" })), field("TTI (ms)", num(o2, "tti", { min: "10", max: "100" })),
          field(FA ? "استتار (header)" : "Header", select([["none", "none"], ["srtp", "srtp (FaceTime)"], ["utp", "utp (BitTorrent)"], ["wechat-video", "wechat-video"], ["dtls", "dtls"], ["wireguard", "wireguard"], ["dns", "dns"]], () => hd.type || "none", (v) => { hd.type = v; }))),
        h("div", { class: "three" }, field(FA ? "ظرفیت آپلود (MB/s)" : "Uplink (MB/s)", num(o2, "uplinkCapacity", { min: "0" })), field(FA ? "ظرفیت دانلود (MB/s)" : "Downlink (MB/s)", num(o2, "downlinkCapacity", { min: "0" })),
          field(FA ? "رمز (seed، اختیاری)" : "Seed (optional)", input(() => o2.seed, (v) => { if (v) o2.seed = v; else delete o2.seed; }, { dir: "ltr" }))),
        h("div", { class: "three" }, field(FA ? "بافر خواندن (MB)" : "Read buffer (MB)", num(o2, "readBufferSize", { min: "1" })), field(FA ? "بافر نوشتن (MB)" : "Write buffer (MB)", num(o2, "writeBufferSize", { min: "1" })),
          h("div", {}, opt(o2, "congestion", FA ? "کنترل ازدحام" : "Congestion control"))),
        h("p", { class: "small muted" }, FA ? "mKCP روی UDP است؛ پورت UDP باید باز باشد. در اشتراک‌های sing-box و Clash نمی‌آید (فقط لینک)." : "mKCP runs over UDP; the UDP port must be open. Not in sing-box or Clash profiles (links only)."));
    }
    if (net === "ws" || net === "httpupgrade") {
      const o = obj(st, net === "ws" ? "wsSettings" : "httpupgradeSettings");
      kids.push(h("div", { class: "two" }, field(ie("path"), input(() => o.path, (v) => { o.path = v; }, { dir: "ltr", placeholder: "/" })),
        field(ie("host"), input(() => o.host, (v) => { if (v) o.host = v; else delete o.host; }, { dir: "ltr" }))));
      kids.push(h("div", { class: "two" }, net === "ws" ? field(FA ? "Heartbeat (ثانیه، ۰ = خاموش)" : "Heartbeat (s, 0 = off)", num(o, "heartbeatPeriod", { min: "0", placeholder: "0" })) : h("span"),
        field(FA ? "هدرهای اضافه" : "Extra headers", hdrs(o, "headers"))));
    }
    if (net === "grpc") {
      const o = obj(st, "grpcSettings");
      kids.push(field(ie("service"), input(() => o.serviceName, (v) => { o.serviceName = v; }, { dir: "ltr" })),
        check(() => o.multiMode, (v) => { if (v) o.multiMode = true; else delete o.multiMode; }, ie("multi")),
        field("Authority", input(() => o.authority, (v) => { if (v) o.authority = v; else delete o.authority; }, { dir: "ltr", placeholder: FA ? "خالی = خودکار" : "empty = automatic" })));
    }
    if (net === "xhttp" || net === "splithttp") {
      const o = obj(st, net === "xhttp" ? "xhttpSettings" : "splithttpSettings");
      kids.push(h("div", { class: "two" }, field(ie("path"), input(() => o.path, (v) => { o.path = v; }, { dir: "ltr", placeholder: "/" })),
        field(ie("host"), input(() => o.host, (v) => { if (v) o.host = v; else delete o.host; }, { dir: "ltr" }))),
        h("div", { class: "two" },
          field(ie("xmode"), select([["auto", "auto"], ["packet-up", "packet-up"], ["stream-up", "stream-up"], ["stream-one", "stream-one"]], () => o.mode || "auto", (v) => { o.mode = v; })),
          field(ie("padding"), input(() => o.xPaddingBytes, (v) => { if (v) o.xPaddingBytes = v; else delete o.xPaddingBytes; }, { dir: "ltr", placeholder: "100-1000" }))));
      kids.push(h("details", { class: "adv-box" }, h("summary", {}, FA ? "تنظیمات بیشتر XHTTP" : "More XHTTP settings"),
        h("div", { class: "three" }, field("scMaxBufferedPosts", num(o, "scMaxBufferedPosts", { min: "1", placeholder: "30" })), field("scMaxEachPostBytes", input(() => o.scMaxEachPostBytes, (v) => { if (v) o.scMaxEachPostBytes = v; else delete o.scMaxEachPostBytes; }, { dir: "ltr", placeholder: "1000000" })),
          field("scStreamUpServerSecs", input(() => o.scStreamUpServerSecs, (v) => { if (v) o.scStreamUpServerSecs = v; else delete o.scStreamUpServerSecs; }, { dir: "ltr", placeholder: "20-80" }))),
        opt(o, "noSSEHeader", "noSSEHeader"), field(FA ? "هدرهای اضافه" : "Extra headers", hdrs(o, "headers"))));
    }
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
      kids.push(field(ie("sec"), segPick(opts, () => sec, async (v) => {
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
      // Dark's SNI finder: measures real sites from a node and fills the target.
      const nodesForSni = o.nodes || [];
      const finder = nodesForSni.length ? sniFinder({ nodes: nodesForSni, node: o.nodeId || nodesForSni[0]?.ID, onPick: (d) => { rs[tKey] = `${d}:443`; rs.serverNames = [d]; draw(); } }) : null;
      if (finder) finder.hidden = true;
      kids.push(
        finder ? h("div", { class: "row" }, h("button", { type: "button", class: "ghost", onclick: () => { finder.hidden = !finder.hidden; } }, "🔎 " + t("findSni")),
          h("span", { class: "small muted" }, lang === "fa" ? "بهترین سایت هدف را از روی نود اندازه می‌گیرد و target و serverNames را پر می‌کند." : "Measures the best target site from the node and fills target and serverNames.")) : null,
        finder,
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
        h("div", { class: "two" }, fpSel(), field(ie("spx"), input(() => meta.spider_x, (v) => { if (v) meta.spider_x = v; else delete meta.spider_x; }, { dir: "ltr", placeholder: "/" }))),
        h("details", { class: "adv-box" }, h("summary", {}, lang === "fa" ? "تنظیمات بیشتر REALITY" : "More REALITY settings"),
          h("div", { class: "three" }, field(lang === "fa" ? "حداکثر اختلاف ساعت (ms)" : "Max time difference (ms)", num(rs, "maxTimeDiff", { min: "0", placeholder: "0" })),
            field(lang === "fa" ? "حداقل نسخهٔ کلاینت" : "Min client version", input(() => rs.minClientVer, (v) => { if (v) rs.minClientVer = v; else delete rs.minClientVer; }, { dir: "ltr", placeholder: "25.1.1" })),
            field(lang === "fa" ? "حداکثر نسخهٔ کلاینت" : "Max client version", input(() => rs.maxClientVer, (v) => { if (v) rs.maxClientVer = v; else delete rs.maxClientVer; }, { dir: "ltr" })))));
    }
    if (sec === "tls") {
      const ts = obj(st, "tlsSettings");
      const certs = Array.isArray(ts.certificates) ? ts.certificates : (ts.certificates = []);
      const curCert = certs.find((c) => c.dark_cert)?.dark_cert || "";
      const alpnOpts = proto === "hysteria" ? ["h3"] : ["h3", "h2", "http/1.1"];
      const alpn = new Set(ts.alpn || []);
      // Xray serves the certificate whose domain matches the client's SNI;
      // the first one is the default. QUIC protocols use one certificate.
      const oneCert = proto === "hysteria" || proto === "tuic";
      const picked = certs.map((c) => c.dark_cert).filter(Boolean);
      const others = certs.filter((c) => !c.dark_cert); // inline certificates stay as they are
      const setPicked = (ids) => {
        ts.certificates = [...ids.map((id) => ({ dark_cert: id })), ...others];
        const c = certList.find((x) => x.id === ids[0]); if (c?.domains?.[0] && !ts.serverName) ts.serverName = c.domains[0];
      };
      const fa = lang === "fa";
      // Add a certificate right here: paste or upload the files (stored on
      // the HUB, sent to every node), or point at files already on the node.
      const addCertBox = () => {
        const box = h("details", { class: "adv-box" }, h("summary", {}, fa ? "➕ افزودن گواهی" : "➕ Add a certificate"));
        let how = "paste";
        const pane = h("div", { class: "form" });
        const tabs = h("div", { class: "chips" });
        const err = h("div", { class: "error", role: "alert" });
        const domain = h("input", { dir: "ltr", placeholder: "sub.example.com" });
        const readFile = (ta) => { const f = h("input", { type: "file", accept: ".pem,.crt,.key,.cer,.txt" }); f.addEventListener("change", async () => { if (f.files[0]) ta.value = await f.files[0].text(); }); return f; };
        const drawPane = () => {
          tabs.replaceChildren(...[["paste", fa ? "فایل‌ها (fullchain و privkey)" : "Files (fullchain and privkey)"], ["path", fa ? "فایل روی سرور" : "File on the server"]].map(([k, l]) => {
            const b2 = h("button", { type: "button", class: `chip ${how === k ? "on" : ""}` }, l); b2.onclick = () => { how = k; err.textContent = ""; drawPane(); }; return b2; }));
          if (how === "paste") {
            const cert = h("textarea", { class: "code", rows: "5", spellcheck: "false", dir: "ltr", placeholder: "-----BEGIN CERTIFICATE-----" });
            const key = h("textarea", { class: "code", rows: "4", spellcheck: "false", dir: "ltr", placeholder: "-----BEGIN PRIVATE KEY-----" });
            pane.replaceChildren(
              h("label", {}, h("span", {}, fa ? "دامنه (نام گواهی)" : "Domain (certificate name)"), domain),
              h("label", {}, h("span", {}, "fullchain.pem"), cert, readFile(cert)),
              h("label", {}, h("span", {}, "privkey.pem"), key, readFile(key)),
              h("p", { class: "small muted" }, fa ? "گواهی روی هاب ذخیره می‌شود و به همهٔ نودهای این اینباند می‌رسد." : "Stored on the HUB and sent to every node of this inbound."),
              err,
              h("button", { type: "button", class: "primary", onclick: async () => {
                err.textContent = "";
                const id = (domain.value.trim().toLowerCase().replace(/[^a-z0-9._-]/g, "-").replace(/^[^a-z0-9]+/, "") || "").slice(0, 63);
                if (!id) { err.textContent = fa ? "دامنه را بنویسید." : "Enter the domain."; return; }
                if (!cert.value.includes("BEGIN") || !key.value.includes("BEGIN")) { err.textContent = fa ? "هر دو فایل لازم است." : "Both files are needed."; return; }
                try {
                  await api("PUT", `/api/v2/admin/certs/${encodeURIComponent(id)}`, { Cert: cert.value, Key: key.value });
                  const fresh = await api("GET", "/api/v2/admin/certs").catch(() => null);
                  if (fresh) certList.splice(0, certList.length, ...fresh); else if (!certList.find((x) => x.id === id)) certList.push({ id, domains: [domain.value.trim()] });
                  setPicked([...picked.filter((x) => x !== id), id]);
                  if (!ts.serverName) ts.serverName = domain.value.trim();
                  toast(fa ? "گواهی اضافه و انتخاب شد" : "Certificate added and selected"); draw();
                } catch (x) { err.textContent = x.message; }
              } }, fa ? "ذخیره و استفاده" : "Save and use"));
          } else {
            const cf = h("input", { dir: "ltr", placeholder: "/etc/letsencrypt/live/sub.example.com/fullchain.pem" });
            const kf = h("input", { dir: "ltr", placeholder: "/etc/letsencrypt/live/sub.example.com/privkey.pem" });
            domain.oninput = () => { const d = domain.value.trim(); if (d) { cf.value = `/etc/letsencrypt/live/${d}/fullchain.pem`; kf.value = `/etc/letsencrypt/live/${d}/privkey.pem`; } };
            pane.replaceChildren(
              h("label", {}, h("span", {}, fa ? "دامنه" : "Domain"), domain),
              h("label", {}, h("span", {}, fa ? "مسیر fullchain روی نود" : "fullchain path on the node"), cf),
              h("label", {}, h("span", {}, fa ? "مسیر privkey روی نود" : "privkey path on the node"), kf),
              h("p", { class: "small muted" }, fa ? "Xray فایل‌ها را مستقیم از روی خود نود می‌خواند؛ روی هر نودی که این اینباند را دارد باید همین مسیرها وجود داشته باشند. تمدید گواهی هم با خود سرور است." : "Xray reads the files on the node itself; every node running this inbound needs them at these paths, and renews them itself."),
              err,
              h("button", { type: "button", class: "primary", onclick: () => {
                err.textContent = "";
                if (!cf.value.startsWith("/") || !kf.value.startsWith("/")) { err.textContent = fa ? "مسیر کامل هر دو فایل را بنویسید." : "Enter the full path of both files."; return; }
                ts.certificates = [...(ts.certificates || []), { certificateFile: cf.value.trim(), keyFile: kf.value.trim() }];
                if (!ts.serverName && domain.value.trim()) ts.serverName = domain.value.trim();
                draw();
              } }, fa ? "افزودن" : "Add"));
          }
        };
        drawPane();
        box.append(tabs, pane);
        return box;
      };
      const certField = oneCert
        ? field(ie("cert"), select([["", "—"], ...certList.map((c) => [c.id, `${c.id} · ${(c.domains || []).join(", ")}`])], () => curCert, (v) => setPicked(v ? [v] : []), draw))
        : field(fa ? "گواهی‌ها (چندتایی؛ اولی پیش‌فرض)" : "Certificates (several; the first is the default)",
          h("div", { class: "cert-pick" },
            certList.length ? certList.map((c) => {
              const i = picked.indexOf(c.id);
              const box = h("input", { type: "checkbox", checked: i >= 0 });
              box.addEventListener("change", () => { setPicked(box.checked ? [...picked, c.id] : picked.filter((x) => x !== c.id)); draw(); });
              return h("label", { class: "check" }, box, h("span", { dir: "ltr" }, `${c.id} · ${(c.domains || []).join(", ")}`),
                i === 0 && picked.length > 1 ? h("span", { class: "badge good" }, fa ? "پیش‌فرض" : "default") : null,
                i > 0 ? h("button", { type: "button", class: "ghost small", title: fa ? "پیش‌فرض کن" : "Make default", onclick: () => { setPicked([c.id, ...picked.filter((x) => x !== c.id)]); draw(); } }, fa ? "پیش‌فرض کن" : "make default") : null);
            }) : h("span", { class: "muted small" }, fa ? "هنوز گواهی‌ای نیست (تنظیمات ← گواهی‌ها)." : "No certificates yet (Settings → Certificates)."),
            picked.length > 1 ? h("p", { class: "small muted" }, fa ? "Xray گواهیِ هم‌نامِ SNI کلاینت را می‌دهد؛ اگر هیچ‌کدام نخورد، اولی (پیش‌فرض). لینک‌های اشتراک از فیلد SNI استفاده می‌کنند." : "Xray serves the certificate matching the client's SNI, else the first (default). Subscription links use the SNI field.") : null,
            // Certificates kept as files on the node itself.
            ...others.filter((c) => c.certificateFile).map((c) => h("div", { class: "check" }, h("span", { class: "badge" }, fa ? "فایل روی سرور" : "file on server"),
              h("span", { dir: "ltr", class: "small" }, c.certificateFile),
              h("button", { type: "button", class: "ghost small", onclick: () => { ts.certificates = ts.certificates.filter((x) => x !== c); draw(); } }, fa ? "حذف" : "remove"))),
            oneCert ? null : addCertBox()));
      // SNI per node: each node's links can carry its own domain (and Xray
      // answers it with the matching certificate).
      const sniPerNode = () => {
        const rts = o.routes || [];
        if (rts.length < 1) return null;
        const domains = [...new Set(picked.flatMap((id) => (certList.find((c) => c.id === id)?.domains || [])).filter((d) => d && !d.startsWith("*")))];
        const nodeName = (id) => { const n = (o.nodes || []).find((x) => x.ID === id || x.id === id); return n ? (n.Name || n.name || id) : id; };
        return h("details", { class: "adv-box", open: Object.keys(o.routeSni).length > 0 || rts.some((r) => (r.hosts || []).some((x) => x.sni)) },
          h("summary", {}, fa ? "SNI هر نود" : "SNI per node"),
          h("p", { class: "small muted" }, fa ? "دامنه‌ای که لینک‌های هر نود با آن ساخته می‌شوند. خالی = همان فیلد SNI بالا. با چند گواهی، Xray روی هر نود گواهیِ همان دامنه را می‌دهد." : "The domain each node's links use. Empty = the SNI field above. With several certificates Xray answers each with the matching one."),
          ...rts.map((r) => {
            const cur = r.node_id in o.routeSni ? o.routeSni[r.node_id] : ((r.hosts || []).find((x) => x.sni)?.sni || "");
            const custom = cur && !domains.includes(cur);
            const inp = h("input", { dir: "ltr", value: custom ? cur : "", placeholder: "sub.example.com", hidden: !custom });
            inp.addEventListener("input", () => { o.routeSni[r.node_id] = inp.value.trim(); });
            const sel = h("select", {}, [["", fa ? `— همان SNI بالا (${ts.serverName || "—"})` : `— the SNI above (${ts.serverName || "—"})`], ...domains.map((d) => [d, d]), ["*custom", fa ? "دامنهٔ دیگر…" : "Other domain…"]]
              .map(([v, l]) => h("option", { value: v, selected: custom ? v === "*custom" : v === cur }, l)));
            sel.addEventListener("change", () => {
              if (sel.value === "*custom") { inp.hidden = false; inp.focus(); o.routeSni[r.node_id] = inp.value.trim(); } else { inp.hidden = true; o.routeSni[r.node_id] = sel.value; }
            });
            return h("div", { class: "two" }, field(nodeName(r.node_id), sel), h("label", {}, h("span", {}, "\u00a0"), inp));
          }));
      };
      kids.push(
        h("div", { class: "two" },
          certField,
          field(ie("sniName"), input(() => ts.serverName, (v) => { ts.serverName = v; }, { dir: "ltr" }))),
        sniPerNode(),
        h("div", { class: "row" }, h("span", { class: "muted small" }, ie("alpn")), ...alpnOpts.map((a) => check(() => alpn.has(a), (v) => { if (v) alpn.add(a); else alpn.delete(a); ts.alpn = alpnOpts.filter((x) => alpn.has(x)); if (!ts.alpn.length) delete ts.alpn; }, a))),
        h("div", { class: "three" },
          field(ie("minVer"), select([["", "—"], ["1.2", "1.2"], ["1.3", "1.3"]], () => ts.minVersion || "", (v) => { if (v) ts.minVersion = v; else delete ts.minVersion; })),
          field(lang === "fa" ? "حداکثر نسخهٔ TLS" : "Max TLS version", select([["", "—"], ["1.2", "1.2"], ["1.3", "1.3"]], () => ts.maxVersion || "", (v) => { if (v) ts.maxVersion = v; else delete ts.maxVersion; })),
          fpSel()),
        h("details", { class: "adv-box" }, h("summary", {}, lang === "fa" ? "تنظیمات بیشتر TLS" : "More TLS settings"),
          field("Cipher suites", input(() => ts.cipherSuites, (v) => { if (v) ts.cipherSuites = v; else delete ts.cipherSuites; }, { dir: "ltr", placeholder: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:…" })),
          h("div", { class: "row" }, opt(ts, "rejectUnknownSni", lang === "fa" ? "رد SNI ناشناس" : "Reject unknown SNI"), opt(ts, "disableSystemRoot", lang === "fa" ? "بدون گواهی‌های ریشهٔ سیستم" : "Disable system roots"),
            opt(ts, "enableSessionResumption", lang === "fa" ? "ادامهٔ نشست (session resumption)" : "Session resumption")),
          field(lang === "fa" ? "OCSP stapling (ثانیه، ۰ = خاموش)" : "OCSP stapling (s, 0 = off)", num(ts, "ocspStapling", { min: "0", placeholder: "0" }))),
        check(() => meta.insecure, (v) => { if (v) meta.insecure = true; else delete meta.insecure; }, ie("insecure")));
    }
    if (sec === "none" && proto === "vless" && !tpl.settings?.decryption) kids.push(h("p", { class: "small warn-text" }, lang === "fa"
      ? "توجه: کلاینت‌های Xray 26 (v2rayNG، v2rayN، Happ جدید) VLESS بدون TLS/REALITY را به IP عمومی وصل نمی‌کنند. یا «رمزنگاری VLESS» را در بخش پروتکل روشن کنید، یا TLS/REALITY بگذارید، یا فقط پشت CDN با TLS استفاده کنید."
      : "Note: Xray 26 clients (recent v2rayNG, v2rayN, Happ) refuse VLESS without TLS/REALITY to a public IP. Turn on VLESS encryption in the protocol section, use TLS/REALITY, or use it only behind a TLS CDN."));
    return section(ie("security"), ...kids);
  }

  function drawSockopt() {
    const st = obj(tpl, "streamSettings");
    const so = st.sockopt || {};
    const FA = lang === "fa";
    const commit = () => { if (Object.keys(so).length) st.sockopt = so; else delete st.sockopt; };
    const bool = (key, label) => check(() => so[key], (v) => { if (v) so[key] = true; else delete so[key]; commit(); }, label);
    const n2 = (key, attrs) => input(() => so[key], (v) => { if (v === undefined || Number.isNaN(v) || v === 0) delete so[key]; else so[key] = v; commit(); }, { type: "number", dir: "ltr", ...attrs });
    return h("details", { class: "ie-sec adv-box", open: Object.keys(so).length > 0 }, h("summary", {}, FA ? "تنظیمات سوکت (پیشرفته)" : "Socket options (advanced)"),
      h("div", { class: "row" }, bool("acceptProxyProtocol", ie("proxyProto")), bool("tcpFastOpen", "TCP Fast Open"), bool("tcpMptcp", "MPTCP"), bool("V6Only", FA ? "فقط IPv6" : "IPv6 only")),
      h("div", { class: "three" },
        field(FA ? "الگوریتم ازدحام" : "TCP congestion", select([["", FA ? "پیش‌فرض سیستم" : "System default"], ["bbr", "bbr"], ["cubic", "cubic"], ["reno", "reno"]], () => so.tcpcongestion || "", (v) => { if (v) so.tcpcongestion = v; else delete so.tcpcongestion; commit(); })),
        field("Keep-alive interval (s)", n2("tcpKeepAliveInterval", { min: "0" })), field("Keep-alive idle (s)", n2("tcpKeepAliveIdle", { min: "0" }))),
      h("div", { class: "three" },
        field("TProxy", select([["", FA ? "خاموش" : "Off"], ["redirect", "redirect"], ["tproxy", "tproxy"]], () => so.tproxy || "", (v) => { if (v) so.tproxy = v; else delete so.tproxy; commit(); })),
        field("Mark", n2("mark", { min: "0" })), field("Interface", input(() => so.interface, (v) => { if (v) so.interface = v; else delete so.interface; commit(); }, { dir: "ltr", placeholder: "eth0" }))));
  }

  function drawSniff() {
    const sn = obj(tpl, "sniffing");
    const dest = new Set(sn.destOverride || []);
    return section(ie("sniffing"),
      h("div", { class: "row" }, check(() => sn.enabled, (v) => { sn.enabled = v; }, ie("sniffOn")),
        check(() => sn.routeOnly, (v) => { if (v) sn.routeOnly = true; else delete sn.routeOnly; }, ie("routeOnly")),
        check(() => sn.metadataOnly, (v) => { if (v) sn.metadataOnly = true; else delete sn.metadataOnly; }, ie("metaOnly"))),
      h("div", { class: "row" }, h("span", { class: "muted small" }, ie("dest")), ...["http", "tls", "quic", "fakedns"].map((x) => check(() => dest.has(x), (v) => { if (v) dest.add(x); else dest.delete(x); sn.destOverride = [...dest]; }, x))),
      h("div", { class: "two" },
        field(lang === "fa" ? "دامنه‌های مستثنا" : "Excluded domains", lines(() => sn.domainsExcluded, (v) => { if (v.length) sn.domainsExcluded = v; else delete sn.domainsExcluded; }, { placeholder: "courier.push.apple.com" })),
        field(lang === "fa" ? "IPهای مستثنا" : "Excluded IPs", lines(() => sn.ipsExcluded, (v) => { if (v.length) sn.ipsExcluded = v; else delete sn.ipsExcluded; }, { placeholder: "10.0.0.0/8" }))));
  }

  return {
    drawForm, drawJSON,
    readJSON() { try { tpl = JSON.parse(jsonTpl.value); meta = JSON.parse(jsonMeta.value); return ""; } catch (x) { return `${ie("badJson")}: ${x.message}`; } },
    state: () => ({ tpl, meta, enabled }),
  };
}

async function inboundEditor(tag, nodes, certs) {
  const [raw, routes, certList, all] = await Promise.all([
    api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}`),
    api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`).catch(() => []),
    certs ? Promise.resolve(certs) : api("GET", "/api/v2/admin/certs").catch(() => []),
    api("GET", "/api/v2/admin/inbounds").catch(() => [])]);
  const view = (all || []).find((x) => x.tag === tag) || {};
  const lim = { cap: view.cap_bytes ? +(view.cap_bytes / 2 ** 30).toFixed(2) : 0, ends: view.ends_unix ? new Date(view.ends_unix * 1000).toISOString().slice(0, 10) : "", speed: view.speed_kbps ? +(view.speed_kbps / 1000).toFixed(2) : 0 };
  const speed0 = lim.speed;
  const lim0 = JSON.stringify({ cap: lim.cap, ends: lim.ends });
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

  const routeSni = {}; // nodeID -> SNI chosen in the TLS section, saved into that node's hosts
  const kit = inboundFormKit({ tpl, meta, enabled, lim, view, routed, certList, err, draw: () => draw(), nodes: nodes || [], nodeId: (routes || [])[0]?.node_id || raw.node_id, routes: routes || [], routeSni });
  function draw() {
    tabs.replaceChildren(...[["form", ie("form")], ["json", ie("json")]].map(([k, l]) => h("button", { type: "button", role: "tab", "aria-current": mode === k ? "step" : null, onclick: () => switchTo(k) }, l)));
    body.replaceChildren(...(mode === "form" ? kit.drawForm() : kit.drawJSON()));
  }
  function switchTo(k) {
    if (k === mode) return;
    if (mode === "json") { const e = kit.readJSON(); if (e) { err.textContent = e; return; } }
    err.textContent = ""; mode = k; draw();
  }
  async function save() {
    err.textContent = "";
    if (mode === "json") { const e = kit.readJSON(); if (e) { err.textContent = e; return; } }
    ({ tpl, meta, enabled } = kit.state());
    const st = tpl.streamSettings || {};
    if (st.security === "tls" && tpl.protocol !== "hysteria" && !(st.tlsSettings?.certificates || []).length) { err.textContent = ie("needCert"); return; }
    if (st.security === "reality" && st.realitySettings) meta.short_id = (st.realitySettings.shortIds || [])[0] || meta.short_id;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}`, { node_id: raw.node_id, template: tpl, meta, enabled }));
      if (Object.keys(routeSni).length) {
        // Per-node SNI: read the routes again and change only the SNI of
        // that node's hosts, so nothing else in the route layout moves.
        const fresh = await api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`);
        for (const r of fresh) if (r.node_id in routeSni) for (const x of r.hosts || []) x.sni = routeSni[r.node_id];
        await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`, fresh);
      }
      if (+lim.speed !== speed0) await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/speed`, { kbps: Math.round((+lim.speed || 0) * 1000) });
      if (JSON.stringify({ cap: lim.cap, ends: lim.ends }) !== lim0) {
        await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/limits`, { cap_gb: +lim.cap || 0, ends_unix: lim.ends ? Math.floor(new Date(lim.ends + "T23:59:59").getTime() / 1000) : 0 });
      }
      toast(ie("saved")); d.close("save"); route();
    } catch (x) { err.textContent = x.message; }
  }
  draw();
}

function certForm(id) {
  const idf = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", value: id || "", disabled: !!id, autofocus: !id });
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
  const tag = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", placeholder: "de-reality" });
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
  const fa = lang === "fa";
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", autofocus: true, dir: "ltr", placeholder: "fr-1" });
  const name = h("input", { placeholder: fa ? "فرانسه" : "France" });
  let mode = "ssh";
  const host = h("input", { dir: "ltr", placeholder: "1.2.3.4" }), port = h("input", { type: "number", dir: "ltr", value: "22", min: "1", max: "65535" });
  const user = h("input", { dir: "ltr", value: "root" }), pass = h("input", { type: "password", dir: "ltr", autocomplete: "new-password" });
  const key = h("textarea", { rows: "3", dir: "ltr", spellcheck: "false", placeholder: "-----BEGIN OPENSSH PRIVATE KEY-----" });
  const out = h("div", { class: "form" });
  const log = h("pre", { class: "logview", dir: "ltr", hidden: true });
  const body = h("div", { class: "form" });
  const seg = h("div", { class: "seg", role: "radiogroup" });
  const save = h("button", { class: "primary", value: "save" }, fa ? "نصب" : "Install");
  const drawMode = () => {
    seg.replaceChildren(...[["ssh", fa ? "نصب خودکار با SSH" : "Install over SSH"], ["cmd", fa ? "دستور نصب (دستی)" : "Install command (manual)"]].map(([k, l]) =>
      h("button", { type: "button", class: mode === k ? "on" : "", onclick: () => { mode = k; drawMode(); } }, l)));
    save.textContent = mode === "ssh" ? (fa ? "نصب" : "Install") : t("save");
    fill(body, ...(mode === "ssh" ? [
      h("p", { class: "small muted" }, fa ? "هاب با SSH به سرور جدید وصل می‌شود، نود را نصب و به پنل اضافه می‌کند و مراحل را همین‌جا نشان می‌دهد. رمز یا کلید فقط یک بار استفاده می‌شود و هیچ‌جا ذخیره نمی‌شود." : "The HUB signs in to the new server over SSH, installs the node, adds it to the panel and shows each step here. The password or key is used once and never stored."),
      h("div", { class: "three" }, h("label", {}, h("span", {}, fa ? "IP سرور" : "Server IP"), host), h("label", {}, h("span", {}, fa ? "پورت SSH" : "SSH port"), port), h("label", {}, h("span", {}, fa ? "کاربر" : "User"), user)),
      h("label", {}, h("span", {}, fa ? "رمز" : "Password"), pass),
      h("details", { class: "adv-box" }, h("summary", {}, fa ? "یا کلید خصوصی SSH" : "Or an SSH private key"), key)] :
      [h("p", { class: "small muted" }, fa ? "دستور نصب را می‌گیرید و خودتان روی سرور اجرا می‌کنید." : "You get an install command to run on the server yourself.")]));
  };
  save.addEventListener("click", async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    if (mode === "cmd") {
      try {
        const r = await guarded(() => api("POST", "/api/v2/admin/nodes", { ID: id.value, Name: name.value }));
        out.replaceChildren(h("p", { class: "small" }, t("installCmd")), h("code", {}, r.install), h("button", { type: "button", onclick: () => copy(r.install) }, t("copy")));
        save.disabled = true;
      } catch {}
      return;
    }
    if (!host.value.trim() || (!pass.value && !key.value.trim())) { toast(fa ? "IP سرور و رمز (یا کلید) لازم است." : "Server IP and a password (or key) are needed."); return; }
    save.disabled = true; log.hidden = false; log.textContent = fa ? "در حال اتصال…" : "Connecting…";
    let job;
    try { job = (await api("POST", "/api/v2/admin/ssh-install", { node_id: id.value, name: name.value, host: host.value.trim(), port: +port.value || 22, user: user.value.trim() || "root", password: pass.value, key: key.value })).job; }
    catch (x) { log.textContent = x.message; save.disabled = false; return; }
    pass.value = ""; key.value = "";
    const poll = async () => {
      let j;
      try { j = await api("GET", `/api/v2/admin/ssh-install/${job}`); } catch { setTimeout(poll, 2000); return; }
      log.textContent = j.lines.join("\n"); log.scrollTop = log.scrollHeight;
      if (j.status === "running") { setTimeout(poll, 1500); return; }
      if (j.status === "done") { out.replaceChildren(h("p", { class: "small", style: "color:var(--good)" }, fa ? "✅ نود نصب شد؛ چند ثانیه دیگر در فهرست نودها آنلاین می‌شود." : "✅ Installed; the node comes online in a few seconds."), h("button", { type: "button", onclick: () => { document.getElementById("modal").close(); route(); } }, fa ? "رفتن به نودها" : "Go to nodes")); }
      else { out.replaceChildren(h("p", { class: "error" }, (fa ? "نصب نشد: " : "Not installed: ") + sshErr(j.error || "", fa))); save.disabled = false; }
    };
    poll();
  });
  drawMode();
  openModal(t("addNode"), h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("nodeId")), id), h("label", {}, h("span", {}, t("name")), name)), seg, body, log, out), [save]);
}

// Why an SSH install failed, in words the owner can act on.
function sshErr(e, fa) {
  if (!fa) return e;
  if (/unable to authenticate/i.test(e)) return "سرور نام کاربری یا رمز را قبول نکرد. رمز را دوباره (بدون فاصلهٔ اضافه) وارد کنید؛ اگر سرور فقط کلید می‌پذیرد، کلید خصوصی را بدهید.";
  if (/connection refused/i.test(e)) return "پورت SSH روی سرور بسته است؛ شمارهٔ پورت را بررسی کنید.";
  if (/i\/o timeout|timed out|no route/i.test(e)) return "هاب به سرور نرسید؛ IP، پورت و فایروال سرور را بررسی کنید.";
  if (/exit status|Process exited/i.test(e)) return "ورود انجام شد ولی نصب‌کننده با خطا تمام شد؛ جزئیات در خروجی بالا است.";
  return e;
}

// Owners manage resellers at /api/v2/admin/reps; a reseller manages its own
// sub-sellers at /api/v2/rep/sellers with the same forms.
// idList: numeric Telegram ids, one field each, with ＋ to add another and
// ✕ to take one out. get() returns the valid ids in order.
function idList(values, max = 10) {
  const fa = lang === "fa";
  const rows = h("div", { class: "id-list" });
  const add = h("button", { type: "button", class: "ghost id-add", title: fa ? "افزودن شناسهٔ دیگر" : "Add another id", "aria-label": fa ? "افزودن شناسهٔ دیگر" : "Add another id" }, fa ? "＋ افزودن شناسه" : "＋ Add an id");
  const sync = () => { add.hidden = rows.children.length >= max; };
  const row = (v) => {
    const inp = h("input", { dir: "ltr", inputmode: "numeric", pattern: "[0-9]{4,15}", placeholder: "123456789", value: v ? String(v) : "" });
    const line = h("div", { class: "row tight" }, inp, h("button", { type: "button", class: "ghost", title: fa ? "حذف" : "Remove", "aria-label": fa ? "حذف این شناسه" : "Remove this id",
      onclick: () => { if (rows.children.length > 1) line.remove(); else inp.value = ""; sync(); } }, "✕"));
    rows.append(line); sync();
    return inp;
  };
  add.onclick = () => row("").focus();
  const set = (list) => { rows.replaceChildren(); (list && list.length ? list : [""]).forEach(row); };
  set(values);
  return { el: h("div", { class: "id-list-wrap" }, rows, add), set,
    get: () => [...new Set([...rows.querySelectorAll("input")].map((i) => parseInt(toLatinDigits(i.value.trim()), 10)).filter((x) => x > 0))] };
}
const toLatinDigits = (v) => String(v).replace(/[۰-۹]/g, (d) => "۰۱۲۳۴۵۶۷۸۹".indexOf(d)).replace(/[٠-٩]/g, (d) => "٠١٢٣٤٥٦٧٨٩".indexOf(d));

// randomPassword: 14 characters without look-alikes (no 0/O, 1/l/I).
function randomPassword(n = 14) {
  const abc = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789";
  const buf = crypto.getRandomValues(new Uint32Array(n));
  return [...buf].map((x) => abc[x % abc.length]).join("");
}
// passwordField: a visible password box with a "random" button.
function passwordField() {
  const fa = lang === "fa";
  const inp = h("input", { type: "text", dir: "ltr", autocomplete: "off", autocapitalize: "off", spellcheck: "false", minlength: "10" });
  const el = h("div", { class: "pw-field" }, inp,
    h("button", { type: "button", class: "ghost", onclick: () => { inp.value = randomPassword(); inp.dispatchEvent(new Event("input")); } }, fa ? "🎲 رمز تصادفی" : "🎲 Random"),
    h("button", { type: "button", class: "ghost", onclick: () => inp.value && copy(inp.value) }, lang === "fa" ? "⧉ کپی" : "⧉ Copy"));
  return { el, inp, get: () => inp.value.trim() };
}

const repBase = () => (me && me.role === "rep" ? "/api/v2/rep/sellers" : "/api/v2/admin/reps");

async function viewSellers() {
  const [all, inbounds, sold] = await Promise.all([api("GET", "/api/v2/rep/sellers"), api("GET", "/api/v2/rep/inbounds"), me.resale ? api("GET", "/api/v2/rep/resale/panels").catch(() => null) : null]);
  // Panels sold through "Panel sale" are managed there.
  const soldIDs = new Set(((sold && sold.panels) || []).map((p) => p.id));
  const reps = (all || []).filter((r) => !soldIDs.has(r.ID));
  const cards = (reps || []).map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.Name || r.ID), h("span", { class: "badge" }, r.ID),
      h("span", { class: `badge ${r.Enabled ? "good" : "bad"} spacer` }, t(r.Enabled ? "enabled" : "disabled"))),
    h("div", { class: "muted small" }, `${t("volumeCredit")}: ${bytes(r.VolumeCredit)} · ${t("units")}: ${fmtNum(r.UnlimitedUnits)} · ${t("clientsCount", { n: fmtNum(r.Clients) })} · ${lang === "fa" ? "مصرف کل" : "used"}: ${bytes(r.UsedBytes || 0)}${movedHere(r.UsedBytes, r.UsedHere) ? ` · ${lang === "fa" ? "در پنل ما" : "here"}: ${bytes(r.UsedHere || 0)}` : ""}`),
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

// botLookCard: the look of a bot's main menu (order, names, colours and
// width of its buttons, and the picture above it) with a live preview.
// base is /api/v2/admin/telegram for the main bot, /api/v2/rep/bot for a
// reseller's own.
function botLookCard(base) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const card = h("div", { class: "card form" }, h("h2", {}, "🎨 ", T("ظاهر منوی ربات", "Bot menu look")), h("p", { class: "muted small" }, "…"));
  const COLORS = [["", T("خودکار", "Auto")], ["plain", T("بی‌رنگ", "None")], ["primary", T("آبی", "Blue")], ["success", T("سبز", "Green")], ["danger", T("قرمز", "Red")]];
  // Which buttons a customer sees depends on who they are; the preview shows both.
  const WHO = { fresh: ["trial", "shop", "wallet", "guide", "status", "invite", "coupon", "me", "panel", "link"], has: ["renew", "mine", "shop", "wallet", "guide", "status", "invite", "coupon", "me", "panel", "link"] };
  const draw = (st) => {
    const defs = st.defaults || [];
    const byKey = Object.fromEntries(defs.map((d) => [d.key, d]));
    const saved = (st.look && st.look.buttons) || [];
    let items = saved.length ? saved.filter((b) => byKey[b.key]).map((b) => ({ key: b.key, label: b.label || "", style: b.style || "", hidden: !!b.hidden, wide: !!b.wide })) : [];
    for (const d of defs) if (!items.some((i) => i.key === d.key)) items.push({ key: d.key, label: "", style: "", hidden: false, wide: !!d.wide });
    let banner = (st.look && st.look.banner) || "";
    let hasBanner = !!st.has_banner, who = "fresh", bust = Date.now();
    const list = h("div", { class: "look-list" }), prev = h("div", { class: "tg-prev", dir: "rtl", "aria-label": T("پیش‌نمایش منو", "Menu preview") });
    const err = h("div", { class: "error", role: "alert" });
    const paint = () => {
      const shown = items.filter((i) => !i.hidden && WHO[who].includes(i.key) && (st.main || i.key !== "invite"));
      const rows = []; let pend = [];
      for (const i of shown) { if (i.wide) { if (pend.length) rows.push(pend); pend = []; rows.push([i]); } else { pend.push(i); if (pend.length === 2) { rows.push(pend); pend = []; } } }
      if (pend.length) rows.push(pend);
      const pic = banner === "custom" && hasBanner ? h("img", { class: "tg-banner", alt: "", src: `${base}/banner?v=${bust}` })
        : banner === "" && st.main ? h("div", { class: "tg-banner tg-banner-default" }, "DARK VPN") : null;
      prev.replaceChildren(
        h("div", { class: "tg-msg" }, pic, h("div", { class: "tg-text" }, h("b", {}, T("سلام علی 👋", "Hi Ali 👋")), h("br"),
          who === "fresh" ? T("خوش آمدید. اول رایگان امتحان کنید، بعد تصمیم بگیرید.", "Welcome. Try it free first, then decide.") : T("📦 ۴۰ گیگ · یک ماهه — ۲۸ گیگ مانده", "📦 40 GB · one month — 28 GB left"))),
        ...rows.map((r) => h("div", { class: "tg-row" }, r.map((i) => h("span", { class: `tg-btn ${i.style || "auto"}` }, i.label || byKey[i.key].label)))));
    };
    const move = (n, d) => { const m = n + d; if (m < 0 || m >= items.length) return; [items[n], items[m]] = [items[m], items[n]]; rows(); };
    const rows = () => {
      list.replaceChildren(...items.map((it, n) => {
        const d = byKey[it.key];
        const label = h("input", { value: it.label, placeholder: d.label, maxlength: "40", "aria-label": T("نام دکمه", "Button name"), oninput: (e) => { it.label = e.target.value; paint(); } });
        const color = h("select", { "aria-label": T("رنگ", "Colour"), onchange: (e) => { it.style = e.target.value; paint(); } }, COLORS.map(([v, l]) => h("option", { value: v, selected: v === it.style }, l)));
        const wide = h("input", { type: "checkbox", checked: it.wide, onchange: (e) => { it.wide = e.target.checked; paint(); } });
        const show = h("input", { type: "checkbox", checked: !it.hidden, onchange: (e) => { it.hidden = !e.target.checked; paint(); } });
        return h("div", { class: `look-row ${it.hidden ? "off" : ""}` },
          h("div", { class: "look-move" },
            h("button", { type: "button", class: "ghost", disabled: n === 0, title: T("بالا", "Up"), "aria-label": T("یک ردیف بالاتر", "Move up"), onclick: () => move(n, -1) }, "▲"),
            h("button", { type: "button", class: "ghost", disabled: n === items.length - 1, title: T("پایین", "Down"), "aria-label": T("یک ردیف پایین‌تر", "Move down"), onclick: () => move(n, 1) }, "▼")),
          h("div", { class: "look-main" }, label,
            h("div", { class: "look-opts" }, color, h("label", { class: "check" }, wide, T("ردیف کامل", "Full row")), h("label", { class: "check" }, show, T("نمایش", "Show"))),
            d.note ? h("span", { class: "small muted" }, d.note) : null));
      }));
      paint();
    };
    const bannerSel = h("select", { onchange: (e) => { banner = e.target.value; paint(); } },
      [["", st.main ? T("بنر پیش‌فرض", "Built-in banner") : T("پیش‌فرض (بدون عکس)", "Default (no picture)")], ["custom", T("عکس خودم", "My own picture")], ["none", T("بدون عکس", "No picture")]]
        .map(([v, l]) => h("option", { value: v, selected: v === banner }, l)));
    const file = h("input", { type: "file", accept: "image/jpeg,image/png", onchange: async () => {
      const f = file.files[0]; err.textContent = "";
      if (!f) return;
      if (f.size > 2 * 1024 * 1024) { err.textContent = T("عکس باید کمتر از ۲ مگابایت باشد.", "The picture must be under 2 MB."); file.value = ""; return; }
      try {
        const res = await fetch(`${base}/banner`, { method: "PUT", credentials: "same-origin", body: f, headers: { "X-Dark-Request": "1", "Content-Type": "application/octet-stream" } });
        if (!res.ok) throw new Error(faErr((await res.text()).trim() || res.statusText));
        hasBanner = true; banner = "custom"; bannerSel.value = "custom"; bust = Date.now(); paint();
        toast(T("عکس بارگذاری شد؛ برای اعمال، ذخیره را بزنید.", "Picture uploaded; press Save to apply it."));
      } catch (x) { err.textContent = x.message; }
    } });
    const whoSeg = h("div", { class: "seg" }, [["fresh", T("مشتری جدید", "New customer")], ["has", T("مشتری دارای سرویس", "Customer with a service")]].map(([k, l]) =>
      h("button", { type: "button", class: k === who ? "on" : "", "aria-pressed": String(k === who), onclick: (e) => {
        who = k; [...whoSeg.children].forEach((b) => { b.classList.toggle("on", b === e.currentTarget); b.setAttribute("aria-pressed", String(b === e.currentTarget)); }); paint();
      } }, l)));
    const save = h("button", { class: "primary", type: "button", onclick: async () => {
      err.textContent = "";
      if (banner === "custom" && !hasBanner) { err.textContent = T("اول یک عکس انتخاب کنید.", "Choose a picture first."); return; }
      if (!items.some((i) => !i.hidden)) { err.textContent = T("دست‌کم یک دکمه باید نمایش داده شود.", "At least one button must stay visible."); return; }
      try { draw(await api("PUT", `${base}/look`, { buttons: items.map((i) => ({ key: i.key, label: i.label.trim(), style: i.style, hidden: i.hidden, wide: i.wide })), banner })); toast(t("saved")); }
      catch (x) { err.textContent = x.message; }
    } }, t("save"));
    const reset = h("button", { class: "ghost", type: "button", onclick: async () => {
      if (!(await confirmAction(T("چینش، نام و رنگ دکمه‌ها و بنر به حالت پیش‌فرض برگردد؟", "Put the buttons and the banner back to the built-in look?"), T("برگشت به پیش‌فرض", "Reset")))) return;
      try { draw(await api("PUT", `${base}/look`, { buttons: [], banner: "" })); toast(t("saved")); } catch (x) { err.textContent = x.message; }
    } }, T("برگشت به پیش‌فرض", "Reset to built-in"));
    fill(card, h("h2", {}, "🎨 ", T("ظاهر منوی ربات", "Bot menu look")),
      h("p", { class: "small muted" }, T("ترتیب، نام، رنگ و پهنای دکمه‌های منوی اصلی مشتری و عکس بالای آن. تلگرام فقط همین سه رنگ را می‌دهد. منوی مدیریت و مراحل خرید تغییر نمی‌کنند.",
        "Order, names, colours and width of the customer's main-menu buttons, and the picture above it. Telegram offers only these three colours. The management menu and the purchase steps stay as they are.")),
      h("div", { class: "look-grid" },
        h("div", {}, h("h3", { class: "section" }, T("دکمه‌ها", "Buttons")), list,
          h("h3", { class: "section" }, T("عکس بالای منو", "Picture above the menu")),
          h("label", {}, h("span", {}, T("بنر", "Banner")), bannerSel),
          h("label", {}, h("span", {}, T("عکس خودم (JPEG یا PNG، کمتر از ۲ مگابایت، بهتر است افقی باشد)", "My picture (JPEG or PNG, under 2 MB, landscape works best)")), file)),
        h("div", { class: "look-side" }, h("h3", { class: "section" }, T("پیش‌نمایش", "Preview")), whoSeg, prev,
          h("p", { class: "small muted" }, T("رنگ «خودکار» را ربات از روی معنی دکمه می‌گذارد و در پیش‌نمایش خاکستری دیده می‌شود.", "“Auto” is coloured by the bot from the button's meaning; the preview shows it grey.")))),
      err, h("div", { class: "row" }, save, reset));
    rows();
  };
  api("GET", `${base}/look`).then(draw).catch((x) => fill(card, h("h2", {}, "🎨 ", T("ظاهر منوی ربات", "Bot menu look")), h("p", { class: "error" }, x.message)));
  return card;
}

// repLoginMessage: what the owner sends the new reseller.
function repLoginMessage(url, user, pass, name) {
  return lang === "fa"
    ? `پنل نمایندگی ${name || ""} آماده است ✅\n\n🌐 آدرس پنل:\n${url}\n\n👤 نام کاربری: ${user}\n🔑 رمز: ${pass}\n\nبعد از اولین ورود، رمز را از بخش تنظیمات عوض کنید.`
    : `Your reseller panel ${name || ""} is ready ✅\n\n🌐 Panel:\n${url}\n\n👤 Username: ${user}\n🔑 Password: ${pass}\n\nChange the password in Settings after your first sign-in.`;
}
async function panelUrl() {
  let path = "";
  try { path = (await api("GET", "/api/v2/admin/panel-path")).path || ""; } catch {}
  return path ? `${location.origin}/${path}/` : `${location.origin}/`;
}
function showRepLogin(title, user, pass, name) {
  const fa = lang === "fa";
  const box = h("textarea", { rows: "9", readonly: true, dir: "auto" });
  panelUrl().then((u) => { box.value = repLoginMessage(u, user, pass, name); });
  openModal(title, h("div", { class: "form" },
    h("p", { class: "small muted" }, fa ? "این پیام را کپی کنید و برای نماینده بفرستید. رمز بعد از بستن این پنجره دیگر نمایش داده نمی‌شود." : "Copy this message and send it to the reseller. The password is not shown again after you close this."),
    box, h("div", { class: "row" }, h("button", { class: "primary", type: "button", onclick: () => copy(box.value) }, fa ? "کپی پیام" : "Copy message"))));
}

function repForm(r) { return r ? repEditForm(r) : repCreateForm(); }

// repCreateForm: everything a new reseller needs on one page, saved in one go.
async function repCreateForm() {
  const fa = lang === "fa";
  // A reseller opening a sub-seller hands out part of its own credit and
  // never more rights than it has itself.
  const sub = me.role === "rep";
  const [inbounds, sale, mine] = await Promise.all([api("GET", sub ? "/api/v2/rep/inbounds" : "/api/v2/admin/inbounds").catch(() => []),
    sub ? null : api("GET", "/api/v2/admin/rep-sale").catch(() => ({ packages: [] })), sub ? api("GET", "/api/v2/rep/sellers-limits").catch(() => null) : null]);
  const pkgs = (sale && sale.packages) || [];
  const lim = (mine && mine.Policy) || null;
  const num = (v, step = "1") => h("input", { type: "number", min: "0", step, value: v || 0, dir: "ltr" });
  const box = (v) => h("input", { type: "checkbox", checked: !!v });
  const id = h("input", { required: true, dir: "ltr", autocapitalize: "off", pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", placeholder: "arda" });
  const name = h("input", {});
  const user = h("input", { dir: "ltr", autocapitalize: "off", autocomplete: "off", pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", placeholder: fa ? "خالی = همان شناسه" : "empty = the id" });
  const pass = passwordField(); pass.inp.value = randomPassword();
  const f = { gb: num(0), units: num(0), mc: num(lim ? lim.MaxClients : 0), md: num(lim ? lim.MaxDays : 0), mg: num(lim ? lim.MaxClientGB : 0),
    au: box(lim ? lim.AllowUnlimited : true), cd: box(lim ? lim.CanDelete : true), ru: box(lim ? lim.RefundUnused : false), mp: box(true) };
  if (lim) { // what the reseller may not do, its sub-seller cannot either
    for (const [el, ok] of [[f.au, lim.AllowUnlimited], [f.cd, lim.CanDelete], [f.ru, lim.RefundUnused]]) if (!ok) el.disabled = true;
    for (const [el, max] of [[f.mc, lim.MaxClients], [f.md, lim.MaxDays], [f.mg, lim.MaxClientGB]]) if (max > 0) el.max = String(max);
  }
  const haveGB = mine ? Math.floor((mine.VolumeCredit || 0) / 2 ** 30) : 0;
  const tags = [...new Set((inbounds || []).map((ib) => ib.tag))];
  const ibBoxes = tags.map((tag) => h("label", { class: "check" }, h("input", { type: "checkbox", value: tag }), tag));
  const pkgSel = h("select", {}, h("option", { value: "" }, fa ? "تنظیم دستی" : "Set by hand"),
    ...pkgs.map((p) => h("option", { value: p.id }, `${p.name || p.id}${p.gb ? ` · ${fmtNum(p.gb)} GB` : ""}`)));
  pkgSel.onchange = () => {
    const p = pkgs.find((x) => x.id === pkgSel.value);
    if (!p) return;
    f.gb.value = p.gb || 0; f.units.value = p.units || 0; f.mc.value = p.max_clients || 0; f.md.value = p.max_days || 0; f.mg.value = p.max_client_gb || 0;
    f.au.checked = !!p.allow_unlimited; f.cd.checked = !!p.can_delete; f.ru.checked = !!p.refund_unused;
    const allow = new Set(p.inbounds || []);
    ibBoxes.forEach((b) => { const i = b.querySelector("input"); i.checked = allow.has(i.value); });
  };
  const err = h("div", { class: "error", role: "alert" });
  const lab = (text, el, hint) => h("label", {}, h("span", {}, text), el, hint ? h("span", { class: "small muted" }, hint) : null);
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    const rid = id.value.trim().toLowerCase(), u = (user.value.trim() || rid).toLowerCase(), pw = pass.get();
    const re = /^[a-z0-9][a-z0-9._-]{0,62}$/;
    const bad = !re.test(rid) ? (fa ? "شناسه فقط حروف کوچک انگلیسی، عدد، نقطه، خط تیره و زیرخط می‌پذیرد." : "The id takes lowercase letters, digits, dot, dash and underscore only.")
      : !re.test(u) ? (fa ? "نام کاربری فقط حروف کوچک انگلیسی، عدد، نقطه، خط تیره و زیرخط می‌پذیرد." : "The sign-in name takes lowercase letters, digits, dot, dash and underscore only.")
      : pw.length < 10 ? (fa ? "رمز باید حداقل ۱۰ نویسه باشد. دکمهٔ «رمز تصادفی» یک رمز مناسب می‌سازد." : "The password needs at least 10 characters. “Random” makes a good one.")
      : sub && (+f.gb.value || 0) > haveGB ? (fa ? `اعتبار شما ${fmtNum(haveGB)} گیگ است؛ بیشتر از آن نمی‌شود منتقل کرد.` : `You have ${haveGB} GB of credit; you cannot hand out more.`)
      : sub && (+f.units.value || 0) > (mine ? mine.UnlimitedUnits || 0 : 0) ? (fa ? `شما ${fmtNum(mine ? mine.UnlimitedUnits || 0 : 0)} واحد نامحدود دارید.` : `You have ${mine ? mine.UnlimitedUnits || 0 : 0} unlimited units.`) : "";
    if (bad) { err.textContent = bad; return; }
    save.disabled = true;
    try {
      await api("POST", sub ? "/api/v2/rep/sellers" : "/api/v2/admin/reps", { ID: rid, Name: name.value.trim(), Username: u, Password: pw,
        Policy: { MaxClients: +f.mc.value || 0, MaxDays: +f.md.value || 0, MaxClientGB: +f.mg.value || 0, AllowUnlimited: f.au.checked, CanDelete: f.cd.checked, RefundUnused: f.ru.checked,
          Inbounds: ibBoxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value) },
        ManageProducts: sub ? null : f.mp.checked, CreditGB: +f.gb.value || 0, Units: +f.units.value || 0 });
      document.getElementById("modal").close("save"); route();
      showRepLogin(fa ? `${sub ? "زیرفروشنده" : "نماینده"} ساخته شد · ${rid}` : `${sub ? "Sub-seller" : "Reseller"} created · ${rid}`, u, pw, name.value.trim() || rid);
    } catch (x) {
      err.textContent = x.message || "";
    } finally { save.disabled = false; }
  } }, fa ? (sub ? "ساخت زیرفروشنده" : "ساخت نماینده") : (sub ? "Create sub-seller" : "Create reseller"));
  const d = openModal(fa ? (sub ? "زیرفروشندهٔ جدید" : "نمایندهٔ جدید") : (sub ? "New sub-seller" : "New reseller"), h("div", { class: "form" },
    h("h3", { class: "section" }, fa ? "مشخصات و ورود" : "Identity and sign-in"),
    h("div", { class: "two" }, lab(t("repId"), id), lab(t("name"), name)),
    lab(t("loginUser"), user),
    lab(t("loginPass"), pass.el, fa ? "حداقل ۱۰ نویسه. بعد از ساخت، در یک پیام آماده نشان داده می‌شود." : "At least 10 characters. Shown in a ready message after saving."),
    h("h3", { class: "section" }, fa ? "اعتبار و محدودیت‌ها" : "Credit and limits"),
    pkgs.length ? lab(fa ? "بسته" : "Package", pkgSel, fa ? "با انتخاب بسته، موارد پایین پر می‌شوند و می‌توانید تغییرشان دهید." : "Choosing a package fills the fields below; you can still change them.") : null,
    sub ? h("p", { class: "small muted" }, fa ? `اعتبار اولیه از اعتبار خود شما کم می‌شود. اعتبار شما: ${fmtNum(haveGB)} گیگ و ${fmtNum(mine ? mine.UnlimitedUnits || 0 : 0)} واحد نامحدود.`
      : `The first credit comes out of your own. You have ${haveGB} GB and ${mine ? mine.UnlimitedUnits || 0 : 0} unlimited units.`) : null,
    h("div", { class: "two" }, lab(sub ? (fa ? "انتقال اعتبار اولیه (GB)" : "First credit to hand over (GB)") : `${t("volumeCredit")} (GB)`, f.gb), lab(t("units"), f.units)),
    h("div", { class: "two" }, lab(t("maxClients"), f.mc), lab(t("maxDays"), f.md)),
    lab(t("maxClientGB"), f.mg, fa ? "صفر یعنی بدون سقف." : "Zero means no limit."),
    h("label", { class: "check" }, f.au, t("allowUnlimited")),
    h("label", { class: "check" }, f.cd, t("canDelete")),
    h("label", { class: "check" }, f.ru, t("refundUnused")),
    sub ? null : h("label", { class: "check" }, f.mp, t("manageProducts")),
    sub && lim ? h("p", { class: "small muted" }, fa ? "گزینه‌های خاموش را خود شما هم ندارید؛ زیرفروشنده بیشتر از شما اجازه نمی‌گیرد." : "Greyed-out options are ones you do not have yourself; a sub-seller never gets more than you.") : null,
    tags.length ? h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("allowedInbounds")), ibBoxes) : null,
    err), [save]);
  d.classList.add("wide");
}

// repEditForm: name and on/off, a new password, and the Telegram ids.
function repEditForm(r) {
  const fa = lang === "fa";
  const name = h("input", { value: r.Name || "" });
  const en = h("input", { type: "checkbox", checked: r.Enabled });
  const user = h("input", { dir: "ltr", autocapitalize: "off", autocomplete: "off", pattern: "[a-z0-9][a-z0-9._\\-]{0,62}" });
  const pass = passwordField();
  const err = h("div", { class: "error", role: "alert" });
  const base = `${repBase()}/${encodeURIComponent(r.ID)}`;
  api("GET", `${base}/login`).then((x) => { user.value = (x.usernames || [])[0] || r.ID; }).catch(() => { user.value = r.ID; });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    const pw = pass.get(), u = (user.value.trim() || r.ID).toLowerCase();
    if (pw && pw.length < 10) { err.textContent = fa ? "رمز باید حداقل ۱۰ نویسه باشد." : "The password needs at least 10 characters."; return; }
    try {
      await api("PUT", base, { Name: name.value, Enabled: en.checked, Username: "", Password: "" });
      if (pw) await api("PUT", `${base}/login`, { Username: u, Password: pw });
      document.getElementById("modal").close("save"); route();
      if (pw) showRepLogin(fa ? `رمز جدید · ${r.ID}` : `New password · ${r.ID}`, u, pw, name.value || r.ID); else toast(t("saved"));
    } catch (x) {
      err.textContent = x.message || "";
    }
  } }, t("save"));
  openModal(`${t("edit")} · ${r.ID}`, h("div", { class: "form" },
    h("label", {}, h("span", {}, t("name")), name),
    h("label", { class: "check" }, en, t("enabled")),
    h("h3", { class: "section" }, fa ? "تغییر رمز ورود" : "Change the password"),
    h("label", {}, h("span", {}, t("loginUser")), user),
    h("label", {}, h("span", {}, fa ? "رمز جدید" : "New password"), pass.el,
      h("span", { class: "small muted" }, fa ? "خالی بگذارید تا رمز فعلی بماند. با رمز جدید، نماینده از همهٔ دستگاه‌ها خارج می‌شود." : "Leave empty to keep the current one. A new password signs the reseller out everywhere.")),
    err), [save]);
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
  // Selling panels in its own bot (owner only, top-level resellers only).
  const fa = lang === "fa";
  const rsOn = me.role === "owner" && !r.ParentID;
  const rs = { al: box(false), cr: box(false), pt: h("input", { type: "number", min: "0", step: "1000", dir: "ltr", value: 0 }), hint: h("span", { class: "small muted" }), loaded: null };
  const rsBox = rsOn ? h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" },
    h("legend", { class: "small muted" }, "🏪 ", fa ? "فروش پنل نمایندگی در ربات خودش" : "Selling reseller panels in its own bot"),
    h("label", { class: "check" }, rs.al, fa ? "اجازهٔ فروش پنل" : "May sell panels"),
    h("label", { class: "check" }, rs.cr, fa ? "اگر کیف پول کافی نبود، فاکتور ۲۴ ساعته (حداکثر یک فاکتور باز)" : "If the wallet is short, a 24-hour bill (one open at most)"),
    h("label", {}, h("span", {}, fa ? "قیمت عمدهٔ اختصاصی هر ترابایت (۰ = قیمت کلی)" : "Own wholesale price per TB (0 = the general price)"), rs.pt, rs.hint)) : null;
  const lg = { al: box(false), loaded: null };
  const lgBox = rsOn ? h("label", { class: "check" }, lg.al, fa ? "📥 اجازهٔ آوردن کاربران قدیمی از پنل دیگر (فقط مصرف واقعی از اعتبار کم می‌شود؛ قابل تمدید نیستند)" : "📥 May bring old users from another panel (only real use is charged; they cannot be renewed)") : null;
  if (rsOn) api("GET", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/legacy`).then((v) => { lg.loaded = v; lg.al.checked = !!v.allowed; }).catch(() => {});
  if (rsOn) api("GET", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/resale`).then((v) => {
    rs.loaded = v.resale; rs.al.checked = !!v.resale.allowed; rs.cr.checked = !!v.resale.credit; rs.pt.value = v.resale.price_tb || 0;
    rs.hint.textContent = fa ? `قیمت کلی: ${fmtNum(v.resale.price_tb ? 0 : v.wholesale_tb)} تومان` : `General price: ${fmtNum(v.wholesale_tb)} toman`;
    if (v.resale.price_tb) rs.hint.textContent = "";
  }).catch(() => {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      if (rs.loaded && (rs.al.checked !== !!rs.loaded.allowed || rs.cr.checked !== !!rs.loaded.credit || (+rs.pt.value || 0) !== (rs.loaded.price_tb || 0)))
        await api("PUT", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/resale`, { allowed: rs.al.checked, credit: rs.cr.checked, price_tb: +rs.pt.value || 0 });
      if (lg.loaded && lg.al.checked !== !!lg.loaded.allowed) await api("PUT", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/legacy`, { allowed: lg.al.checked });
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
    me.role === "rep" ? null : h("div", { class: "two" }, lab("priceGB", f.pg), lab("priceUnit", f.pu)), rsBox, lgBox, err), [save]);
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
    owner ? api("GET", "/api/v2/admin/reps") : me.can_sell && me.manage_products ? api("GET", "/api/v2/rep/sellers").catch(() => []) : Promise.resolve([])]);
  const canEdit = owner || me.manage_products;
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const planText = (p) => `${p.plan === "unlimited" ? t("planUnlimited") : `${fmtNum(p.gb)} GB`} · ${fmtNum(p.days)}d${p.max_ips ? ` · IP ≤ ${fmtNum(p.max_ips)}` : ""}`;
  const stCls = { published: "good", draft: "", archived: "bad", pending: "warn", fulfilled: "good", cancelled: "bad" };
  const fa = lang === "fa";
  const done = orders.filter((o) => o.status === "fulfilled");
  const pending = orders.filter((o) => o.status === "pending");
  const since = (d) => Date.now() / 1000 - d * 86400;
  const sum = (l) => l.reduce((a, o) => a + (o.amount || 0), 0);
  const sold = {};
  done.forEach((o) => { sold[o.product_id] = (sold[o.product_id] || 0) + 1; });
  const tile = (v, l, cls = "") => h("div", { class: `card shop-kpi ${cls}` }, h("b", {}, v), h("span", {}, l));
  const fulfill = async (o) => { const r = await guarded(() => api("POST", `/api/v2/shop/orders/${o.id}/fulfill`)); toast(t("delivered", { x: r.client_email })); route(); };
  const cancel = async (o) => { if (await confirmAction(fa ? `سفارش #${fmtNum(o.id)} لغو شود؟` : `Cancel order #${o.id}?`)) { await guarded(() => api("POST", `/api/v2/shop/orders/${o.id}/cancel`)); route(); } };

  const productTile = (p) => h("article", { class: `card price-tile ${p.status}` },
    h("div", { class: "row" }, h("span", { class: `badge ${stCls[p.status]}` }, t(`status_${p.status}`)),
      owner && p.seller ? h("span", { class: "badge" }, `${t("seller")}: ${p.seller}`) : null,
      h("span", { class: "muted small spacer", dir: "ltr" }, p.id)),
    h("h3", {}, p.name),
    h("div", { class: "price" }, money(p.price), p.price_stars ? h("small", { class: "muted" }, ` · ⭐ ${fmtNum(p.price_stars)}`) : null),
    h("div", { class: "specs" },
      h("span", {}, p.plan === "unlimited" ? t("planUnlimited") : `${fmtNum(p.gb)} GB`),
      h("span", {}, fa ? `${fmtNum(p.days)} روز` : `${p.days} days`),
      p.max_ips ? h("span", {}, fa ? `${fmtNum(p.max_ips)} دستگاه` : `${p.max_ips} devices`) : null,
      p.first_use ? h("span", {}, fa ? "شروع از اولین اتصال" : "Starts on first use") : null),
    h("div", { class: "row tile-foot" },
      h("span", { class: "small muted" }, fa ? `${fmtNum(sold[p.id] || 0)} فروش` : `${sold[p.id] || 0} sold`),
      h("span", { class: "spacer" }),
      canEdit && p.status !== "archived" ? h("button", { class: "ghost", onclick: () => productForm(p, inbounds, reps) }, t("edit")) : null,
      p.status === "published" ? h("button", { onclick: () => orderForm(products, p.id) }, t("newOrder")) : null));

  const orderRow = (o) => h("div", { class: `order-row ${o.status}` },
    h("div", { class: "o-id" }, h("b", {}, `#${fmtNum(o.id)}`), h("span", { class: "muted small" }, fmtTime(o.created_unix))),
    h("div", { class: "o-main" }, h("b", {}, o.product_name || o.product_id),
      h("span", { class: "muted small" }, [o.buyer, o.client_email ? `${o.renew ? "↻ " : "→ "}${o.client_email}` : t("newClient"), owner && o.seller ? o.seller : "", o.note].filter(Boolean).join(" · "))),
    h("div", { class: "o-amt" }, h("b", {}, money(o.amount)), o.discount ? h("span", { class: "small muted" }, `🎟 ${o.discount_code} −${money(o.discount)}`) : null),
    h("div", { class: "o-st" }, h("span", { class: `badge ${stCls[o.status]}` }, t(`status_${o.status}`)), o.has_receipt ? h("span", { class: "badge good" }, fa ? "🧾 رسید" : "🧾 receipt") : null),
    o.status === "pending" ? h("div", { class: "o-act" }, h("button", { class: "primary", onclick: () => fulfill(o) }, t("fulfill")), iconBtn("trash", t("cancelOrder"), () => cancel(o))) : h("div", { class: "o-act" }));

  // Orders with status filter.
  const ordersPane = () => {
    let f = pending.length ? "pending" : "all";
    const list = h("div", { class: "card order-list" });
    const chips = h("div", { class: "chips", role: "tablist" });
    const draw = () => {
      const counts = { all: orders.length, pending: pending.length, fulfilled: done.length, cancelled: orders.filter((o) => o.status === "cancelled").length };
      chips.replaceChildren(...Object.keys(counts).map((k) => h("button", { type: "button", role: "tab", class: `chip ${f === k ? "on" : ""}`, "aria-selected": String(f === k), onclick: () => { f = k; draw(); } },
        k === "all" ? (fa ? "همه" : "All") : t(`status_${k}`), h("span", { class: "count" }, fmtNum(counts[k])))));
      const l = orders.filter((o) => f === "all" || o.status === f);
      list.replaceChildren(...(l.length ? l.map(orderRow) : [h("p", { class: "muted small", style: "padding:14px" }, f === "pending" ? (fa ? "سفارش منتظری ندارید." : "No orders waiting.") : t("empty"))]));
    };
    draw();
    return h("div", {}, chips, list);
  };
  const productsPane = () => productsView(products, inbounds, reps, sold, canEdit);
  const panes = [["orders", t("orders"), ordersPane, pending.length], ["products", t("products"), productsPane]];
  if (owner) panes.push(["pay", fa ? "پرداخت و تخفیف" : "Payments & codes", () => h("div", { class: "dash" }, payCardCard(), discountsCard(products))]);
  panes.push(["wallets", fa ? "کیف پول و شارژ" : "Wallets & top-ups", () => walletsPane()]);
  let cur = sessionStorage.getItem("shopTab") || (pending.length ? "orders" : "products");
  if (!panes.some((x) => x[0] === cur)) cur = panes[0][0];
  const pane = h("div", {});
  const tabs = h("div", { class: "steps", role: "tablist" });
  const show = () => {
    try { sessionStorage.setItem("shopTab", cur); } catch {}
    tabs.replaceChildren(...panes.map(([k, l, , n]) => h("button", { type: "button", role: "tab", "aria-current": k === cur ? "step" : null, onclick: () => { cur = k; show(); } }, l, n ? h("span", { class: "count-dot" }, fmtNum(n)) : null)));
    fill(pane, panes.find((x) => x[0] === cur)[2]());
  };
  show();
  return h("div", {},
    h("div", { class: "row sticky-head" }, h("h1", {}, t("shop")),
      h("span", { class: "spacer" }), canEdit ? h("button", { class: "ghost", onclick: () => productForm(null, inbounds, reps, { seller: shopSeller }) }, fa ? "محصول جدید" : "New product") : null,
      products.some((p) => p.status === "published") ? h("button", { class: "primary", onclick: () => orderForm(products) }, t("newOrder")) : null),
    h("div", { class: "shop-kpis" },
      tile(fmtNum(pending.length), fa ? "منتظر تأیید" : "Waiting", pending.length ? "warn" : ""),
      tile(money(sum(done.filter((o) => o.created_unix >= since(1)))), fa ? "فروش امروز" : "Sales today"),
      tile(money(sum(done.filter((o) => o.created_unix >= since(30)))), fa ? "فروش ۳۰ روز" : "Sales, 30 days"),
      tile(fmtNum(products.filter((p) => p.status === "published").length), fa ? "محصول فعال" : "Products on sale")),
    tabs, pane);
}

// ---------- Business insights ----------
async function viewAnalytics() {
  const fa = lang === "fa";
  const owner = me.role === "owner";
  let seller = sessionStorage.getItem("anSeller") || "";
  const reps = owner ? await api("GET", "/api/v2/admin/reps").catch(() => []) : [];
  const body = h("div", {});
  const pct = (x) => `${fmtNum(Math.round(x * 100))}٪`;
  const gbs = (b) => b < 0 ? (fa ? "نامحدود" : "unlimited") : `${(b / 2 ** 30).toFixed(1)} ${fa ? "گیگ" : "GB"}`;
  const monthName = (m) => new Intl.DateTimeFormat(fa ? "fa-IR-u-ca-persian" : "en-GB", { month: "short" }).format(new Date(m + "-15T12:00:00Z"));
  const delta = (cur, prev) => {
    if (!prev) return cur ? h("span", { class: "chg up" }, fa ? "جدید" : "new") : null;
    const p = Math.round(((cur - prev) / prev) * 100);
    return h("span", { class: `chg ${p > 0 ? "up" : p < 0 ? "down" : ""}`, dir: "ltr" }, `${p > 0 ? "+" : ""}${fmtNum(p)}%`);
  };
  const bars = (rows, keys, colors, names, label) => chartKit({ xs: rows.map((r) => r.month), mode: "stack", bin: false, height: 230, label,
    series: keys.map((k, j) => ({ id: k, name: names[j], color: colors[j], values: rows.map((r) => r[k] || 0) })),
    xLabel: (i) => monthName(rows[i].month), tipTitle: (i) => monthName(rows[i].month), fmt: (v) => `${money(v)} ${fa ? "تومان" : "toman"}`,
    axis: (v) => fmtNum(v / 1e6), totals: (v) => fmtNum(Math.round(v / 1e5) / 10) });
  const line = (daily) => chartKit({ xs: daily.map((d) => d.day), mode: "lines", bin: false, int: true, height: 190, label: fa ? "مشتری فعال روزانه" : "Active customers per day",
    series: [{ id: "a", name: fa ? "مشتری فعال" : "Active customers", color: "var(--ion)", area: true, values: daily.map((d) => d.active) }],
    fmt: (v) => fmtNum(v), partial: daily.at(-1)?.day === todayUTC() ? daily.length - 1 : -1 });
  const load = async () => {
    fill(body, h("p", { class: "muted small" }, fa ? "در حال محاسبه…" : "Working…"));
    let a;
    try { a = await api("GET", `/api/v2/analytics${owner && seller ? `?seller=${encodeURIComponent(seller)}` : ""}`); } catch (e) { fill(body, h("p", { class: "error" }, e.message)); return; }
    const kpi = (v, l, extra, sub) => h("div", { class: "card stat" }, h("div", { class: "v" }, v, " ", extra || null), h("div", { class: "k" }, l), sub ? h("div", { class: "small muted" }, sub) : null);
    const showResell = owner && a.months.some((m) => m.resell);
    const sentToday = (r) => r.reminded && Date.now() / 1000 - r.reminded < 12 * 3600;
    const risk = a.at_risk.length ? h("div", { class: "risk-list" }, a.at_risk.map((r) => {
      const days = r.expires ? Math.max(0, Math.ceil((r.expires - Date.now() / 1000) / 86400)) : null;
      const btn = h("button", { class: "ghost", disabled: !r.telegram || sentToday(r), title: r.telegram ? "" : (fa ? "این مشتری به ربات وصل نیست" : "Not linked to the bot"),
        onclick: async () => { btn.disabled = true; try { await api("POST", `/api/v2/analytics/remind/${encodeURIComponent(r.email)}`); btn.textContent = fa ? "✓ فرستاده شد" : "✓ Sent"; } catch (e) { toast(e.message); btn.disabled = false; } } },
        !r.telegram ? (fa ? "بدون تلگرام" : "No Telegram") : sentToday(r) ? (fa ? "✓ امروز فرستاده شد" : "✓ Sent today") : (fa ? "🔔 یادآوری" : "🔔 Remind"));
      const why = [r.reason === "volume" ? (fa ? "حجم رو به اتمام" : "Low volume") : (fa ? "پایان مدت" : "Ending"),
        days !== null ? (fa ? `${fmtNum(days)} روز مانده` : `${days} d left`) : null, r.left_bytes >= 0 ? gbs(r.left_bytes) : null].filter(Boolean).join(" · ");
      return h("div", { class: `risk-row ${days !== null && days <= 2 ? "hot" : ""}` },
        h("div", { class: "rr-main" }, h("a", { href: `#/clients?q=${encodeURIComponent(r.email)}`, dir: "ltr" }, r.email), owner && r.seller ? h("span", { class: "badge" }, r.seller) : null,
          h("div", { class: "small muted" }, why)), btn);
    })) : h("p", { class: "muted small" }, fa ? "فعلاً مشتری در خطر نیست." : "No one at risk right now.");
    fill(body,
      h("div", { class: "grid" },
        kpi(money(a.revenue_month), fa ? "درآمد این ماه (تومان)" : "Revenue this month", delta(a.revenue_month, a.revenue_prev), owner ? (fa ? "فروش فروشگاه + فروش نماینده‌ها" : "shop + resellers") : (fa ? "فروش فروشگاه ربات شما" : "your bot shop")),
        kpi(fmtNum(a.active), fa ? "مشتری فعال" : "Active customers", null, fa ? `از ${fmtNum(a.total)} سرویس` : `of ${a.total}`),
        kpi(a.renew_rate < 0 ? "—" : pct(a.renew_rate), fa ? "نرخ تمدید (۳۰ روز)" : "Renewal rate (30d)", null, fa ? `${fmtNum(a.renewed_30d)} تمدید · ${fmtNum(a.lapsed_30d)} تمدید نکرده` : `${a.renewed_30d} renewed · ${a.lapsed_30d} lapsed`),
        kpi(fmtNum(a.expiring_7d), fa ? "پایان در ۷ روز آینده" : "Ending in 7 days")),
      h("section", { class: "card an-sec" }, h("h2", {}, fa ? "درآمد ماهانه (میلیون تومان)" : "Monthly revenue (million toman)"),
        showResell ? chartLegend([{ id: "a", name: fa ? "فروشگاه" : "Shop", color: "var(--ion)" }, { id: "b", name: fa ? "نماینده‌ها (به قیمت نماینده)" : "Resellers", color: "var(--sky)" }]) : null,
        bars(a.months, showResell ? ["shop", "resell"] : ["shop"], ["var(--ion)", "var(--sky)"], [fa ? "فروشگاه" : "Shop", fa ? "نماینده‌ها" : "Resellers"], fa ? "درآمد ماهانه" : "Monthly revenue")),
      h("section", { class: "card an-sec" }, h("h2", {}, fa ? "مشتری‌ها ماه‌به‌ماه" : "Customers by month"),
        h("div", { class: "table-wrap" }, h("table", { class: "table" },
          h("thead", {}, h("tr", {}, [fa ? "ماه" : "Month", fa ? "جدید" : "New", fa ? "تمدید" : "Renewed", fa ? "ریزش" : "Churned"].map((x) => h("th", { scope: "col" }, x)))),
          h("tbody", {}, a.months.slice().reverse().map((m) => h("tr", {}, h("th", { scope: "row" }, monthName(m.month)), h("td", {}, fmtNum(m.new)), h("td", {}, fmtNum(m.renewed)), h("td", { class: m.churned ? "bad" : "" }, fmtNum(m.churned))))))),
        h("p", { class: "small muted" }, fa ? "ریزش: سرویس‌هایی که در آن ماه تمام شدند و بیش از ۷ روز است تمدید نشده‌اند. آمار تمدید از نسخهٔ ۲.۲۳ به بعد کامل است." : "Churned: ended that month and not renewed for 7+ days.")),
      h("section", { class: "card an-sec" }, h("h2", {}, fa ? "مشتری فعال در ۳۰ روز اخیر" : "Active customers, last 30 days"), line(a.daily)),
      h("section", { class: "card an-sec" }, h("h2", {}, fa ? "در خطر: تمدید نکرده‌اند" : "At risk"),
        h("p", { class: "small muted" }, fa ? "سرویس‌هایی که تا ۷ روز دیگر تمام می‌شوند یا کمتر از ۱۵٪ حجمشان مانده. «یادآوری» یک پیام با دکمهٔ تمدید از ربات برای مشتری می‌فرستد (هر مشتری حداکثر هر ۱۲ ساعت یک بار)." : "Ending within 7 days or under 15% volume left. Remind sends a renewal message from the bot."),
        risk),
      a.products.length ? h("section", { class: "card an-sec" }, h("h2", {}, fa ? "پرفروش‌ها (۳۰ روز)" : "Best sellers (30 days)"),
        h("div", { class: "table-wrap" }, h("table", { class: "table" },
          h("thead", {}, h("tr", {}, [fa ? "محصول" : "Product", fa ? "تعداد" : "Sold", fa ? "مبلغ" : "Toman"].map((x) => h("th", { scope: "col" }, x)))),
          h("tbody", {}, a.products.map((p) => h("tr", {}, h("th", { scope: "row" }, p.name), h("td", {}, fmtNum(p.count)), h("td", {}, money(p.toman)))))))) : null,
      a.sellers.length ? h("section", { class: "card an-sec" }, h("h2", {}, fa ? "نرخ تمدید به تفکیک فروشنده (۳۰ روز)" : "Renewal rate by seller"),
        h("div", { class: "table-wrap" }, h("table", { class: "table" },
          h("thead", {}, h("tr", {}, [fa ? "فروشنده" : "Seller", fa ? "تمدید" : "Renewed", fa ? "تمدید نکرده" : "Lapsed", fa ? "نرخ" : "Rate"].map((x) => h("th", { scope: "col" }, x)))),
          h("tbody", {}, a.sellers.sort((x, y) => y.rate - x.rate).map((x) => h("tr", {}, h("th", { scope: "row" }, x.seller ? x.name || x.seller : (fa ? "فروش مستقیم من" : "My own")), h("td", {}, fmtNum(x.renewed)), h("td", {}, fmtNum(x.lapsed)), h("td", {}, h("b", {}, pct(x.rate))))))))) : null);
  };
  const sel = owner && reps.length ? h("select", { onchange: (e) => { seller = e.target.value; try { sessionStorage.setItem("anSeller", seller); } catch {} load(); } },
    h("option", { value: "" }, fa ? "همه" : "Everyone"), h("option", { value: "-", selected: seller === "-" }, fa ? "فروش مستقیم من" : "My own sales"),
    reps.map((r) => h("option", { value: r.ID, selected: r.ID === seller }, r.Name || r.ID))) : null;
  load();
  return h("div", { class: "analytics" },
    h("div", { class: "row sticky-head" }, h("h1", {}, t("analytics")), h("span", { class: "spacer" }), sel),
    body);
}

// ---------- Support tickets ----------
async function viewTickets() {
  const fa = lang === "fa";
  const owner = me.role === "owner";
  let f = sessionStorage.getItem("ticketFilter") || "active";
  const list = h("div", { class: "card order-list" });
  const chips = h("div", { class: "chips", role: "tablist" });
  const fmtTime = (u) => new Intl.DateTimeFormat(fa ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const stName = { open: [fa ? "منتظر پاسخ" : "waiting", "warn"], answered: [fa ? "پاسخ داده شد" : "answered", "good"], closed: [fa ? "بسته" : "closed", ""] };
  let all = [];
  const draw = () => {
    const counts = { active: all.filter((x) => x.status !== "closed").length, open: all.filter((x) => x.status === "open").length, answered: all.filter((x) => x.status === "answered").length, closed: all.filter((x) => x.status === "closed").length, all: all.length };
    const names = { active: fa ? "باز" : "Active", open: fa ? "منتظر پاسخ" : "Waiting", answered: fa ? "پاسخ‌داده" : "Answered", closed: fa ? "بسته" : "Closed", all: fa ? "همه" : "All" };
    chips.replaceChildren(...Object.keys(counts).map((k) => h("button", { type: "button", role: "tab", class: `chip ${f === k ? "on" : ""}`, "aria-selected": String(f === k),
      onclick: () => { f = k; try { sessionStorage.setItem("ticketFilter", k); } catch {} draw(); } }, names[k], h("span", { class: "count" }, fmtNum(counts[k])))));
    const l = all.filter((x) => f === "all" || (f === "active" ? x.status !== "closed" : x.status === f));
    list.replaceChildren(...(l.length ? l.map((x) => h("button", { type: "button", class: `order-row ticket-row ${x.status}`, onclick: () => openTicket(x.id) },
      h("div", { class: "o-id" }, h("b", {}, `#${fmtNum(x.id)}`), h("span", { class: "muted small" }, fmtTime(x.updated))),
      h("div", { class: "o-main" }, h("b", {}, x.name || String(x.chat)), h("span", { class: "muted small" }, x.last)),
      h("div", { class: "o-amt" }, h("span", { class: "small muted" }, fa ? `${fmtNum(x.count)} پیام` : `${x.count} msgs`), owner && x.seller ? h("span", { class: "badge" }, x.seller) : null),
      h("div", { class: "o-st" }, h("span", { class: `badge ${stName[x.status][1]}` }, stName[x.status][0])))) :
      [h("p", { class: "muted small", style: "padding:14px" }, f === "active" || f === "open" ? (fa ? "تیکت بازی ندارید." : "No open tickets.") : t("empty"))]));
  };
  const load = async () => { all = await api("GET", "/api/v2/tickets"); draw(); };
  const openTicket = async (id) => {
    const tk = await api("GET", `/api/v2/tickets/${id}`);
    const thread = h("div", { class: "ticket-thread" }, (tk.msgs || []).map((m) => h("div", { class: `tmsg ${m.staff ? "staff" : "cust"}` },
      h("div", { class: "tmeta small muted" }, m.staff ? (fa ? `پشتیبانی · ${m.author}` : `Support · ${m.author}`) : (tk.name || (fa ? "مشتری" : "Customer")), " · ", fmtTime(m.unix),
        m.staff && !m.delivered ? h("span", { class: "badge warn" }, fa ? "در صف ارسال" : "queued") : null),
      h("div", { class: "tbody" }, m.text))));
    const txt = h("textarea", { rows: 4, maxlength: "4000", placeholder: fa ? "پاسخ را بنویسید… (از طریق ربات برای مشتری فرستاده می‌شود)" : "Write the answer… (sent to the customer by the bot)" });
    const err = h("div", { class: "error" });
    const send = (close) => async (e) => {
      e.preventDefault(); err.textContent = "";
      if (!txt.value.trim()) { txt.focus(); return; }
      try { await api("POST", `/api/v2/tickets/${id}/reply`, { text: txt.value, close }); document.getElementById("modal").close(); toast(fa ? "پاسخ ثبت شد؛ ربات تا چند ثانیهٔ دیگر می‌فرستد" : "Answer queued; the bot sends it in a few seconds"); load(); } catch (x) { err.textContent = x.message; }
    };
    const actions = tk.status === "closed" ? [] : [
      h("button", { class: "ghost", value: "close", onclick: async (e) => { e.preventDefault(); await guarded(() => api("POST", `/api/v2/tickets/${id}/close`)); document.getElementById("modal").close(); load(); } }, fa ? "بستن بدون پاسخ" : "Close"),
      h("button", { value: "sc", onclick: send(true) }, fa ? "ارسال و بستن" : "Send and close"),
      h("button", { class: "primary", value: "s", onclick: send(false) }, fa ? "ارسال پاسخ" : "Send")];
    const d = openModal(`${fa ? "تیکت" : "Ticket"} #${fmtNum(id)} · ${tk.name || tk.chat}`, h("div", { class: "form" },
      h("div", { class: "small muted" }, [owner && tk.seller ? `${fa ? "فروشنده" : "Seller"}: ${tk.seller}` : null, (tk.clients || []).length ? `${fa ? "سرویس‌ها" : "Services"}: ${tk.clients.join("، ")}` : null, `Telegram: ${tk.chat}`].filter(Boolean).join(" · ")),
      thread, tk.status === "closed" ? h("p", { class: "muted small" }, fa ? "این تیکت بسته است؛ پیام تازهٔ مشتری تیکت جدیدی باز می‌کند." : "Closed; a new customer message opens a new ticket.") : txt, err), actions);
    d.classList.add("wide");
    requestAnimationFrame(() => { thread.scrollTop = thread.scrollHeight; });
  };
  await load();
  return h("div", {},
    h("div", { class: "row sticky-head" }, h("h1", {}, t("tickets")), h("span", { class: "spacer" }), h("button", { class: "ghost", onclick: load }, fa ? "تازه کردن" : "Refresh")),
    h("p", { class: "small muted" }, fa ? "پیام‌هایی که مشتری‌ها در ربات به پشتیبانی می‌فرستند. هر مشتری تا وقتی تیکتش باز است در همان تیکت ادامه می‌دهد؛ پاسخ شما از طریق ربات به خودش می‌رسد." : "Messages customers send to support in the bot. Answers go back to them through the bot."),
    chips, list);
}

// ---------- Toman wallets, top-ups, resellers' own bots ----------
function walletsPane() {
  const fa = lang === "fa";
  const box = h("div", { class: "form" });
  const fmtTime = (u) => new Intl.DateTimeFormat(fa ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const load = async () => {
    let tops, wals;
    try { [tops, wals] = await Promise.all([api("GET", "/api/v2/shop/topups"), api("GET", "/api/v2/shop/wallets")]); } catch (x) { fill(box, h("p", { class: "error" }, x.message)); return; }
    const review = tops.filter((x) => x.status === "review" || x.status === "awaiting");
    const decide = async (x, ok) => { await guarded(() => api("POST", `/api/v2/shop/topups/${x.id}/${ok ? "approve" : "reject"}`)); toast(ok ? (fa ? "تأیید شد" : "Approved") : (fa ? "رد شد" : "Rejected")); load(); };
    const st = { awaiting: [fa ? "منتظر رسید" : "awaiting", ""], review: [fa ? "منتظر بررسی" : "to review", "warn"], approved: [fa ? "تأیید شد" : "approved", "good"], rejected: [fa ? "رد شد" : "rejected", "bad"] };
    const topRow = (x) => h("div", { class: `order-row ${x.status}` },
      h("div", { class: "o-id" }, h("b", {}, `#${fmtNum(x.id)}`), h("span", { class: "muted small" }, fmtTime(x.created))),
      h("div", { class: "o-main" }, h("b", { dir: "ltr" }, String(x.chat)), h("span", { class: "muted small" }, (fa ? "موجودی فعلی: " : "balance: ") + money(x.balance))),
      h("div", { class: "o-amt" }, h("b", {}, money(x.amount))),
      h("div", { class: "o-st" }, h("span", { class: `badge ${st[x.status][1]}` }, st[x.status][0]), x.has_receipt ? h("span", { class: "badge good" }, fa ? "🧾 رسید در تلگرام" : "🧾 receipt in Telegram") : null),
      x.status === "review" || x.status === "awaiting" ? h("div", { class: "o-act" }, h("button", { class: "primary", onclick: () => decide(x, true) }, fa ? "تأیید" : "Approve"), h("button", { class: "ghost danger", onclick: () => decide(x, false) }, fa ? "رد" : "Reject")) : null);
    const chat = h("input", { dir: "ltr", inputmode: "numeric", placeholder: fa ? "آیدی عددی تلگرام" : "Telegram user id" });
    const delta = h("input", { dir: "ltr", inputmode: "numeric", placeholder: fa ? "مبلغ (منفی = کسر)" : "Amount (negative = take)" });
    const note = h("input", { placeholder: fa ? "توضیح" : "Note" });
    fill(box,
      h("div", { class: "card form" }, h("h2", {}, fa ? `شارژهای منتظر (${fmtNum(review.length)})` : `Top-ups to review (${review.length})`),
        h("p", { class: "small muted" }, fa ? "مشتری در ربات مبلغ را انتخاب می‌کند، به کارت شما واریز می‌کند و عکس رسید را می‌فرستد. رسید در تلگرام برای شما می‌آید؛ همان‌جا یا این‌جا تأیید کنید." : "In the bot the customer picks an amount, pays to your card and sends the receipt photo, which reaches you in Telegram; approve there or here."),
        h("div", { class: "order-list" }, ...(tops.length ? tops.slice(0, 60).map(topRow) : [h("p", { class: "muted small", style: "padding:10px" }, fa ? "شارژی ثبت نشده." : "No top-ups yet.")]))),
      h("div", { class: "card form" }, h("h2", {}, fa ? `کیف پول مشتری‌ها (${fmtNum(wals.length)})` : `Customer wallets (${wals.length})`),
        h("div", { class: "row" }, chat, delta, note, h("button", { onclick: async () => {
          const r = await guarded(() => api("POST", "/api/v2/shop/wallet", { chat: +chat.value || 0, delta: +delta.value || 0, note: note.value }));
          toast((fa ? "موجودی جدید: " : "New balance: ") + money(r.balance)); load();
        } }, fa ? "اعمال" : "Apply")),
        wals.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" }, h("thead", {}, h("tr", {}, h("th", {}, fa ? "تلگرام" : "Telegram"), h("th", {}, fa ? "موجودی" : "Balance"), h("th", {}, fa ? "آخرین تراکنش" : "Last"))),
          h("tbody", {}, ...wals.slice(0, 300).map((w) => h("tr", { onclick: () => { chat.value = w.chat; delta.focus(); }, style: "cursor:pointer" }, h("td", { dir: "ltr" }, String(w.chat)), h("td", {}, money(w.balance)), h("td", {}, fmtTime(w.last)))))))
          : h("p", { class: "muted small" }, fa ? "هنوز کیف پولی ساخته نشده." : "No wallets yet.")));
  };
  load();
  return box;
}

// A reseller's own Telegram sales bot.
// mirzaCard connects a sales bot or a website made for PasarGuard to this
// panel (Mirza above all). A reseller picks how it sells: its Dark bot,
// Mirza (its Dark bot goes off: both would serve the same customers) or the
// PasarGuard API for any other bot or site (its Dark bot stays on). The
// owner's access never turns the main bot off.
function mirzaCard(path, isRep) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const body = h("div", { class: "form" });
  const err = h("div", { class: "error", role: "alert" });
  const ago = (u) => {
    if (!u) return T("هنوز وصل نشده", "never connected");
    const m = Math.max(0, Math.round((Date.now() / 1000 - u) / 60));
    return m < 2 ? T("همین الان", "just now") : m < 60 ? T(`${fmtNum(m)} دقیقه پیش`, `${m} min ago`) : m < 1440 ? T(`${fmtNum(Math.round(m / 60))} ساعت پیش`, `${Math.round(m / 60)} h ago`) : new Date(u * 1000).toLocaleDateString(fa ? "fa-IR" : "en-GB");
  };
  const copyBtn = (value) => h("button", { type: "button", class: "ghost", onclick: () => copy(value) }, T("کپی", "Copy"));
  const field = (label, value, secret) => h("div", { class: "mz-field" }, h("span", { class: "small muted" }, label),
    h("div", { class: "mz-copy" }, h("code", { dir: "ltr", class: secret ? "mz-secret" : "" }, value), copyBtn(value)));
  // shown keeps a password made in this visit on screen while only the mode
  // changes (a mode switch keeps the password but does not send it again).
  const shown = { password: "", api_key: "" };
  let busy = false; // one request at a time: a second click waits for the first
  const send = async (method, data, sub) => {
    if (busy) return;
    busy = true;
    err.textContent = "";
    try {
      const a = await api(method, path + (sub || ""), data);
      for (const k of ["password", "api_key"]) {
        if (a[k]) shown[k] = a[k];
        else if (a.enabled && shown[k]) a[k] = shown[k];
      }
      if (!a.enabled) shown.password = shown.api_key = "";
      draw(a);
    } catch (x) { err.textContent = x.message; }
    finally { busy = false; }
  };
  const turnOff = (mode) => {
    const q = !isRep ? T("دسترسی API قطع شود؟ ربات‌ها و سایت‌هایی که با آن وصل‌اند دیگر کار نمی‌کنند.", "Turn the API access off? Bots and sites using it stop working.")
      : mode === "mirza" ? T("اتصال میرزا قطع و ربات دارک دوباره روشن شود؟", "Disconnect Mirza and turn the Dark bot back on?")
        : T("دسترسی API قطع شود؟ ربات‌ها و سایت‌هایی که با آن وصل‌اند دیگر کار نمی‌کنند.", "Turn the API access off? Bots and sites using it stop working.");
    if (confirm(q)) send("DELETE");
  };
  // choose moves a reseller to a way of selling: off, or on in a mode. A
  // new access comes with a fresh password; switching keeps the password.
  const choose = (want, cur) => {
    if (want === cur) return;
    if (want === "dark") return turnOff(cur);
    if (cur === "dark") return send("POST", { mode: want });
    // Between Mirza and the API nothing breaks (the same username, password
    // and API key) and it is undone with one click: no question.
    send("PUT", { mode: want });
  };

  // The PasarGuard API in short: how to sign in, and the calls most bots
  // and sites need. Same paths and fields as PasarGuard's own API.
  function guide(a) {
    const base = location.origin;
    // In Persian text, Latin words (paths, field names) are isolated so the
    // right-to-left line keeps its order and its brackets.
    const bi = (text) => fa ? text.split(/(\/?[A-Za-z_][A-Za-z0-9_\/{}.\-]*)/).map((x, i) => i % 2 ? h("bdi", { dir: "ltr" }, x) : x) : text;
    const login = `curl -X POST ${base}/api/admin/token \\\n  -d "username=${a.username}" -d "password=YOUR_PASSWORD"`;
    const until = new Date(Date.now() + 30 * 86400e3).toISOString().slice(0, 10) + "T00:00:00Z";
    const create = `curl -X POST ${base}/api/user \\\n  -H "Authorization: Bearer TOKEN" -H "Content-Type: application/json" \\\n  -d '{"username": "ali123", "data_limit": 32212254720, "expire": "${until}"}'`;
    const calls = [
      ["GET", "/api/users", T("فهرست کاربرها، با فیلتر وضعیت، جستجو و صفحه‌بندی", "list users, by status, search and page")],
      ["POST", "/api/user", T("ساخت کاربر", "create a user")],
      ["GET", "/api/user/{username}", T("یک کاربر: مصرف، انقضا، لینک‌ها", "one user: usage, expiry, links")],
      ["PUT", "/api/user/{username}", T("تمدید یا تغییر (حجم، انقضا، روشن/خاموش)", "renew or change (volume, expiry, on/off)")],
      ["POST", "/api/user/{username}/reset", T("صفر کردن مصرف", "reset usage")],
      ["POST", "/api/user/{username}/revoke_sub", T("لینک و مشخصات تازه", "a new link and credentials")],
      ["DELETE", "/api/user/{username}", T("حذف", "delete")],
      ["GET", "/api/groups", T("اینباندهای مجاز شما، همان group_ids", "the inbounds you may give: the group_ids")],
      ["GET", "/api/user_templates", T("پلن‌های شما", "your plans")],
      ["POST", "/api/user/from_template", T("فروش یک پلن", "sell a plan")],
      ["GET", "/api/users/usage", T("نمودار مصرف", "usage chart")],
    ];
    const code = (text) => h("div", { class: "mz-code-row" }, h("pre", { dir: "ltr", class: "mz-code" }, text), copyBtn(text.replace(/\\\n\s*/g, "")));
    return h("details", { class: "adv-box mz-guide" }, h("summary", {}, T("📘 راهنمای API (مثل API پاسارگاد)", "📘 API guide (as PasarGuard's API)")),
      h("p", { class: "small muted" }, isRep
        ? T("هر ربات یا سایتی که برای پاسارگاد ساخته شده با همین آدرس‌ها و فیلدها کار می‌کند. کاربرهایی که می‌سازد مال شما هستند و از اعتبارتان کم می‌شود.",
          "Any bot or site made for PasarGuard works with these same paths and fields. The users it makes are yours and use your credit.")
        : T("هر ربات یا سایتی که برای پاسارگاد ساخته شده با همین آدرس‌ها و فیلدها کار می‌کند. کاربرهایی که می‌سازد مال پنل هستند (نه نماینده‌ها).",
          "Any bot or site made for PasarGuard works with these same paths and fields. The users it makes are the panel's own (not a reseller's).")),
      h("p", { class: "small" }, T("۱. گرفتن توکن (۲۴ ساعت اعتبار دارد):", "1. Get a token (valid for 24 hours):")), code(login),
      h("p", { class: "small" }, T("۲. توکن را در هر درخواست بفرستید:", "2. Send it with every call:")), code("Authorization: Bearer TOKEN"),
      h("p", { class: "small" }, T("یا به‌جای توکن، API Key را در هر درخواست بفرستید (مثل پاسارگاد):", "Or, instead of a token, send the API Key with every call (as in PasarGuard):")), code("X-Api-Key: pg_key_..."),
      h("p", { class: "small" }, T("۳. درخواست‌های اصلی:", "3. The main calls:")),
      h("div", { class: "mz-calls", dir: "ltr" }, calls.map(([m, p, what]) => h("div", { class: "mz-call" },
        h("code", { class: `mz-method m-${m.toLowerCase()}` }, m), h("code", { class: "mz-path" }, p), h("span", { class: "small muted", dir: fa ? "rtl" : "ltr" }, bi(what))))),
      h("p", { class: "small" }, T("نمونهٔ ساخت کاربر ۳۰ گیگ و ۳۰ روزه:", "Example: a 30 GB, 30-day user:")), code(create),
      h("p", { class: "small muted" }, bi(T("حجم یا data_limit به بایت است و ۰ یعنی نامحدود. انقضا یا expire زمان یونیکس یا تاریخ ISO است. بدون group_ids کاربر همهٔ اینباندهایی را که می‌توانید بدهید می‌گیرد؛ برای انتخاب، شماره‌ها را از /api/groups بگیرید. بقیهٔ مسیرهای پاسارگاد هم هستند: by-id، کارهای گروهی و قالب‌ها.",
        "data_limit is in bytes (0 = unlimited) and expire is a Unix time or an ISO date. Without group_ids the user gets every inbound you may give; to choose, take the numbers from /api/groups. PasarGuard's other paths (by-id, bulk actions, templates) are here too."))));
  }

  function draw(a) {
    const on = !!a.enabled;
    const cur = !on ? "dark" : (a.mode || "mirza");
    const seg = (key, icon, label) => h("button", { type: "button", class: "seg", "aria-pressed": String(cur === key), onclick: () => choose(key, cur) }, icon + " ", label);
    const modes = isRep ? h("div", { class: "segs", role: "group", "aria-label": T("روش فروش", "How you sell") },
      seg("dark", "🤖", T("ربات دارک", "Dark bot")), seg("mirza", "🔗", T("ربات میرزا", "Mirza bot")), seg("api", "🔌", T("API پاسارگاد", "PasarGuard API"))) : null;
    if (!on) {
      fill(body, modes, err,
        h("p", { class: "small muted" }, isRep
          ? T("فروش با ربات دارک است. اگر مشتری‌هایتان در ربات میرزا هستند «ربات میرزا» را بزنید (ربات دارک خاموش می‌شود). برای وصل کردن ربات یا سایت دیگری که با پاسارگاد کار می‌کند «API پاسارگاد» را بزنید (ربات دارک روشن می‌ماند).",
            "You sell with the Dark bot. If your customers are in a Mirza bot, choose “Mirza bot” (the Dark bot goes off). To connect another bot or a website that works with PasarGuard, choose “PasarGuard API” (the Dark bot stays on).")
          : T("ربات میرزا یا هر ربات و سایتی که پنل پاسارگاد را می‌شناسد با این دسترسی به این پنل وصل می‌شود. ربات اصلی دارک روشن می‌ماند.",
            "Mirza, or any bot or site that knows the PasarGuard panel, connects to this panel with this access. The main Dark bot stays on.")),
        isRep ? null : h("button", { type: "button", class: "primary", onclick: () => send("POST", { mode: "api" }) }, T("ساخت دسترسی API", "Create the API access")));
      return;
    }
    const mirza = isRep ? cur === "mirza" : true;
    fill(body, modes, err,
      h("div", { class: "mz-badges" }, h("span", { class: "badge good" }, "● ", T("فعال", "on")),
        h("span", { class: "badge" }, T("آخرین اتصال: ", "last call: "), ago(a.last_used)),
        h("span", { class: "badge" }, T(`کاربر ساخته شده با API: ${fmtNum(a.made)}`, `users made through the API: ${a.made}`))),
      mirza ? h("ol", { class: "mz-steps small" },
        h("li", {}, T("در پنل ادمین میرزا «🖥 اضافه کردن پنل» را بزنید و نوع پنل را «پاسارگارد» انتخاب کنید.", "In Mirza's admin menu tap “🖥 Add panel” and choose the PasarGuard type.")),
        h("li", {}, T("آدرس زیر را بفرستید؛ وقتی API Key خواست، API Key زیر را بفرستید (اگر میرزا نام کاربری و رمز خواست، همان‌ها را).", "Send the address below; when it asks for an API Key, send the API Key below (if Mirza asks for a username and password instead, send those).")),
        h("li", {}, T("در «✏️ مدیریت پنل» دکمهٔ «🎛 تنظیم نام گروه» را بزنید و عبارت all-groups را بفرستید تا همهٔ اینباندهای مجاز شما برای کاربرهای جدید انتخاب شود (یا نام کاربری یکی از کاربرهایتان را بفرستید تا فقط اینباندهای او برداشته شود).", "In “✏️ Panel management” tap “🎛 Set group name” and send all-groups to give new users every inbound you may give (or send one of your users' names to copy just its inbounds).")))
        : h("p", { class: "small" }, T("ربات یا سایت شما با آدرس و API Key زیر (یا نام کاربری و رمز) مثل یک پنل پاسارگاد وصل می‌شود. راهنمای API پایین همین کارت است.",
          "Your bot or site connects with the address and API Key below (or the username and password), as to a PasarGuard panel. The API guide is at the bottom of this card.")),
      field(T("آدرس پنل (API)", "Panel address (API)"), location.origin),
      field(T("نام کاربری", "Username"), a.username),
      a.password ? field(T("رمز (فقط همین یک بار نشان داده می‌شود)", "Password (shown only this once)"), a.password, true)
        : h("p", { class: "small muted" }, T("رمز فقط هنگام ساخت نشان داده می‌شود. اگر ندارید، رمز تازه بسازید و در ربات یا سایتتان عوض کنید.", "The password is shown only when made. If you lost it, make a new one and change it in your bot or site.")),
      a.api_key ? field(T("API Key (در هدر X-Api-Key؛ فقط همین یک بار نشان داده می‌شود)", "API Key (sent in X-Api-Key; shown only this once)"), a.api_key, true)
        : h("p", { class: "small muted" }, a.has_key
          ? T("API Key فقط هنگام ساخت نشان داده می‌شود. اگر ندارید، API Key تازه بسازید و در ربات یا سایتتان عوض کنید.", "The API Key is shown only when made. If you lost it, make a new one and change it in your bot or site.")
          : T("هنوز API Key ساخته نشده؛ «API Key تازه» را بزنید.", "No API Key yet; tap “New API Key”.")),
      isRep ? h("p", { class: "small muted" }, cur === "mirza"
        ? T("کاربرهایی که میرزا می‌سازد زیر حساب شما ثبت و از اعتبارتان کم می‌شود. ربات دارک شما تا وقتی این حالت روشن است خاموش است.", "Users Mirza makes are yours and use your credit. Your Dark bot is off while this is on.")
        : T("کاربرهایی که از راه API ساخته می‌شوند زیر حساب شما ثبت و از اعتبارتان کم می‌شود. ربات دارک شما روشن می‌ماند.", "Users made through the API are yours and use your credit. Your Dark bot stays on.")) : null,
      guide(a),
      h("div", { class: "row" },
        h("button", { type: "button", onclick: () => confirm(T("رمز تازه ساخته شود؟ رمز قبلی دیگر کار نمی‌کند و باید در ربات یا سایتتان عوض شود. API Key عوض نمی‌شود.", "Make a new password? The old one stops working; change it in your bot or site. The API Key stays.")) && send("POST") }, T("رمز تازه", "New password")),
        h("button", { type: "button", onclick: () => (!a.has_key || confirm(T("API Key تازه ساخته شود؟ کلید قبلی دیگر کار نمی‌کند و باید در ربات یا سایتتان عوض شود. رمز عوض نمی‌شود.", "Make a new API Key? The old one stops working; change it in your bot or site. The password stays."))) && send("POST", undefined, "/key") }, T("API Key تازه", "New API Key")),
        isRep ? null : h("button", { type: "button", class: "danger", onclick: () => turnOff("api") }, T("قطع دسترسی", "Turn off"))));
  }
  api("GET", path).then(draw).catch((x) => fill(body, h("p", { class: "error" }, x.message)));
  return h("section", { class: "card form" }, h("h2", {}, isRep ? T("روش فروش", "How you sell") : T("🔌 API پاسارگاد (میرزا و ربات‌های دیگر)", "🔌 PasarGuard API (Mirza and other bots)")), body);
}

function repBotCard(path) {
  const fa = lang === "fa";
  const tok = h("input", { type: "password", dir: "ltr", autocomplete: "off", placeholder: "123456789:AA..." });
  const en = h("input", { type: "checkbox" });
  const mini = h("input", { type: "checkbox", checked: true });
  const st = h("div", { class: "small" });
  // The numeric Telegram ids are the reseller's own to set, in its own panel.
  const own = me.role === "rep";
  const owners = idList([]);
  const draw = (b) => {
    owners.set(b.owners || []);
    en.checked = !!b.enabled;
    mini.checked = b.miniapp !== false;
    tok.placeholder = b.has_token ? (fa ? "توکن ثبت شده — برای تغییر، توکن جدید بزنید" : "token saved — type a new one to change") : "123456789:AA...";
    st.replaceChildren(b.running ? h("span", { class: "badge good" }, `● ${fa ? "روشن" : "running"} @${b.username || ""}`) :
      b.error ? h("span", { class: "badge bad" }, (fa ? "خطا: " : "error: ") + b.error)
        : h("span", { class: "badge" }, b.mirza ? (fa ? "خاموش: با ربات میرزا می‌فروشید (روش فروش)" : "off: you sell through Mirza (How you sell)") : (fa ? "خاموش" : "off")));
  };
  api("GET", path).then(draw).catch(() => {});
  return h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault();
    const r = await guarded(() => api("PUT", path, own ? { token: tok.value.trim(), enabled: en.checked, miniapp: mini.checked, owners: owners.get() } : { token: tok.value.trim(), enabled: en.checked, miniapp: mini.checked }));
    tok.value = ""; draw(r); toast(fa ? "ذخیره شد؛ ربات تا چند ثانیه دیگر روشن می‌شود" : "Saved; the bot starts in a few seconds");
  } },
    h("h2", {}, "🤖 ", fa ? "ربات فروش اختصاصی" : "Own sales bot"),
    h("p", { class: "small muted" }, fa ? "از @BotFather یک ربات بسازید و توکنش را این‌جا بزنید. ربات فقط محصولات شما را می‌فروشد، پرداخت‌ها به کارت شما می‌آید، کیف پول مشتری‌ها مال شماست و فقط کلاینت‌های خودتان را نشان می‌دهد. رسیدها برای تلگرامی که به نمایندگی وصل کرده‌اید فرستاده می‌شود." : "Create a bot with @BotFather and paste its token. It sells only your products, payments go to your card, the customers' wallets are yours and it shows only your clients. Receipts go to the Telegram you linked to your reseller account."),
    st, h("label", {}, h("span", {}, fa ? "توکن ربات" : "Bot token"), tok),
    !own ? null : h("label", {}, h("span", {}, fa ? "شناسهٔ عددی تلگرام مدیران" : "Managers' numeric Telegram ids"), owners.el,
      h("span", { class: "small muted" }, fa ? "هر شناسه در ربات، این پنل را کامل مدیریت می‌کند (سفارش‌ها، رسیدها، هشدارها). با ＋ شناسهٔ بعدی را اضافه کنید؛ شناسه را از @userinfobot بگیرید." : "Each id fully manages this panel in the bot (orders, receipts, alerts). ＋ adds another; get an id from @userinfobot.")),
    h("label", { class: "check" }, en, fa ? "روشن باشد" : "Enabled"),
    h("label", { class: "check" }, mini, fa ? "دکمهٔ فروشگاه (مینی‌اپ) در ربات: پلن‌ها، کارت و کیف پول خودتان، با نام و لوگوی برندتان" : "Store button (Mini App) in the bot: your plans, card and wallet, with your brand"),
    h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, t("save"))));
}

// A seller's card for card-to-card payment (resellers: their own card).
function sellerCardCard(path) {
  // Several cards per seller (up to five) and the safe-card options.
  return multiCardCard(path.replace("/api/v2/shop/card", "/api/v2/shop/cards"));
}

function multiCardCard(path) {
  const fa = lang === "fa";
  const T = (a, b) => (fa ? a : b);
  const list = h("div", { class: "form" });
  const phone = h("input", { type: "checkbox" }), iran = h("input", { type: "checkbox" }), last4 = h("input", { type: "checkbox" });
  let max = 5;
  const rows = [];
  const toLocal = (u) => { if (!u) return ""; const d = new Date(u * 1000); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 16); };
  const addRow = (c) => {
    c = c || { enabled: true };
    const num = h("input", { dir: "ltr", inputmode: "numeric", placeholder: "6037 9911 2233 4455", value: c.number || "" });
    const holder = h("input", { value: c.holder || "" }), bank = h("input", { value: c.bank || "" });
    const on = h("input", { type: "checkbox", checked: c.enabled !== false });
    const cap = h("input", { type: "number", min: "0", step: "100000", dir: "ltr", value: c.daily_cap || "" , placeholder: "0" });
    const until = h("input", { type: "datetime-local", dir: "ltr", value: toLocal(c.until) });
    const status = c.number ? h("span", { class: `badge ${c.live ? "good" : ""}` },
      c.live ? T("فعال", "live") : T("غیرفعال", "off"), " · ", T("امروز: ", "today: "), tomanTxt(c.used_today || 0), c.daily_cap ? ` / ${tomanTxt(c.daily_cap)}` : "") : null;
    const r = { id: c.id || 0, num, holder, bank, on, cap, until };
    const box = h("div", { class: "card" },
      h("div", { class: "row" }, h("strong", {}, "💳 ", T("کارت", "Card")), status,
        h("button", { class: "ghost", type: "button", onclick: () => { rows.splice(rows.indexOf(r), 1); box.remove(); addBtn.disabled = rows.length >= max; } }, T("حذف", "Remove"))),
      h("label", {}, h("span", {}, T("شماره کارت (یا شبا)", "Card number (or IBAN)")), num),
      h("div", { class: "two" }, h("label", {}, h("span", {}, T("به نام", "Holder")), holder), h("label", {}, h("span", {}, T("بانک", "Bank")), bank)),
      h("div", { class: "two" },
        h("label", {}, h("span", {}, T("سقف روزانه (تومان، خالی = بی‌سقف)", "Daily cap (toman, empty = none)")), cap),
        h("label", {}, h("span", {}, T("کارت موقت: تا این زمان (خالی = همیشه)", "Temporary: until (empty = always)")), until)),
      h("label", { class: "check" }, on, h("span", {}, T("روشن", "On"))));
    r.box = box;
    rows.push(r);
    list.append(box);
    addBtn.disabled = rows.length >= max;
  };
  const addBtn = h("button", { class: "ghost", type: "button", onclick: () => addRow() }, T("+ کارت دیگر", "+ Another card"));
  const load = async () => {
    const r = await api("GET", path).catch(() => null);
    list.replaceChildren(); rows.length = 0;
    if (!r) return;
    max = r.max || 5;
    (r.cards || []).forEach(addRow);
    if (!rows.length) addRow();
    const sf = r.safety || {};
    phone.checked = !!sf.require_phone; iran.checked = !!sf.iran_only; last4.checked = !!sf.ask_last4;
    iran.disabled = !phone.checked;
  };
  phone.onchange = () => { iran.disabled = !phone.checked; if (!phone.checked) iran.checked = false; };
  load();
  return h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault();
    const cards = rows.filter((r) => r.num.value.trim()).map((r) => ({ id: r.id, number: r.num.value, holder: r.holder.value, bank: r.bank.value, enabled: r.on.checked,
      daily_cap: Math.max(0, Math.round(+r.cap.value || 0)), until: r.until.value ? Math.floor(new Date(r.until.value).getTime() / 1000) : 0 }));
    await guarded(() => api("PUT", path, { cards, safety: { require_phone: phone.checked, iran_only: iran.checked, ask_last4: last4.checked } }));
    toast(t("saved")); load();
  } },
    h("h2", {}, "💳 ", T("کارت‌های من (کارت به کارت)", "My cards (card-to-card)")),
    h("p", { class: "small muted" }, T("تا ۵ کارت. هر سفارش یا شارژ یکی از کارت‌های روشن را می‌گیرد (کارتی که امروز جای بیشتری دارد) و همان کارت برایش می‌ماند. کارتی که به سقف روزانه برسد تا فردا نشان داده نمی‌شود؛ کارت موقت بعد از زمانش خودکار مخفی می‌شود. بدون کارت، پرداخت کارتی خاموش است.",
      "Up to five cards. Each order or top-up gets one of the cards that are on (the one with the most room today) and keeps it. A card that reaches its daily cap is hidden until tomorrow; a temporary card hides itself after its time. No card = card payment off.")),
    list,
    h("div", { class: "row" }, addBtn),
    h("h3", {}, "🛡 ", T("حالت امن کارت", "Safe card mode")),
    h("label", { class: "check" }, phone, h("span", {}, T("قبل از دیدن کارت، مشتری شمارهٔ موبایلش را تأیید کند", "Customers verify their phone number before they see a card"))),
    h("label", { class: "check" }, iran, h("span", {}, T("فقط شمارهٔ ایران (+98)", "Iranian numbers only (+98)"))),
    h("label", { class: "check" }, last4, h("span", {}, T("بعد از رسید، ۴ رقم آخر کارت واریزکننده پرسیده شود", "Ask for the last four digits of the paying card after the receipt"))),
    h("p", { class: "small muted" }, T("رسید تکراری همیشه بررسی می‌شود: اگر عکس رسیدی قبلاً برای پرداخت دیگری فرستاده شده باشد، روی رسید هشدار می‌آید.", "Duplicate receipts are always checked: a receipt photo sent before for another payment is flagged on the receipt.")),
    h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, t("save"))));
}

// ---------- Card-to-card payment and discount codes ----------
const PAY = {
  fa: { cardTitle: "پرداخت کارت به کارت", cardHint: "با شماره کارت، ربات برای هر سفارش فاکتور با مبلغ و شمارهٔ کارت می‌فرستد. مشتری عکس رسید را می‌فرستد و رسید با دکمهٔ تأیید و رد برای شما می‌آید؛ تأیید یعنی تحویل خودکار. خالی بودن شماره یعنی خاموش.",
    cardNum: "شماره کارت (یا شبا)", cardHolder: "به نام", cardBank: "بانک", save: "ذخیره",
    dcTitle: "کدهای تخفیف", dcHint: "مشتری کد را در ربات یا فروشگاه وارد می‌کند و تخفیف روی سفارش بعدی‌اش اعمال می‌شود. کد درصدی قیمت استارز را هم کم می‌کند. اگر سفارش لغو شود، استفاده برمی‌گردد.",
    dcCode: "کد", dcKind: "نوع", dcPct: "درصد", dcAmt: "مبلغ (تومان)", dcValue: "مقدار", dcMax: "حداکثر استفاده (۰ = نامحدود)", dcDays: "اعتبار (روز، ۰ = همیشه)", dcProduct: "بسته", dcAny: "همهٔ بسته‌ها",
    dcOnce: "هر مشتری فقط یک بار", dcAdd: "ساخت کد", dcNone: "هنوز کدی نساخته‌اید.", dcUsed: "{u} از {m} استفاده", dcUsedInf: "{u} استفاده", dcExp: "تا {d}", dcExpired: "منقضی", dcOff: "خاموش", dcDelAsk: "کد {x} حذف شود؟", dcRandom: "کد تصادفی" },
  en: { cardTitle: "Card-to-card payment", cardHint: "With a card number the bot sends an invoice with the amount and the card for each order. The customer sends a receipt photo; it reaches you with Approve and Refuse — approving delivers at once. Empty number = off.",
    cardNum: "Card number (or IBAN)", cardHolder: "Holder", cardBank: "Bank", save: "Save",
    dcTitle: "Discount codes", dcHint: "Customers enter the code in the bot or the store and it applies to their next order. Percent codes also lower the Stars price. A cancelled order gives the use back.",
    dcCode: "Code", dcKind: "Kind", dcPct: "Percent", dcAmt: "Amount (toman)", dcValue: "Value", dcMax: "Max uses (0 = unlimited)", dcDays: "Valid for (days, 0 = always)", dcProduct: "Product", dcAny: "Every product",
    dcOnce: "Once per customer", dcAdd: "Create code", dcNone: "No codes yet.", dcUsed: "{u} of {m} used", dcUsedInf: "{u} used", dcExp: "until {d}", dcExpired: "expired", dcOff: "off", dcDelAsk: "Delete the code {x}?", dcRandom: "Random code" },
};
const pay = (k, v) => { let x = (PAY[lang] || PAY.en)[k] ?? PAY.en[k] ?? k; for (const [a, b] of Object.entries(v || {})) x = x.replaceAll(`{${a}}`, b); return x; };

function payCardCard() {
  return multiCardCard("/api/v2/shop/cards");
}

function discountsCard(products) {
  const list = h("div", { class: "form" });
  const fmtD = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "medium" }).format(new Date(u * 1000));
  const load = async () => {
    const ds = await api("GET", "/api/v2/admin/discounts").catch(() => []);
    const now = Date.now() / 1000;
    list.replaceChildren(...(ds.length ? ds.map((d) => {
      const expired = d.expires_unix && d.expires_unix <= now;
      const full = d.max_uses && d.used >= d.max_uses;
      return h("div", { class: "dc-row" },
        h("code", { class: "dc-code" }, d.code),
        h("b", {}, d.percent ? `${fmtNum(d.percent)}%` : money(d.amount)),
        h("span", { class: "small muted" }, d.max_uses ? pay("dcUsed", { u: fmtNum(d.used), m: fmtNum(d.max_uses) }) : pay("dcUsedInf", { u: fmtNum(d.used) })),
        d.product_id ? h("span", { class: "badge" }, d.product_id) : null,
        expired ? h("span", { class: "badge bad" }, pay("dcExpired")) : d.expires_unix ? h("span", { class: "badge" }, pay("dcExp", { d: fmtD(d.expires_unix) })) : null,
        full ? h("span", { class: "badge warn" }, lang === "fa" ? "ظرفیت تمام شد" : "used up") : null, !d.enabled ? h("span", { class: "badge" }, pay("dcOff")) : null,
        h("span", { class: "spacer" }),
        h("button", { type: "button", class: "ghost small", onclick: () => copy(d.code) }, t("copy")),
        h("button", { type: "button", class: "ghost danger small", onclick: async () => { if (await confirmAction(pay("dcDelAsk", { x: d.code }))) { await guarded(() => api("DELETE", `/api/v2/admin/discounts/${encodeURIComponent(d.code)}`)); load(); } } }, t("delete")));
    }) : [h("p", { class: "muted small" }, pay("dcNone"))]));
  };
  const code = h("input", { required: true, dir: "ltr", pattern: "[A-Za-z0-9_\\-]{3,32}", style: "text-transform:uppercase" });
  const rnd = h("button", { type: "button", class: "ghost", onclick: () => { code.value = Array.from(crypto.getRandomValues(new Uint8Array(6)), (b) => "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"[b % 32]).join(""); } }, pay("dcRandom"));
  const kind = h("select", {}, h("option", { value: "pct" }, pay("dcPct")), h("option", { value: "amt" }, pay("dcAmt")));
  const value = h("input", { type: "number", min: 1, required: true, value: 10 });
  const max = h("input", { type: "number", min: 0, value: 0 }), days = h("input", { type: "number", min: 0, value: 30 });
  const prod = h("select", {}, h("option", { value: "" }, pay("dcAny")), (products || []).filter((p) => !p.seller).map((p) => h("option", { value: p.id }, p.name)));
  const once = h("input", { type: "checkbox", checked: true });
  kind.addEventListener("change", () => { value.max = kind.value === "pct" ? 100 : ""; });
  value.max = 100;
  load();
  return h("div", { class: "card form" }, h("h2", {}, "🎟 ", pay("dcTitle")), h("p", { class: "small muted" }, pay("dcHint")), list,
    h("form", { class: "form dc-new", onsubmit: async (e) => {
      e.preventDefault();
      const v = +value.value;
      await guarded(() => api("PUT", "/api/v2/admin/discounts", { code: code.value, percent: kind.value === "pct" ? v : 0, amount: kind.value === "amt" ? v : 0,
        max_uses: +max.value, expires_unix: +days.value ? Math.floor(Date.now() / 1000) + +days.value * 86400 : 0, product_id: prod.value, once_per_buyer: once.checked, enabled: true }));
      toast(t("saved")); code.value = ""; load();
    } },
      h("div", { class: "two" }, h("label", {}, h("span", {}, pay("dcCode")), h("div", { class: "row tight" }, code, rnd)), h("label", {}, h("span", {}, pay("dcProduct")), prod)),
      h("div", { class: "three" }, h("label", {}, h("span", {}, pay("dcKind")), kind), h("label", {}, h("span", {}, pay("dcValue")), value), h("label", {}, h("span", {}, pay("dcMax")), max)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, pay("dcDays")), days), h("label", { class: "check" }, once, pay("dcOnce"))),
      h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, pay("dcAdd")))));
}

// The shop's products: grouped by category in the seller's order, with a
// category editor, a published switch, copy and up/down on each card.
// Owners pick whose shop they look at (their own or a reseller's).
let shopSeller = "";
function productsView(products, inbounds, reps, sold, canEdit) {
  const fa = lang === "fa";
  const owner = me.role === "owner";
  const own = owner ? "" : me.rep_id || "";
  if (!owner && shopSeller && !(reps || []).some((r) => r.ID === shopSeller)) shopSeller = "";
  const seller = shopSeller || own;
  const box = h("div", {});
  const mine = products.filter((p) => (p.seller || "") === seller);
  const sellerSel = (reps || []).length ? h("select", { onchange: (e) => { shopSeller = e.target.value; route(); } },
    h("option", { value: "" }, fa ? "فروشگاه خودم" : "My shop"), reps.map((r) => h("option", { value: r.ID, selected: r.ID === seller }, `${owner ? (fa ? "فروشگاه" : "Shop of") : (fa ? "زیرفروشنده" : "Sub-seller")} ${r.Name || r.ID}`))) : null;
  const toggle = async (p, on) => { await guarded(() => api("PUT", `/api/v2/shop/products/${encodeURIComponent(p.id)}`, { ...p, status: on ? "published" : "draft" })); route(); };
  const move = async (list, i, d) => {
    const j = i + d; if (j < 0 || j >= list.length) return;
    const order = list.slice(); [order[i], order[j]] = [order[j], order[i]];
    await guarded(() => api("PUT", "/api/v2/shop/products-order", { seller, ids: order.map((x) => x.id) }));
    route();
  };
  const restore = async (p) => { await guarded(() => api("PUT", `/api/v2/shop/products/${encodeURIComponent(p.id)}`, { ...p, status: "draft" })); toast(fa ? "برگشت (پیش‌نویس)" : "Restored as draft"); route(); };
  const hiddenN = mine.filter((p) => p.status === "archived").length;
  let showHidden = false;
  const card = (p, list, i) => {
    const on = p.status === "published";
    const sw = h("input", { type: "checkbox", role: "switch", class: "switch", checked: on, disabled: !canEdit || p.status === "archived", "aria-label": fa ? "فعال در ربات" : "On sale" });
    sw.addEventListener("change", () => toggle(p, sw.checked));
    return h("article", { class: `card price-tile ${p.status}` },
      h("div", { class: "row" }, sw, h("span", { class: "small muted" }, p.status === "archived" ? t("status_archived") : on ? (fa ? "در حال فروش" : "On sale") : (fa ? "پیش‌نویس" : "Draft")),
        h("span", { class: "spacer" }), canEdit && list.length > 1 && p.status !== "archived" ? h("span", { class: "row", style: "gap:2px" },
          h("button", { class: "ghost icon-only", title: fa ? "بالاتر" : "Up", disabled: i === 0, onclick: () => move(list, i, -1) }, "↑"),
          h("button", { class: "ghost icon-only", title: fa ? "پایین‌تر" : "Down", disabled: i === list.length - 1, onclick: () => move(list, i, 1) }, "↓")) : null),
      h("h3", {}, p.name),
      h("div", { class: "price" }, p.price ? money(p.price) : (fa ? "رایگان" : "Free"), p.price ? h("small", { class: "muted" }, fa ? " تومان" : " toman") : null, p.price_stars ? h("small", { class: "muted" }, ` · ⭐ ${fmtNum(p.price_stars)}`) : null),
      h("div", { class: "specs" },
        h("span", {}, p.plan === "unlimited" ? t("planUnlimited") : `${fmtNum(p.gb)} GB`),
        h("span", {}, fa ? `${fmtNum(p.days)} روز` : `${p.days} days`),
        p.max_ips ? h("span", {}, fa ? `${fmtNum(p.max_ips)} کاربره` : `${p.max_ips} devices`) : null,
        p.first_use ? h("span", {}, fa ? "از اولین اتصال" : "from first use") : null,
        p.no_renew ? h("span", {}, fa ? "فقط خرید جدید" : "new only") : null),
      h("div", { class: "row tile-foot" },
        h("span", { class: "small muted" }, fa ? `${fmtNum(sold[p.id] || 0)} فروش` : `${sold[p.id] || 0} sold`),
        h("span", { class: "spacer" }),
        canEdit && p.status === "archived" ? h("button", { class: "ghost", onclick: () => restore(p) }, fa ? "بازگرداندن" : "Restore") : null,
        canEdit && p.status !== "archived" ? h("button", { class: "ghost", onclick: () => productForm(p, inbounds, reps, { sold: sold[p.id] || 0 }) }, t("edit")) : null,
        canEdit ? h("button", { class: "ghost", onclick: () => productForm({ ...p, id: "", name: `${p.name} (${fa ? "کپی" : "copy"})`, status: "draft" }, inbounds, reps, { copy: true }) }, fa ? "کپی" : "Copy") : null,
        canEdit && p.status !== "archived" ? iconBtn("trash", fa ? "حذف" : "Delete", () => deleteProduct(p, sold[p.id] || 0)) : null));
  };
  function draw(cats) {
    const groups = [];
    const byCat = {};
    for (const p of mine) if (showHidden || p.status !== "archived") (byCat[p.category || ""] ||= []).push(p);
    const order = cats.map((c) => c.name).filter((n) => byCat[n]);
    for (const n of Object.keys(byCat)) if (n && !order.includes(n)) order.push(n);
    if (byCat[""]) order.push("");
    const emoji = Object.fromEntries(cats.map((c) => [c.name, c.emoji]));
    for (const n of order) {
      const list = byCat[n].filter((p) => p.status !== "archived").concat(byCat[n].filter((p) => p.status === "archived"));
      groups.push(h("section", { class: "shop-group" },
        h("h2", { class: "row" }, h("span", {}, `${emoji[n] || (n ? "📂" : "📦")} ${n || (fa ? "بدون دسته" : "No category")}`), h("span", { class: "badge" }, fmtNum(list.length))),
        h("div", { class: "price-grid" }, list.map((p, i) => card(p, list.filter((x) => x.status !== "archived"), i)))));
    }
    return [
      h("div", { class: "row shop-bar" }, sellerSel,
        h("div", { class: "chips" }, cats.map((c) => h("span", { class: "chip" }, `${c.emoji || "📂"} ${c.name}`, h("span", { class: "count" }, fmtNum(c.count))))),
        hiddenN ? h("button", { class: "ghost", "aria-pressed": String(showHidden), onclick: () => { showHidden = !showHidden; fill(box, ...draw(lastCats)); } }, fa ? `${showHidden ? "پنهان کردن" : "نمایش"} حذف‌شده‌ها (${fmtNum(hiddenN)})` : `${showHidden ? "Hide" : "Show"} removed (${hiddenN})`) : null,
        canEdit ? h("button", { class: "ghost spacer", onclick: () => categoriesForm(seller, cats) }, fa ? "مدیریت دسته‌ها" : "Categories") : null),
      groups.length ? groups : emptyState(fa ? "هنوز محصولی ندارید" : "No products yet", fa ? "یک بسته (حجم، مدت و قیمت) بسازید تا در ربات و فروشگاه فروخته شود." : "Create a plan (volume, days and price) to sell it in the bot and the store.",
        canEdit ? `${t("add")} · ${t("product")}` : "", () => productForm(null, inbounds, reps, { seller }))];
  }
  let lastCats = [];
  const load = () => api("GET", `/api/v2/shop/categories${owner || seller !== own ? `?seller=${encodeURIComponent(seller)}` : ""}`).then((cats) => { lastCats = cats || []; fill(box, ...draw(lastCats)); }).catch(() => fill(box, ...draw([])));
  fill(box, ...draw([]));
  load();
  return box;
}

// deleteProduct: removes a never-sold product; a sold one is hidden (restorable).
async function deleteProduct(p, sold) {
  const fa = lang === "fa";
  const msg = sold ? (fa ? `«${p.name}» ${fmtNum(sold)} بار فروخته شده؛ برای حفظ تاریخچهٔ سفارش‌ها از فروشگاه حذف و پنهان می‌شود (قابل بازگرداندن). ادامه؟` : `"${p.name}" was sold ${sold} times; it is removed from the shop and hidden (restorable). Continue?`)
    : (fa ? `محصول «${p.name}» برای همیشه حذف شود؟` : `Delete "${p.name}" for good?`);
  if (!(await confirmAction(msg))) return false;
  let r;
  try { r = await api("DELETE", `/api/v2/shop/products/${encodeURIComponent(p.id)}`); } catch (e) { toast(t("failed", { e: e.message })); return false; }
  toast(r.deleted ? (fa ? "حذف شد" : "Deleted") : (fa ? "از فروشگاه حذف شد" : "Removed from the shop"));
  route();
  return true;
}

// categoriesForm: rename, emoji, order and remove the shop's categories.
function categoriesForm(seller, cats) {
  const fa = lang === "fa";
  const rows = cats.map((c) => ({ name: c.name, emoji: c.emoji || "", old: c.name, count: c.count }));
  const list = h("div", { class: "form" });
  const draw = () => fill(list, rows.length ? rows.map((r, i) => {
    const em = h("input", { value: r.emoji, maxlength: "4", placeholder: "📂", style: "inline-size:56px;text-align:center" });
    const nm = h("input", { value: r.name, maxlength: "40" });
    em.addEventListener("input", () => { r.emoji = em.value.trim(); });
    nm.addEventListener("input", () => { r.name = nm.value.trim(); });
    return h("div", { class: "row nowrap cat-row" }, em, nm,
      h("span", { class: "muted small" }, fa ? `${fmtNum(r.count || 0)} محصول` : `${r.count || 0}`),
      h("button", { type: "button", class: "ghost icon-only", disabled: i === 0, onclick: () => { [rows[i - 1], rows[i]] = [rows[i], rows[i - 1]]; draw(); } }, "↑"),
      h("button", { type: "button", class: "ghost icon-only", disabled: i === rows.length - 1, onclick: () => { [rows[i + 1], rows[i]] = [rows[i], rows[i + 1]]; draw(); } }, "↓"),
      h("button", { type: "button", class: "ghost icon-only danger", title: fa ? "حذف دسته (محصولاتش بدون دسته می‌شوند)" : "Remove (its products become uncategorised)", onclick: () => { rows.splice(i, 1); draw(); } }, "✕"));
  }) : [h("p", { class: "muted small" }, fa ? "هنوز دسته‌ای ندارید." : "No categories yet.")]);
  const add = h("button", { type: "button", class: "ghost", onclick: () => { rows.push({ name: fa ? "دستهٔ جدید" : "New category", emoji: "", count: 0 }); draw(); } }, fa ? "+ دستهٔ جدید" : "+ New category");
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", type: "button", onclick: async () => {
    err.textContent = "";
    try {
      await api("PUT", "/api/v2/shop/categories", { seller, categories: rows.map((r) => ({ name: r.name, emoji: r.emoji, old: r.old || "" })) });
      document.getElementById("modal").close(); toast(t("saved")); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  draw();
  openModal(fa ? "دسته‌بندی فروشگاه" : "Shop categories", h("div", { class: "form" },
    h("p", { class: "small muted" }, fa ? "ترتیب این‌جا همان ترتیب دسته‌ها در ربات است. تغییر نام روی همهٔ محصولات آن دسته اعمال می‌شود؛ حذف دسته محصولاتش را پاک نمی‌کند، فقط بدون دسته می‌شوند." : "This order is the order in the bot. A rename applies to all its products; removing a category keeps its products, uncategorised."),
    list, add, err), [save]);
}

// productForm: name, category, volume, term, devices and price; the ID is
// made automatically. A live line shows the button customers see in the bot.
function productForm(p, inbounds, reps, opt = {}) {
  const fa = lang === "fa";
  const owner = me.role === "owner";
  const T = (f, e) => (fa ? f : e);
  const isNew = !p || opt.copy;
  const v = p || { id: "", name: "", plan: "volume", gb: 30, days: 30, max_ips: 2, inbounds: [], price: 0, status: "published", first_use: true, seller: opt.seller || "", category: "" };
  const newID = () => `p${Date.now().toString(36)}${Math.floor(Math.random() * 1296).toString(36)}`;
  const id = isNew ? newID() : v.id;
  const name = h("input", { required: true, maxlength: "120", value: v.name, placeholder: T("مثلاً: ۳۰ گیگ یک‌ماهه", "e.g. 30 GB monthly"), autofocus: isNew });
  const canUnl = !(me.role === "rep" && me.policy && !me.policy.AllowUnlimited);
  let plan = v.plan === "unlimited" && canUnl ? "unlimited" : "volume";
  const planSeg = h("div", { class: "seg" });
  const gb = h("input", { type: "number", min: "1", value: v.gb || 30, dir: "ltr" });
  const days = h("input", { type: "number", min: "1", max: "3650", value: v.days || 30, dir: "ltr" });
  const ips = h("input", { type: "number", min: "0", max: "100", value: v.max_ips, dir: "ltr" });
  const price = h("input", { type: "number", min: "0", step: "1000", value: v.price || "", dir: "ltr", placeholder: "150000" });
  const priceTxt = h("span", { class: "small muted" });
  const stars = h("input", { type: "number", min: "0", max: "100000", step: "1", value: v.price_stars || 0, dir: "ltr" });
  const first = h("input", { type: "checkbox", checked: v.first_use });
  const onSale = h("input", { type: "checkbox", checked: v.status !== "draft" && v.status !== "archived" });
  const renewable = h("input", { type: "checkbox", checked: !v.no_renew });
  const perBuyer = h("input", { type: "number", min: "0", max: "1000", value: v.per_buyer || 0, dir: "ltr" });
  const seller = owner || (reps || []).length ? h("select", {}, h("option", { value: owner ? "" : me.rep_id }, owner ? t("ownerSelf") : T("فروشگاه خودم", "My shop")), (reps || []).map((r) => h("option", { value: r.ID, selected: (v.seller || "") === r.ID }, owner ? r.Name || r.ID : `${T("زیرفروشنده", "Sub-seller")} ${r.Name || r.ID}`))) : null;
  const catSel = h("select", {}, h("option", { value: "" }, T("بدون دسته", "No category")));
  const catNew = h("input", { maxlength: "40", placeholder: T("نام دستهٔ جدید", "New category name"), hidden: true });
  const loadCats = (sel) => api("GET", `/api/v2/shop/categories${owner || sel ? `?seller=${encodeURIComponent(sel || "")}` : ""}`).then((cats) => {
    catSel.replaceChildren(h("option", { value: "" }, T("بدون دسته", "No category")),
      ...(cats || []).map((c) => h("option", { value: c.name, selected: c.name === v.category }, `${c.emoji || "📂"} ${c.name}`)),
      h("option", { value: "+" }, T("➕ دستهٔ جدید…", "➕ New category…")));
    if (v.category && !(cats || []).some((c) => c.name === v.category)) catSel.prepend(h("option", { value: v.category, selected: true }, v.category));
  }).catch(() => {});
  loadCats(v.seller);
  if (seller) seller.addEventListener("change", () => loadCats(seller.value));
  catSel.addEventListener("change", () => { catNew.hidden = catSel.value !== "+"; if (!catNew.hidden) catNew.focus(); });
  const ibs = (inbounds || []).filter((ib) => !["socks", "http", "dokodemo-door", "tunnel", "mtproto"].includes(ib.protocol));
  const picked = (v.inbounds || []).filter((x) => ibs.some((ib) => ib.tag === x));
  const allIb = !picked.length || ibs.every((ib) => picked.includes(ib.tag));
  const allChip = h("button", { type: "button", class: "chip", "aria-pressed": String(allIb) }, T("همه (سرورهای بعدی هم)", "All (new ones too)"));
  const ibChips = ibs.map((ib) => {
    const b = h("button", { type: "button", class: "chip", "aria-pressed": String(!allIb && picked.includes(ib.tag)), "data-tag": ib.tag, dir: "ltr" }, ib.tag);
    b.addEventListener("click", () => {
      b.setAttribute("aria-pressed", String(b.getAttribute("aria-pressed") !== "true"));
      const any = ibChips.some((x) => x.getAttribute("aria-pressed") === "true");
      allChip.setAttribute("aria-pressed", String(!any));
    });
    return b;
  });
  allChip.addEventListener("click", () => { allChip.setAttribute("aria-pressed", "true"); ibChips.forEach((x) => x.setAttribute("aria-pressed", "false")); });
  const chipRow = (inp, vals, fmt) => h("div", { class: "chips qp" }, vals.map((x) => h("button", { type: "button", class: "chip", onclick: () => { inp.value = x; calc(); } }, fmt(x))));
  const gbBox = h("div", {}, h("label", {}, h("span", {}, T("حجم (گیگ)", "Volume (GB)")), gb), chipRow(gb, [10, 20, 30, 50, 100, 200], (x) => `${fmtNum(x)} GB`));
  const preview = h("div", { class: "csum" });
  const calc = () => {
    gbBox.hidden = plan === "unlimited";
    const pr = Math.round(+price.value || 0);
    priceTxt.textContent = pr ? `${fmtNum(pr)} ${T("تومان", "toman")}` : T("رایگان", "free");
    const size = plan === "unlimited" ? T("نامحدود", "unlimited") : `${fmtNum(+gb.value || 0)} ${T("گیگ", "GB")}`;
    const cat = catSel.value === "+" ? catNew.value.trim() : catSel.value;
    preview.replaceChildren(
      h("div", { class: "small muted" }, T("در ربات این‌طور دیده می‌شود:", "Customers see in the bot:")),
      h("div", { class: "bot-btn" }, `📦 ${name.value || T("نام محصول", "Product name")} — ${pr ? `${fmtNum(pr)} ${T("تومان", "toman")}` : T("رایگان", "free")}`),
      h("div", { class: "small" }, [cat ? `📂 ${cat}` : null, size, T(`${fmtNum(+days.value || 0)} روز`, `${days.value} days`), +ips.value ? T(`${fmtNum(+ips.value)} کاربره`, `${ips.value} devices`) : null,
        first.checked ? T("شروع از اولین اتصال", "starts on first use") : null, onSale.checked ? T("در حال فروش", "on sale") : T("پیش‌نویس (در ربات دیده نمی‌شود)", "draft (hidden in the bot)")].filter(Boolean).join(" · ")));
  };
  for (const [k, l] of [["volume", T("حجمی", "Volume")], canUnl ? ["unlimited", T("نامحدود", "Unlimited")] : null].filter(Boolean)) {
    planSeg.append(h("button", { type: "button", role: "radio", "aria-checked": String(plan === k), onclick: (e) => { plan = k; for (const x of planSeg.children) x.setAttribute("aria-checked", String(x === e.currentTarget)); calc(); } }, l));
  }
  [name, gb, days, ips, price, catNew].forEach((x) => x.addEventListener("input", calc));
  [first, onSale, catSel].forEach((x) => x.addEventListener("change", calc));
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    if (!name.reportValidity()) return;
    const cat = catSel.value === "+" ? catNew.value.trim() : catSel.value;
    const tags = ibChips.filter((b) => b.getAttribute("aria-pressed") === "true").map((b) => b.dataset.tag);
    try {
      await api("PUT", `/api/v2/shop/products/${encodeURIComponent(id)}`, {
        name: name.value.trim(), plan, gb: plan === "unlimited" ? 0 : +gb.value || 0, days: +days.value, max_ips: +ips.value || 0, price: Math.round(+price.value || 0), price_stars: Math.round(+stars.value || 0),
        status: onSale.checked ? "published" : "draft", first_use: first.checked, seller: seller ? seller.value : "",
        category: cat, no_renew: !renewable.checked, per_buyer: +perBuyer.value || 0, sort: v.sort || 0,
        inbounds: allChip.getAttribute("aria-pressed") === "true" ? [] : tags });
      document.getElementById("modal").close("save"); toast(t("saved")); route();
    } catch (x) { err.textContent = x.message; }
  } }, isNew ? T("ساخت محصول", "Create product") : t("save"));
  calc();
  const d = openModal(isNew ? T("محصول جدید", "New product") : `${t("edit")} · ${v.name}`, h("div", { class: "form cform" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, T("نام محصول", "Product name")), name), h("label", {}, h("span", {}, T("دسته", "Category")), catSel, catNew)),
    h("div", {}, h("span", { class: "lbl" }, T("نوع", "Type")), planSeg),
    h("div", { class: "two" }, gbBox,
      h("div", {}, h("label", {}, h("span", {}, T("مدت (روز)", "Days")), days), chipRow(days, [30, 60, 90, 180, 365], (x) => (x === 365 ? T("۱ سال", "1 yr") : T(`${fmtNum(x / 30)} ماه`, `${x / 30} mo`))))),
    h("div", { class: "two" },
      h("div", {}, h("label", {}, h("span", {}, T("تعداد کاربر (دستگاه هم‌زمان)", "Users (devices at once)")), ips), chipRow(ips, [1, 2, 3, 5], (x) => T(`${fmtNum(x)} کاربره`, `${x}`))),
      h("label", {}, h("span", {}, T("قیمت (تومان)", "Price (toman)")), price, priceTxt)),
    h("label", { class: "check" }, onSale, T("در حال فروش (در ربات و فروشگاه نمایش داده شود)", "On sale (shown in the bot and store)")),
    h("label", { class: "check" }, first, T("زمان از اولین اتصال مشتری شروع شود", "Time starts at the customer's first connection")),
    h("label", { class: "check" }, renewable, T("برای تمدید هم نمایش داده شود", "Also offered for renewals")),
    h("details", { class: "adv" }, h("summary", {}, T("تنظیمات پیشرفته", "Advanced")), h("div", { class: "form" },
      h("label", {}, h("span", {}, T("سقف خرید هر مشتری (۰ = آزاد)", "Per-customer limit (0 = none)")), perBuyer),
      owner ? h("label", {}, h("span", {}, t("priceStars")), stars) : null,
      seller ? h("label", {}, h("span", {}, t("seller")), seller) : null,
      ibs.length > 1 ? h("div", {}, h("span", { class: "lbl" }, T("اینباندها", "Inbounds")), h("div", { class: "chips" }, allChip, ibChips)) : null,
      h("p", { class: "small muted", dir: "ltr" }, `id: ${id}`))),
    preview, err), [!isNew ? h("button", { class: "ghost danger", value: "del", onclick: async (e) => { e.preventDefault(); document.getElementById("modal").close(); await deleteProduct(v, opt.sold || 0); } }, T("حذف محصول", "Delete")) : null, save].filter(Boolean));
  d.classList.add("wide");
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
    salesReportCard(),
    h("div", { class: "row" }, h("h1", {}, t("finance")), h("label", { class: "spacer inline" }, h("span", { class: "small muted" }, t("period")), period)),
    h("div", { class: "grid" }, stat("sold", bytes(sum("SoldBytes"))), stat("revenue", money(sum("Revenue"))), stat("paid", money(sum("Paid"))),
      stat("debt", money(sum("Debt")), sum("Debt") > 0 ? "warn" : "")),
    rows.length ? table : h("p", { class: "muted" }, t("empty")));
}

// salesReportCard: resellers' sales in a period next to the period before,
// new services and renewals apart, the shop's toman orders, a daily chart
// and a CSV for Excel.
function salesReportCard() {
  const fa = lang === "fa";
  const box = h("section", { class: "card sales-report" });
  let days = +(sessionStorage.getItem("salesDays") || 30);
  const gbs = (b) => `${fmtNum(Math.round(b / 2 ** 30))} ${fa ? "گیگ" : "GB"}`;
  const delta = (cur, prev) => {
    if (!prev) return cur ? h("span", { class: "chg up" }, fa ? "جدید" : "new") : h("span", { class: "chg" }, "—");
    const p = Math.round(((cur - prev) / prev) * 100);
    return h("span", { class: `chg ${p > 0 ? "up" : p < 0 ? "down" : ""}`, dir: "ltr" }, `${p > 0 ? "+" : ""}${fmtNum(p)}%`);
  };
  const csv = (r) => {
    const head = ["reseller", "parent", "new", "renewals", "sold_gb", "refund_gb", "net_gb", "prev_net_gb", "revenue", "shop_orders", "shop_toman", "clients"];
    const q = (v) => `"${String(v ?? "").replace(/"/g, '""')}"`;
    const lines = [head.join(",")].concat(r.reps.map((x) => [x.name || x.rep_id, x.parent, x.new, x.renew, (x.sold_bytes / 2 ** 30).toFixed(1), (x.refund_bytes / 2 ** 30).toFixed(1),
      (x.net_bytes / 2 ** 30).toFixed(1), (x.prev_net_bytes / 2 ** 30).toFixed(1), x.revenue, x.shop_orders, x.shop_toman, x.clients].map(q).join(",")));
    const blob = new Blob(["\ufeff" + lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const a = h("a", { href: URL.createObjectURL(blob), download: `sales-${days}d-${new Date().toISOString().slice(0, 10)}.csv` });
    document.body.append(a); a.click(); a.remove();
  };
  const chart = (daily) => daily.length ? chartKit({ xs: daily.map((d) => d.day), mode: "stack", height: 200, label: fa ? "فروش روزانه" : "Daily sales",
    series: [{ id: "s", name: fa ? "حجم فروخته‌شده" : "Volume sold", color: "var(--ion)", values: daily.map((d) => d.bytes) }],
    fmt: gbs, partial: daily.at(-1).day === todayUTC() ? daily.length - 1 : -1,
    tipExtra: (i) => h("div", { class: "ct-row tot" }, h("span", {}, fa ? "تعداد فروش" : "Orders"), h("span", { class: "v" }, fmtNum(daily[i].count || 0))) }) : null;
  const load = async () => {
    fill(box, h("p", { class: "muted small" }, fa ? "در حال آماده‌سازی گزارش…" : "Loading report…"));
    let r;
    try { r = await api("GET", `/api/v2/admin/reports/sales?days=${days}`); } catch (e) { fill(box, h("p", { class: "error" }, e.message)); return; }
    const reps = r.reps.filter((x) => x.new || x.renew || x.sold_bytes || x.prev_net_bytes || x.shop_orders).sort((a, b) => b.net_bytes - a.net_bytes);
    const tot = (k) => r.reps.reduce((a, x) => a + (x[k] || 0), 0);
    const chips = h("div", { class: "chips", role: "tablist" }, [[1, fa ? "امروز" : "Today"], [7, fa ? "۷ روز" : "7 days"], [30, fa ? "۳۰ روز" : "30 days"], [90, fa ? "۹۰ روز" : "90 days"]].map(([d, l]) =>
      h("button", { type: "button", role: "tab", class: `chip ${d === days ? "on" : ""}`, "aria-selected": String(d === days), onclick: () => { days = d; try { sessionStorage.setItem("salesDays", d); } catch {} load(); } }, l)));
    const kpi = (v, l, extra) => h("div", { class: "card stat" }, h("div", { class: "v" }, v, " ", extra || null), h("div", { class: "k" }, l));
    fill(box,
      h("div", { class: "row" }, h("h2", {}, fa ? "گزارش فروش نماینده‌ها" : "Reseller sales"), h("span", { class: "spacer" }), chips,
        h("button", { class: "ghost", onclick: () => csv(r) }, fa ? "خروجی CSV" : "Export CSV"),
        reportXlsxLinks()),
      h("div", { class: "grid" },
        kpi(gbs(tot("net_bytes")), fa ? "فروش خالص" : "Net sold", delta(tot("net_bytes"), tot("prev_net_bytes"))),
        kpi(fmtNum(tot("new")), fa ? "سرویس جدید" : "New services"),
        kpi(fmtNum(tot("renew")), fa ? "تمدید" : "Renewals"),
        kpi(money(tot("shop_toman") + (r.owner_shop_toman || 0)), fa ? "فروش تومانی فروشگاه" : "Shop sales (toman)")),
      chart(r.daily),
      reps.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
        h("thead", {}, h("tr", {}, [fa ? "نماینده" : "Reseller", fa ? "جدید" : "New", fa ? "تمدید" : "Renew", fa ? "فروش" : "Sold", fa ? "برگشتی" : "Refund", fa ? "خالص" : "Net", fa ? "نسبت به دورهٔ قبل" : "vs before", fa ? "درآمد" : "Revenue", fa ? "فروشگاه" : "Shop"].map((x) => h("th", { scope: "col" }, x)))),
        h("tbody", {}, reps.map((x) => h("tr", {},
          h("th", { scope: "row" }, h("a", { href: `#/activity?rep=${encodeURIComponent(x.rep_id)}` }, x.name || x.rep_id), x.parent ? h("div", { class: "small muted" }, `${fa ? "زیرِ" : "under"} ${x.parent}`) : null),
          h("td", {}, fmtNum(x.new)), h("td", {}, fmtNum(x.renew)), h("td", {}, gbs(x.sold_bytes)), h("td", { class: "muted" }, gbs(x.refund_bytes)),
          h("td", {}, h("b", {}, gbs(x.net_bytes))), h("td", {}, delta(x.net_bytes, x.prev_net_bytes)), h("td", {}, money(x.revenue)),
          h("td", {}, x.shop_orders ? `${fmtNum(x.shop_orders)} · ${money(x.shop_toman)}` : "—")))))) :
        h("p", { class: "muted" }, fa ? "در این بازه فروشی ثبت نشده." : "No sales in this period."));
  };
  load();
  return box;
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

// ---- Resellers: dashboard, credit history, renewals -------------------

// A small two-series chart for reseller days: a = ion line, b = ember line.
function repChart(days, a, b, la, lb) {
  return barsSvg(days.map((d) => ({ Day: d.day, Down: d[a] || 0, Up: b ? d[b] || 0 : 0 })), 1100, { single: !b, la, lb });
}

// Credit reads best in whole GB (resellers count in GB, not TB).
const gbs = (b) => `\u2066${fmtNum(Math.round((b || 0) / 2 ** 30))} GB\u2069`;

function repLegend(a, b) {
  return h("span", { class: "spacer legend" }, h("i", { class: "sw down" }), a, b ? h("i", { class: "sw up" }) : null, b || "");
}

const fmtWhen = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));

function ledgerDelta(e) {
  return [e.VolumeDelta ? `${e.VolumeDelta > 0 ? "+" : "−"}${bytes(Math.abs(e.VolumeDelta))}` : "",
    e.UnitsDelta ? `${e.UnitsDelta > 0 ? "+" : "−"}${fmtNum(Math.abs(e.UnitsDelta))} ${t("units")}` : "",
    e.Amount ? `${t("amount")} ${money(e.Amount)}` : ""].filter(Boolean).join(" · ");
}

// What a ledger row means, in plain words.
function ledgerKind(e) {
  const fa = lang === "fa";
  if (e.Kind === "refund" && /expired/.test(e.Note || "")) return [fa ? "برگشت انقضا" : "Expired, returned", "good"];
  if (e.Kind === "refund") return [fa ? "برگشت حذف" : "Returned on delete", "good"];
  if (e.Kind === "charge") return [fa ? "فروش" : "Sale", ""];
  if (e.Kind === "adjust" && (e.Actor === "import" || e.Actor === "old panel")) return [fa ? "پنل قبلی" : "Old panel", ""];
  if (e.Kind === "adjust") return [e.VolumeDelta < 0 || e.UnitsDelta < 0 ? (fa ? "کسر مالک" : "Owner removed") : (fa ? "شارژ مالک" : "Owner top-up"), e.VolumeDelta < 0 ? "warn" : "good"];
  return [t(`kind_${e.Kind}`), e.Kind === "delete" ? "bad" : e.Kind === "transfer" ? "warn" : ""];
}

// #/dashboard for a reseller, #/repview?rep=<id> for the owner.
async function viewRepDashboard() {
  const fa = lang === "fa";
  const owner = me.role === "owner";
  const rep = new URLSearchParams(location.hash.split("?")[1] || "").get("rep") || "";
  const o = await api("GET", `/api/v2/rep/overview${rep ? `?rep=${encodeURIComponent(rep)}` : ""}`);
  const [allCls, reps, inbounds, alert] = await Promise.all([
    api("GET", "/api/v2/admin/clients").catch(() => []),
    owner ? api("GET", "/api/v2/admin/reps").catch(() => []) : Promise.resolve([]),
    owner ? api("GET", "/api/v2/admin/inbounds").catch(() => []) : Promise.resolve([]),
    api("GET", owner ? `/api/v2/admin/reps/${encodeURIComponent(o.rep_id)}/alert` : "/api/v2/rep/alert").catch(() => ({ low_gb: 50 }))]);
  const cls = (allCls || []).filter((c) => c.RepID === o.rep_id);
  const r = (reps || []).find((x) => x.ID === o.rep_id);
  // Low-credit warning threshold (GB, 0 = off).
  const lowIn = h("input", { type: "number", min: 0, max: 1000000, step: 10, value: alert.low_gb ?? 50, dir: "ltr", style: "max-inline-size:140px" });
  const alertCard = h("div", { class: "card form" }, h("h2", {}, fa ? "هشدار کم شدن اعتبار" : "Low-credit warning"),
    h("p", { class: "small muted" }, owner
      ? (fa ? "وقتی اعتبار این نماینده از این مقدار کمتر شد، به خودش (در رباتش) و به شما در تلگرام خبر داده می‌شود. ۰ یعنی خاموش. هر روز ساعت ۹ صبح هم خلاصهٔ روزش برای نماینده فرستاده می‌شود." : "When this reseller's credit drops under this, they (in their bot) and you are told on Telegram. 0 = off. They also get a daily summary at 9:00.")
      : (fa ? "وقتی اعتبارتان از این مقدار کمتر شد، ربات خبرتان می‌کند. ۰ یعنی خاموش. هر روز ساعت ۹ صبح هم خلاصهٔ فروش و کلاینت‌ها برایتان فرستاده می‌شود." : "Your bot tells you when credit drops under this. 0 = off. You also get a daily summary at 9:00.")),
    h("div", { class: "row" }, lowIn, h("span", {}, fa ? "گیگ" : "GB"), h("button", { onclick: async () => {
      await guarded(() => api("PUT", owner ? `/api/v2/admin/reps/${encodeURIComponent(o.rep_id)}/alert` : "/api/v2/rep/alert", { low_gb: +lowIn.value || 0 }));
      toast(fa ? "ذخیره شد" : "Saved");
    } }, t("save"))));
  // Owner: this reseller's clients, those needing attention first.
  const nowS = Date.now() / 1000;
  const score = (c) => !c.Enabled ? 3 : clientStatus(c)[0] === "bad" ? 2 : (c.EffectiveUntil && c.EffectiveUntil - nowS <= 3 * 86400) || (c.VolumeBytes > 0 && c.Remaining * 10 <= c.VolumeBytes) ? 0 : 1;
  const clientsCard = owner ? h("div", { class: "card form span2" }, h("div", { class: "row" }, h("h2", {}, fa ? "کلاینت‌های این نماینده" : "This reseller's clients"), h("span", { class: "badge" }, fmtNum(cls.length))),
    cls.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
      h("thead", {}, h("tr", {}, ...[fa ? "کلاینت" : "Client", fa ? "وضعیت" : "State", fa ? "مصرف" : "Used", fa ? "انقضا" : "Expires"].map((x) => h("th", {}, x)))),
      h("tbody", {}, ...[...cls].sort((a, b) => score(a) - score(b)).slice(0, 40).map((c) => {
        const [k, st] = clientStatus(c);
        const left = c.EffectiveUntil ? Math.ceil((c.EffectiveUntil - nowS) / 86400) : null;
        return h("tr", {}, h("td", {}, h("span", { class: `dot ${c.Online ? "on" : ""}` }), " ", h("a", { href: `#/client?e=${encodeURIComponent(c.Email)}` }, c.Email)),
          h("td", {}, h("span", { class: `badge ${k}` }, st)),
          h("td", { dir: "ltr", class: "small" }, `${bytes(c.Usage.Up + c.Usage.Down)} / ${c.VolumeBytes < 0 ? "∞" : bytes(c.VolumeBytes)}`),
          h("td", { class: "small" }, left == null ? (c.DurationSeconds ? (fa ? "از اولین اتصال" : "on first use") : "∞") : left <= 0 ? (fa ? "تمام شده" : "ended") : fa ? `${fmtNum(left)} روز` : `${left} d`));
      })))) : h("p", { class: "muted small" }, t("empty")),
    cls.length > 40 ? h("p", { class: "small muted" }, fa ? "۴۰ مورد اول (اول رو به اتمام‌ها) — بقیه در صفحهٔ کلاینت‌ها با فیلتر نماینده." : "First 40 (ending soon first) — the rest are on the Clients page with the reseller filter.") : null) : null;
  const ownerActions = owner && r ? h("div", { class: "row tight" },
    h("button", { class: "primary", onclick: () => creditForm(r) }, t("addCredit")),
    h("button", { class: "ghost", onclick: () => policyForm(r, inbounds) }, t("policy")),
    h("button", { class: "ghost", onclick: () => paymentForm(r) }, t("payment")),
    h("button", { class: "ghost", onclick: () => openModal(`${fa ? "ربات و کارت" : "Bot & card"} · ${r.Name || r.ID}`, h("div", { class: "form" }, repBotCard(`/api/v2/admin/reps/${encodeURIComponent(r.ID)}/bot`), sellerCardCard(`/api/v2/shop/card?seller=${encodeURIComponent(r.ID)}`))) }, fa ? "ربات و کارت" : "Bot & card"),
    h("button", { class: "ghost", onclick: () => openModal(`${fa ? "برند و صفحهٔ اشتراک" : "Brand & subscription page"} · ${r.Name || r.ID}`, repBrandCard(`/api/v2/admin/reps/${encodeURIComponent(r.ID)}/brand`)) }, fa ? "صفحهٔ اشتراک" : "Sub page"),
    h("button", { class: "ghost", onclick: () => repForm(r) }, t("edit"))) : null;
  const kpi = (ic, k, v, href) => h(href ? "a" : "div", { class: "card kpi", href }, h("div", { class: "k" }, icon(ic), k), h("div", { class: "v" }, v));
  const chip = (k, v, c) => h("span", { class: `badge ${c || ""}` }, `${k}: `, h("b", { dir: "ltr" }, v));
  const hero = h("div", { class: "card credit-hero" },
    h("div", {},
      h("div", { class: "k" }, fa ? "اعتبار حجمی باقی‌مانده" : "Volume credit left"),
      h("div", { class: "big", dir: "ltr" }, gbs(o.volume_credit)),
      o.unlimited_units ? h("div", { class: "small muted" }, `${t("units")}: ${fmtNum(o.unlimited_units)}`) : null),
    h("div", { class: "credit-month" },
      h("div", { class: "small muted" }, fa ? "این ماه" : "This month"),
      h("div", { class: "row tight" },
        chip(fa ? "فروش" : "Sold", gbs(o.month_sold)),
        chip(fa ? "تعداد فروش" : "Sales", fmtNum(o.month_sales)),
        chip(fa ? "برگشتی" : "Returned", gbs(o.month_back), "good"),
        o.month_topup ? chip(fa ? "شارژ مالک" : "Top-ups", gbs(o.month_topup), "good") : null)),
    h("div", { class: "row tight credit-actions" },
      h("a", { class: "btn", href: owner ? `#/activity?rep=${encodeURIComponent(o.rep_id)}` : "#/credit" }, fa ? "تاریخچهٔ اعتبار" : "Credit history"),
      owner ? null : h("a", { class: "btn primary", href: "#/clients" }, fa ? "کلاینت‌ها" : "Clients")));
  const recent = h("div", { class: "card form" }, h("div", { class: "row" }, h("h2", {}, fa ? "آخرین تراکنش‌ها" : "Latest activity"),
    h("a", { class: "spacer small", href: owner ? `#/activity?rep=${encodeURIComponent(o.rep_id)}` : "#/credit" }, fa ? "همه ›" : "All ›")),
    (o.recent || []).length ? h("div", { class: "form" }, o.recent.map((e) => {
      const [k, c] = ledgerKind(e);
      return h("div", { class: "row small" }, h("span", { class: `badge ${c}` }, k), e.ClientEmail ? h("span", {}, e.ClientEmail) : null,
        h("span", { class: "mono", dir: "ltr" }, ledgerDelta(e)), h("span", { class: "muted spacer" }, fmtWhen(e.Unix)));
    })) : h("p", { class: "muted small" }, t("empty")));
  return h("div", {},
    h("div", { class: "row" }, owner ? h("a", { class: "btn ghost back", href: "#/reps" }, fa ? "›" : "‹") : null,
      h("h1", {}, owner ? o.name || o.rep_id : t("dashboard")),
      o.last_login ? h("span", { class: "muted small spacer" }, `${fa ? "آخرین ورود" : "Last sign-in"}: ${fmtWhen(o.last_login)}`) : null),
    ownerActions,
    hero,
    h("div", { class: "kpis" },
      kpi("clients", fa ? "کلاینت فعال" : "Active", h("span", { dir: "ltr" }, fmtNum(o.active), h("small", {}, ` / ${fmtNum(o.clients)}`)), owner ? null : "#/clients"),
      kpi("activity", fa ? "رو به اتمام" : "Ending soon", fmtNum(o.expiring), owner ? null : "#/clients"),
      kpi("power", fa ? "تمام‌شده" : "Ended", fmtNum(o.ended), owner ? null : "#/clients"),
      kpi("online", fa ? "آنلاین" : "Online", fmtNum(o.online), owner ? null : "#/online"),
      kpi("traffic", fa ? "مصرف کل کاربران" : "Users' total traffic", bytes(o.used_bytes)),
      movedHere(o.used_bytes, o.used_here) ? kpi("traffic", fa ? "مصرف در این پنل" : "Traffic on this panel", bytes(o.used_here || 0)) : null),
    h("div", { class: "dash2" },
      h("div", { class: "card form span2" }, h("div", { class: "row" }, h("h2", {}, fa ? "فروش و برگشتی ۳۰ روز" : "Sold and returned, 30 days"),
        repLegend(fa ? "فروش" : "Sold", fa ? "برگشتی" : "Returned")), repChart(o.days || [], "sold", "back", fa ? "فروش" : "Sold", fa ? "برگشتی" : "Returned")),
      h("div", { class: "card form span2" }, h("div", { class: "row" }, h("h2", {}, fa ? "مصرف کاربران ۳۰ روز" : "Users' traffic, 30 days"),
        repLegend(fa ? "مصرف" : "Traffic")), repChart(o.days || [], "traffic", null, fa ? "مصرف" : "Traffic")),
      owner ? clientsCard : endingSoonCard(cls || []),
      recent,
      alertCard));
}

// #/credit: a reseller's own credit history with the balance after each row.
async function viewCredit() {
  const fa = lang === "fa";
  const o = await api("GET", "/api/v2/rep/overview");
  let all = [];
  let before = 0;
  for (let i = 0; i < 10; i++) { // up to 1000 rows
    const page = await api("GET", `/api/v2/rep/ledger${before ? `?before=${before}` : ""}`);
    all = all.concat(page || []);
    if (!page || page.length < 100) break;
    before = page.at(-1).ID;
  }
  // Balance after each row: walk back from today's balance.
  let bal = o.volume_credit;
  for (const e of all) { e._bal = bal; bal -= e.VolumeDelta || 0; }
  const kinds = [["", fa ? "همه" : "All"], ["sale", fa ? "فروش" : "Sales"], ["back", fa ? "برگشتی" : "Returned"], ["owner", fa ? "شارژ مالک" : "Owner"], ["other", fa ? "سایر" : "Other"]];
  const group = (e) => e.Kind === "charge" ? "sale" : e.Kind === "refund" ? "back" : e.Kind === "adjust" && e.Actor !== "import" && e.Actor !== "old panel" ? "owner" : "other";
  let f = "";
  const body = h("tbody", {});
  const draw = () => body.replaceChildren(...all.filter((e) => !f || group(e) === f).map((e) => {
    const [k, c] = ledgerKind(e);
    return h("tr", {}, h("td", { class: "small" }, fmtWhen(e.Unix)), h("td", {}, h("span", { class: `badge ${c}` }, k)),
      h("td", { class: "small" }, e.ClientEmail || ""), h("td", { class: "mono small", dir: "ltr" }, ledgerDelta(e)),
      h("td", { class: "mono small", dir: "ltr" }, e.VolumeDelta ? gbs(e._bal) : ""), h("td", { class: "small muted" }, e.Note || ""));
  }));
  const chips = h("div", { class: "chips" }, kinds.map(([k, label]) => {
    const b = h("button", { type: "button", class: "chip", "aria-pressed": k === f ? "true" : "false", onclick: () => {
      f = k; chips.querySelectorAll("button").forEach((x) => x.setAttribute("aria-pressed", x === b ? "true" : "false")); draw();
    } }, label);
    return b;
  }));
  // Month by month.
  const months = {};
  for (const e of all) {
    const m = new Intl.DateTimeFormat(fa ? "fa-IR-u-ca-persian" : "en-GB", { year: "numeric", month: "long" }).format(new Date(e.Unix * 1000));
    const x = months[m] || (months[m] = { sold: 0, back: 0, owner: 0, n: 0 });
    const g = group(e);
    if (g === "sale") { x.sold -= e.VolumeDelta; x.n++; } else if (g === "back") x.back += e.VolumeDelta; else if (g === "owner") x.owner += e.VolumeDelta;
  }
  const csv = () => {
    const rows = [["time", "kind", "client", "volume_bytes", "units", "balance_bytes", "note"]].concat(all.map((e) => [new Date(e.Unix * 1000).toISOString(), e.Kind, e.ClientEmail, e.VolumeDelta, e.UnitsDelta, e._bal, (e.Note || "").replace(/[\r\n]/g, " ")]));
    const blob = new Blob(["﻿" + rows.map((r) => r.map((v) => `"${String(v ?? "").replace(/"/g, '""')}"`).join(",")).join("\n")], { type: "text/csv" });
    const a = h("a", { href: URL.createObjectURL(blob), download: "credit.csv" }); document.body.append(a); a.click(); a.remove();
  };
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, fa ? "اعتبار من" : "My credit"), h("button", { class: "ghost spacer", onclick: csv }, fa ? "دانلود CSV" : "Download CSV")),
    h("div", { class: "card credit-hero" }, h("div", {}, h("div", { class: "k" }, fa ? "اعتبار حجمی باقی‌مانده" : "Volume credit left"), h("div", { class: "big", dir: "ltr" }, gbs(o.volume_credit))),
      h("div", { class: "small muted" }, fa ? "هر فروش از اعتبار کم می‌کند؛ حجم مصرف‌نشدهٔ کانفیگ منقضی‌شده و کانفیگ حذف‌شده خودکار برمی‌گردد." : "Every sale takes credit; the unused volume of expired or deleted configs comes back by itself.")),
    Object.keys(months).length ? h("div", { class: "card form" }, h("h2", {}, fa ? "خلاصهٔ ماهانه" : "By month"),
      h("div", { class: "table-wrap" }, h("table", { class: "table" },
        h("thead", {}, h("tr", {}, ...[fa ? "ماه" : "Month", fa ? "فروش" : "Sold", fa ? "تعداد" : "Sales", fa ? "برگشتی" : "Returned", fa ? "شارژ مالک" : "Top-ups"].map((x) => h("th", {}, x)))),
        h("tbody", {}, ...Object.entries(months).map(([m, x]) => h("tr", {}, h("td", {}, m), h("td", { dir: "ltr" }, bytes(x.sold)), h("td", {}, fmtNum(x.n)), h("td", { dir: "ltr" }, bytes(x.back)), h("td", { dir: "ltr" }, bytes(x.owner)))))))) : null,
    h("div", { class: "card form" }, h("div", { class: "row" }, h("h2", {}, fa ? "تراکنش‌ها" : "Transactions"), h("span", { class: "spacer" }), chips),
      all.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
        h("thead", {}, h("tr", {}, ...[t("when"), t("what"), fa ? "کلاینت" : "Client", t("credit"), fa ? "مانده" : "Balance", t("note")].map((x) => h("th", {}, x)))), body)) : h("p", { class: "muted" }, t("empty"))),
    (draw(), null));
}

// Quick renewal of one or more clients: add volume and/or days, with the
// cost against the reseller's credit shown before anything is saved.
// renewForm starts a new period for one or more clients (see renew.go on
// the HUB): the term starts again today, usage goes to zero and the volume
// becomes the new package; nothing is added on top of what was left.
// Resellers see the cost first; unused volume from the old package comes back.
function renewForm(list) {
  const fa = lang === "fa";
  const rep = me.role === "rep";
  const gb = h("input", { type: "number", min: 1, max: 100000, step: 1, value: 30, dir: "ltr" });
  const days = h("input", { type: "number", min: 1, max: 3650, step: 1, value: 30, dir: "ltr" });
  const presets = (inp, vals, unit) => h("div", { class: "chips qp" }, vals.map((v) => h("button", { type: "button", class: "chip", onclick: () => { inp.value = v; calc(); } }, `${fmtNum(v)} ${unit}`)));
  const info = h("div", { class: "form" });
  const err = h("div", { class: "error" });
  const isUnl = (c) => c.Plan === "unlimited" || c.VolumeBytes < 0;
  const unl = list.filter(isUnl), vols = list.filter((c) => !isUnl(c));
  const unitsFor = (d) => (d > 0 ? Math.ceil(d / 30) : 0);
  const left = (c) => (isUnl(c) || c.VolumeBytes <= 0 ? 0 : Math.max(0, c.VolumeBytes - (c.Usage.Up + c.Usage.Down)));
  const save = h("button", { class: "primary", type: "button", onclick: async () => {
    err.textContent = "";
    save.disabled = true;
    let done = 0;
    try {
      for (const c of list) {
        await api("POST", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/renew`, { gb: isUnl(c) ? 0 : +gb.value || 0, days: +days.value || 0 });
        done++;
      }
      document.getElementById("modal").close(); toast(fa ? `${fmtNum(done)} کلاینت تمدید شد` : `${done} renewed`); route();
    } catch (e) {
      err.textContent = (done ? (fa ? `${fmtNum(done)} مورد تمدید شد، بقیه نه: ` : `${done} renewed, then: `) : "") + e.message;
      save.disabled = false;
    }
  } }, fa ? "تمدید" : "Renew");
  const calc = () => {
    const nb = Math.round((+gb.value || 0) * 2 ** 30), d = +days.value || 0;
    const charge = vols.length * nb, refund = vols.reduce((a, c) => a + left(c), 0), units = unl.length * unitsFor(d);
    const net = charge - refund;
    const credit = me.volume_credit || 0, haveU = me.unlimited_units || 0;
    const short = rep && (net > credit || units > haveU);
    save.disabled = !d || (vols.length && !nb) || short;
    const until = d ? new Intl.DateTimeFormat(fa ? "fa-IR-u-ca-persian" : "en-GB", { dateStyle: "medium" }).format(new Date(Date.now() + d * 86400000)) : "—";
    fill(info,
      list.length > 1 ? h("div", { class: "small" }, fa ? `${fmtNum(list.length)} کلاینت انتخاب شده` : `${list.length} clients selected`) : null,
      h("div", { class: "csum" }, h("div", { class: "csum-line" }, h("b", {}, fa ? "بعد از تمدید: " : "After renewing: "),
        [vols.length ? `${fmtNum(+gb.value || 0)} GB` : null, unl.length ? (fa ? "حجم نامحدود" : "unlimited volume") : null].filter(Boolean).join(" / "),
        ` · ${fa ? "تا" : "until"} ${until} · ${fa ? "مصرف از صفر" : "usage from zero"}`),
        h("div", { class: "small muted" }, fa ? "دورهٔ قبلی تمام می‌شود و دورهٔ تازه از امروز شروع می‌شود (سرویسی که هنوز وصل نشده، از اولین اتصال)." : "The old period ends and a fresh one starts today (a service never used starts at its first connection)."),
        rep ? h("div", { class: `cost ${short ? "bad" : ""}` },
          h("div", {}, `${fa ? "بستهٔ جدید" : "New package"}: `, h("b", { dir: "ltr" }, [charge ? gbs(charge) : "", units ? `${fmtNum(units)} ${t("units")}` : ""].filter(Boolean).join(" + ") || "0")),
          refund ? h("div", {}, `${fa ? "برگشت حجم مصرف‌نشدهٔ قبلی" : "Unused volume returned"}: `, h("b", { dir: "ltr" }, gbs(refund))) : null,
          h("div", {}, `${fa ? "مانده بعد از تمدید" : "Left after"}: `, h("b", { dir: "ltr" }, gbs(Math.max(0, credit - net))), units || haveU ? ` · ${fmtNum(Math.max(0, haveU - units))} ${t("units")}` : ""),
          short ? h("div", {}, fa ? "اعتبار کافی نیست." : "Not enough credit.") : null) : null));
  };
  gb.addEventListener("input", calc); days.addEventListener("input", calc);
  calc();
  const sub = me && me.sub_seller;
  const planBox = h("div", {});
  if (sub) {
    gb.readOnly = days.readOnly = true;
    api("GET", "/api/v2/shop/products?published").then((ps) => {
      const ok = (ps || []).filter((p) => !p.no_renew);
      if (!ok.length) { fill(planBox, h("p", { class: "error" }, fa ? "نماینده هنوز پلن تمدیدی برای شما تعریف نکرده." : "No renewal plans defined for you yet.")); save.disabled = true; return; }
      const chips = h("div", { class: "chips" });
      for (const p of ok) {
        const b = h("button", { type: "button", class: "chip", "aria-pressed": "false" }, p.name);
        b.addEventListener("click", () => { for (const x of chips.children) x.setAttribute("aria-pressed", String(x === b)); gb.value = p.gb; days.value = p.days; calc(); });
        chips.append(b);
      }
      fill(planBox, h("span", { class: "lbl" }, fa ? "پلن تمدید (تعریف‌شده توسط نماینده)" : "Renewal plan"), chips);
      chips.firstChild.click();
    }).catch(() => {});
  }
  openModal(list.length === 1 ? `${fa ? "تمدید" : "Renew"} · ${list[0].Email}` : (fa ? "تمدید گروهی" : "Renew selected"), h("div", { class: "form cform" },
    planBox,
    vols.length ? h("div", {}, h("label", {}, h("span", {}, fa ? "حجم بستهٔ جدید (گیگ)" : "New package volume (GB)"), gb), sub ? null : presets(gb, [10, 20, 30, 50, 100, 200], "GB")) : null,
    h("div", {}, h("label", {}, h("span", {}, fa ? "مدت دورهٔ جدید (روز)" : "New period (days)"), days), sub ? null : presets(days, [30, 60, 90, 180], fa ? "روز" : "days")),
    info, err), [save]);
}

// ---------- External API: partner keys (owner), reseller keys, docs ----------
// Scopes as people read them; the first ones are what a new key gets.
const API_SCOPES = {
  partner: [
    ["panels:read", "دیدن پنل‌ها، اعتبار، تاریخچه و آمار", "See panels, credit, history and numbers", true],
    ["panels:write", "ساخت و تغییر پنل، روشن/خاموش، رمز و لینک ورود", "Make and change panels, on/off, passwords, sign-in links", true],
    ["credit:add", "افزودن اعتبار", "Add credit", true],
    ["events", "رویدادها و وب‌هوک", "Events and webhook", true],
    ["credit:revoke", "پس گرفتن اعتبار استفاده‌نشده", "Take unused credit back", false],
    ["panels:delete", "حذف پنل", "Delete panels", false],
    ["clients:read", "دیدن مشتری‌های پنل‌ها", "Read the panels' customers", false],
  ],
  reseller: [
    ["account:read", "اعتبار، قوانین، پلن‌ها، استعلام قیمت و تاریخچه", "Credit, rules, plans, price quotes and history", true],
    ["clients:read", "دیدن مشتری‌ها، لینک‌ها و مصرف", "See customers, links and usage", true],
    ["clients:write", "ساخت، تمدید، ریست، لینک تازه، روشن/خاموش", "Make, renew, reset, new link, on/off", true],
    ["clients:delete", "حذف مشتری (با برگشت اعتبار طبق قوانین)", "Delete customers (refunds follow the rules)", true],
    ["events", "رویدادها و وب‌هوک", "Events and webhook", true],
    ["sellers", "زیرفروشنده‌ها و انتقال اعتبار", "Sub-sellers and moving credit", false],
  ],
};

// apiKeyFields: scopes, allowed addresses, expiry and rate, shared by both
// kinds of key. value() gives what the API takes.
function apiKeyFields(kind, k) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const have = k ? new Set(k.scopes || []) : null;
  const boxes = API_SCOPES[kind].map(([s, f, e, def]) => {
    const c = h("input", { type: "checkbox", checked: have ? have.has(s) : def });
    return { s, el: h("label", { class: "check", title: s }, c, h("span", {}, fa ? f : e, " ", h("small", { class: "muted", dir: "ltr" }, s))), c };
  });
  const ips = h("textarea", { rows: "2", dir: "ltr", placeholder: "203.0.113.7\n198.51.100.0/24" }, (k && k.allowed_ips || []).join("\n"));
  const days = h("input", { type: "number", min: "0", max: "3650", dir: "ltr", value: "0" });
  const rate = h("input", { type: "number", min: "0", max: "6000", dir: "ltr", value: String(k && k.rate_per_min || 0) });
  const exp = k && k.expires_unix ? h("span", { class: `badge ${k.expires_unix * 1000 < Date.now() ? "bad" : ""}` }, T("انقضا: ", "expires: "), fmtDT(k.expires_unix)) : null;
  const never = h("input", { type: "checkbox" });
  const neverBox = k && k.expires_unix ? h("label", { class: "check" }, never, h("span", {}, T("بدون انقضا (برداشتن تاریخ انقضا)", "Never expires (remove the expiry)"))) : null;
  never.onchange = () => { days.disabled = never.checked; };
  const el = h("div", { class: "form" },
    h("fieldset", { class: "scopes" }, h("legend", {}, T("دسترسی‌ها", "Permissions")), ...boxes.map((b) => b.el)),
    h("label", {}, h("span", {}, T("فقط از این IPها (هر خط یک IP یا شبکه؛ خالی = همه)", "Only from these addresses (one IP or network per line; empty = any)")), ips),
    h("div", { class: "two" },
      h("label", {}, h("span", {}, k ? T("انقضای تازه از امروز (روز، ۰ = بدون تغییر)", "New expiry from today (days, 0 = keep)") : T("انقضا بعد از (روز، ۰ = هرگز)", "Expires after (days, 0 = never)")), days, exp),
      h("label", {}, h("span", {}, T("سقف درخواست در دقیقه (۰ = پیش‌فرض ۱۲۰)", "Requests a minute (0 = default 120)")), rate)),
    neverBox);
  return {
    el,
    value() {
      const v = { scopes: boxes.filter((b) => b.c.checked).map((b) => b.s), allowed_ips: ips.value.split(/[\s,]+/).filter(Boolean), rate_per_min: +rate.value || 0 };
      if (never.checked || (!k && !(+days.value > 0))) v.expires_unix = 0;
      else if (+days.value > 0) v.expires_unix = Math.floor(Date.now() / 1000) + Math.floor(+days.value) * 86400;
      return v;
    },
  };
}

// showNewKey shows a key once, with a first call to try.
function showNewKey(kind, key) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const base = location.origin;
  const curl = kind === "partner"
    ? `curl -X POST ${base}/api/v2/partner/reps \\\n  -H "Authorization: Bearer ${key}" \\\n  -H "Idempotency-Key: order-1001" \\\n  -H "Content-Type: application/json" \\\n  -d '{"name":"Shop A","credit_gb":100,"external_ref":"order-1001"}'`
    : `curl -X POST ${base}/api/v2/reseller/clients \\\n  -H "Authorization: Bearer ${key}" \\\n  -H "Idempotency-Key: order-1001" \\\n  -H "Content-Type: application/json" \\\n  -d '{"name":"ali","gb":30,"days":30}'`;
  openModal(T("کلید ساخته شد", "Key created"), h("div", { class: "form" },
    h("p", { class: "small" }, T("این کلید را همین حالا کپی کنید؛ دوباره نشان داده نمی‌شود.", "Copy this key now — it is not shown again.")),
    h("div", { class: "mz-copy" }, h("code", { dir: "ltr" }, key), h("button", { type: "button", class: "ghost", onclick: () => copy(key) }, T("کپی", "Copy"))),
    h("p", { class: "small muted" }, kind === "partner"
      ? T("نمونهٔ ساخت پنل (جواب: آدرس پنل، نام کاربری، رمز و متن آمادهٔ ارسال برای مشتری):", "Example: make a panel (the answer has the address, username, password and a ready-to-send message):")
      : T("نمونهٔ ساخت مشتری (جواب: لینک اشتراک، مبلغ کسرشده و اعتبار باقی‌مانده):", "Example: make a customer (the answer has the subscription link, what was charged and the credit left):")),
    h("pre", { class: "code-block", dir: "ltr" }, curl),
    h("div", { class: "row" }, h("button", { type: "button", class: "ghost", onclick: () => copy(curl) }, T("کپی نمونه", "Copy example")),
      h("button", { type: "button", onclick: () => apiDocs(kind) }, "📖 ", T("راهنمای کامل API", "Full API guide")))));
}

// partnerKeys: keys another bot or site uses to make and run reseller
// panels, with a credit limit, a template, packages and permissions.
async function partnerKeys(reps) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const box = h("div", { class: "form" });
  const err = h("div", { class: "error", role: "alert" });
  const tops = (reps || []).filter((r) => !r.ParentID);
  const limit = (given, max, unit) => (max ? `${given} / ${max}${unit}` : `${given}${unit} · ${T("بدون سقف", "no limit")}`);
  let packages = [];
  try { packages = ((await api("GET", "/api/v2/admin/rep-sale")) || {}).packages || []; } catch {}
  const scopeName = (s) => { const x = API_SCOPES.partner.find((y) => y[0] === s); return x ? (fa ? x[1] : x[2]) : s; };

  // keyForm makes (k = null) or changes a key.
  const keyForm = (k) => {
    const name = h("input", { required: true, maxlength: "64", value: k ? k.name : "", placeholder: T("مثلاً سایت فروش", "e.g. sales site") });
    const maxGB = h("input", { type: "number", min: "0", step: "1", value: String(k ? k.max_gb : 0), dir: "ltr" });
    const maxU = h("input", { type: "number", min: "0", step: "1", value: String(k ? k.max_units : 0), dir: "ltr" });
    const tpl = h("select", {}, h("option", { value: "" }, T("قوانین پیش‌فرض", "Default rules")),
      tops.map((r) => h("option", { value: r.ID, selected: k && k.template === r.ID }, `${r.Name || r.ID} (${r.ID})`)));
    const pk = packages.map((p) => ({ id: p.id, c: h("input", { type: "checkbox", checked: !!(k && (k.packages || []).includes(p.id)) }), p }));
    const extra = apiKeyFields("partner", k);
    const e2 = h("div", { class: "error", role: "alert" });
    const save = h("button", { class: "primary", value: "save", onclick: async (ev) => {
      ev.preventDefault(); e2.textContent = "";
      const body = { name: name.value, max_gb: +maxGB.value || 0, max_units: +maxU.value || 0, template: tpl.value,
        packages: pk.filter((x) => x.c.checked).map((x) => x.id), ...extra.value() };
      try {
        if (k) { await api("PUT", `/api/v2/admin/partner-keys/${encodeURIComponent(k.id)}`, { ...body, enabled: k.enabled }); toast(t("saved")); partnerKeys(reps); }
        else { const nk = await api("POST", "/api/v2/admin/partner-keys", body); showNewKey("partner", nk.key); }
      } catch (x) { e2.textContent = x.message; }
    } }, k ? t("save") : T("ساخت کلید", "Create key"));
    openModal(k ? `${T("کلید", "Key")} · ${k.name}` : T("کلید پارتنر تازه", "New partner key"), h("div", { class: "form" },
      h("label", {}, h("span", {}, T("نام", "Name")), name),
      h("div", { class: "two" },
        h("label", {}, h("span", {}, T("سقف کل اعتبار حجمی (گیگ، ۰ = بدون سقف)", "Most volume credit in all (GB, 0 = no limit)")), maxGB),
        h("label", {}, h("span", {}, T("سقف کل واحد نامحدود (۰ = بدون سقف)", "Most unlimited units in all (0 = no limit)")), maxU)),
      h("label", {}, h("span", {}, T("سقف قوانین پنل‌ها از روی نماینده", "Panels' rules are capped by")), tpl,
        h("span", { class: "small muted" }, T("محدودیت‌ها، اینباندها و قیمت‌های این نماینده سقف هر پنل جدید است؛ کلید فقط می‌تواند سخت‌ترش کند.", "That reseller's limits, inbounds and prices cap every new panel; the key can only make them tighter."))),
      packages.length ? h("fieldset", { class: "scopes" }, h("legend", {}, T("پکیج‌های قابل فروش (هیچ‌کدام = همه)", "Packages it may sell (none = all)")),
        ...pk.map((x) => h("label", { class: "check" }, x.c, h("span", {}, `${x.p.name} · ${fmtNum(x.p.gb)} GB`)))) : null,
      extra.el, e2), [save]);
  };

  async function draw() {
    let list = [];
    try { list = await api("GET", "/api/v2/admin/partner-keys"); } catch (x) { err.textContent = x.message; }
    const rowFor = (k) => {
      const on = h("input", { type: "checkbox", checked: !!k.enabled, "aria-label": T("فعال", "On") });
      on.onchange = async () => {
        try { await api("PUT", `/api/v2/admin/partner-keys/${encodeURIComponent(k.id)}`, { name: k.name, max_gb: k.max_gb, max_units: k.max_units, template: k.template, enabled: on.checked }); toast(t("saved")); }
        catch (x) { on.checked = !on.checked; err.textContent = x.message; }
      };
      const expired = k.expires_unix && k.expires_unix * 1000 < Date.now();
      return h("div", { class: "card pk-row" },
        h("div", { class: "row tight" }, h("b", {}, k.name), h("span", { class: "badge", dir: "ltr" }, `dp_${k.id}_…`), h("span", { class: "spacer" }),
          h("label", { class: "check" }, on, h("span", {}, T("فعال", "On")))),
        h("div", { class: "mz-badges" },
          h("span", { class: "badge" }, T(`${fmtNum(k.panels)} پنل ساخته`, `${k.panels} panels`)),
          h("span", { class: "badge", dir: "ltr" }, limit(fmtNum(+k.given_gb.toFixed(1)), k.max_gb ? fmtNum(k.max_gb) : 0, " GB")),
          k.max_units || k.given_units ? h("span", { class: "badge", dir: "ltr" }, limit(fmtNum(k.given_units), k.max_units ? fmtNum(k.max_units) : 0, T(" واحد", " units"))) : null,
          h("span", { class: "badge" }, T("سقف قوانین: ", "rules: "), k.template || T("پیش‌فرض", "default")),
          (k.packages || []).length ? h("span", { class: "badge" }, T("پکیج‌ها: ", "packages: "), k.packages.join("، ")) : null,
          (k.allowed_ips || []).length ? h("span", { class: "badge", dir: "ltr" }, `IP: ${k.allowed_ips.join(", ")}`) : null,
          k.expires_unix ? h("span", { class: `badge ${expired ? "bad" : ""}` }, T("انقضا: ", "expires: "), fmtDT(k.expires_unix)) : null,
          h("span", { class: "badge" }, T("آخرین استفاده: ", "last used: "), k.last_used ? fmtWhen(k.last_used) : T("هرگز", "never"))),
        h("div", { class: "mz-badges" }, (k.scopes || []).map((s) => h("span", { class: `badge ${["credit:revoke", "panels:delete", "clients:read"].includes(s) ? "warn" : ""}` }, scopeName(s)))),
        h("div", { class: "row" },
          h("button", { type: "button", onclick: () => keyForm(k) }, t("edit")),
          h("button", { type: "button", class: "danger", onclick: async () => {
            if (!(await confirmAction(T(`کلید «${k.name}» حذف شود؟ هرچه از آن استفاده می‌کند از کار می‌افتد؛ پنل‌هایی که ساخته می‌مانند.`, `Delete the key “${k.name}”? Whatever uses it stops; the panels it made stay.`), t("delete")))) return;
            await guarded(() => api("DELETE", `/api/v2/admin/partner-keys/${encodeURIComponent(k.id)}`)); partnerKeys(reps);
          } }, t("delete"))));
    };
    fill(box,
      h("p", { class: "small muted" }, T("با کلید پارتنر یک ربات یا سایت دیگر خودکار پنل نمایندگی می‌فروشد: پنل می‌سازد (با پکیج یا اعتبار دلخواه)، اعتبار اضافه می‌کند، قوانین را سخت‌تر می‌کند، رمز و لینک ورود می‌دهد و از رویدادها با وب‌هوک خبردار می‌شود. برای هر کلید سقف اعتبار بگذارید تا اگر لو رفت، بیشتر از آن خرج نشود.",
        "With a partner key another bot or site sells reseller panels on its own: it makes panels (from a package or with any credit), adds credit, tightens their rules, hands out passwords and sign-in links, and hears about events through a webhook. Give each key a credit limit so a leaked key cannot hand out more.")),
      h("div", { class: "row" }, h("button", { class: "primary", onclick: () => keyForm(null) }, T("کلید تازه", "New key")),
        h("button", { onclick: () => apiDocs("partner") }, "📖 ", T("راهنمای API", "API guide"))),
      err,
      list.length ? h("div", { class: "pk-list" }, list.map(rowFor)) : h("p", { class: "muted small" }, T("هنوز کلیدی ساخته نشده.", "No keys yet.")));
  }
  await draw();
  openModal(T("API فروش پنل نمایندگی", "Reseller panel sales API"), box);
}

// resKeysCard: reseller API keys at path (the reseller's own, or the
// owner's view of one reseller).
function resKeysCard(path, title) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const card = h("section", { class: "card form" });
  const scopeName = (s) => { const x = API_SCOPES.reseller.find((y) => y[0] === s); return x ? (fa ? x[1] : x[2]) : s; };
  const form = (k) => {
    const name = h("input", { required: true, maxlength: "64", value: k ? k.name : "", placeholder: T("مثلاً ربات فروش", "e.g. sales bot") });
    const extra = apiKeyFields("reseller", k);
    const e2 = h("div", { class: "error", role: "alert" });
    const save = h("button", { class: "primary", value: "save", onclick: async (ev) => {
      ev.preventDefault(); e2.textContent = "";
      try {
        if (k) { await api("PUT", `${path}/${encodeURIComponent(k.id)}`, { name: name.value, ...extra.value() }); toast(t("saved")); draw(); }
        else { const nk = await api("POST", path, { name: name.value, ...extra.value() }); draw(); showNewKey("reseller", nk.key); }
      } catch (x) { e2.textContent = x.message; }
    } }, k ? t("save") : T("ساخت کلید", "Create key"));
    openModal(k ? `${T("کلید", "Key")} · ${k.name}` : T("کلید API تازه", "New API key"), h("div", { class: "form" }, h("label", {}, h("span", {}, T("نام", "Name")), name), extra.el, e2), [save]);
  };
  async function draw() {
    let list = [];
    const err = h("div", { class: "error", role: "alert" });
    try { list = await api("GET", path); } catch (x) { err.textContent = x.message; }
    fill(card,
      title ? h("h2", {}, title) : null,
      h("p", { class: "small muted" }, T("با این کلید ربات یا سایت خودتان مستقیم به پنل وصل می‌شود: مشتری می‌سازد، تمدید و ریست می‌کند، لینک و QR می‌گیرد و از اعتبار شما کم می‌شود؛ دقیقاً با همان قوانین پنل. ربات دارک شما هم روشن می‌ماند.",
        "With this key your own bot or website talks to the panel directly: it makes, renews and resets customers, gets their links and QR codes, and uses your credit — under exactly the panel's rules. Your Dark bot stays on.")),
      err,
      list.length ? h("div", { class: "pk-list" }, list.map((k) => {
        const on = h("input", { type: "checkbox", checked: !!k.enabled, "aria-label": T("فعال", "On") });
        on.onchange = async () => { try { await api("PUT", `${path}/${encodeURIComponent(k.id)}`, { enabled: on.checked }); toast(t("saved")); } catch (x) { on.checked = !on.checked; toast(x.message); } };
        return h("div", { class: "card pk-row" },
          h("div", { class: "row tight" }, h("b", {}, k.name), h("span", { class: "badge", dir: "ltr" }, `dr_${k.id}_…`), h("span", { class: "spacer" }), h("label", { class: "check" }, on, h("span", {}, T("فعال", "On")))),
          h("div", { class: "mz-badges" }, (k.scopes || []).map((s) => h("span", { class: "badge" }, scopeName(s))),
            (k.allowed_ips || []).length ? h("span", { class: "badge", dir: "ltr" }, `IP: ${k.allowed_ips.join(", ")}`) : null,
            k.expires_unix ? h("span", { class: `badge ${k.expires_unix * 1000 < Date.now() ? "bad" : ""}` }, T("انقضا: ", "expires: "), fmtDT(k.expires_unix)) : null,
            h("span", { class: "badge" }, T("آخرین استفاده: ", "last used: "), k.last_used ? fmtWhen(k.last_used) : T("هرگز", "never"))),
          h("div", { class: "row" }, h("button", { type: "button", onclick: () => form(k) }, t("edit")),
            h("button", { type: "button", class: "danger", onclick: async () => {
              if (!(await confirmAction(T(`کلید «${k.name}» حذف شود؟ هرچه از آن استفاده می‌کند همین حالا از کار می‌افتد.`, `Delete the key “${k.name}”? Whatever uses it stops now.`), t("delete")))) return;
              await guarded(() => api("DELETE", `${path}/${encodeURIComponent(k.id)}`)); draw();
            } }, t("delete"))));
      })) : h("p", { class: "muted small" }, T("هنوز کلیدی ندارید.", "No keys yet.")),
      h("div", { class: "row" }, h("button", { class: "primary", onclick: () => form(null) }, T("کلید تازه", "New key")),
        h("button", { onclick: () => apiDocs("reseller") }, "📖 ", T("راهنمای API", "API guide"))));
  }
  draw();
  return card;
}

// Connected shops: outside sales sites that keep their customers here. The
// owner sees a summary per shop; details only where the shop allows it.
// tenantTrafficCard: the node chart for one connected shop's clients, with a
// chip per shop when there is more than one.
function tenantTrafficCard(list, nodes) {
  const fa = lang === "fa";
  const box = h("div", {});
  let cur = list[0].id;
  try { const k = sessionStorage.getItem("ttShop"); if (list.some((x) => x.id === k)) cur = k; } catch {}
  const draw = () => {
    const x = list.find((y) => y.id === cur) || list[0];
    const chips = list.length > 1 ? h("div", { class: "row cseg" }, list.map((y) => h("button", { type: "button", class: y.id === cur ? "chip on" : "chip", "aria-pressed": String(y.id === cur),
      onclick: () => { cur = y.id; try { sessionStorage.setItem("ttShop", cur); } catch {} draw(); } }, y.name || y.id))) : null;
    fill(box, nodeTrafficCard(nodes, {
      url: (d) => `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/traffic?days=${d}`,
      title: fa ? `ترافیک هر نود · ${x.name || x.id}` : `Traffic per node · ${x.name || x.id}`,
      top: chips,
      hint: fa ? "مصرف کاربرهای همین فروشگاه روی هر نود در هر روز (کاربر حذف‌شده و مصرفی که سایت از پنل قبلی آورده حساب نمی‌شود). موس یا انگشت را روی هر روز ببرید؛ با زدن نام هر نود آن را پنهان کنید."
        : "This shop's clients' traffic per node and day (deleted clients and use brought from an earlier panel are not counted). Point or tap at a day; tap a name to hide or show it.",
    }));
  };
  draw();
  return box;
}

async function viewTenants() {
  const fa = lang === "fa";
  const [list, inbounds, reps, nodes] = await Promise.all([api("GET", "/api/v2/admin/tenants"), api("GET", "/api/v2/admin/inbounds").catch(() => []), api("GET", "/api/v2/admin/reps").catch(() => []), api("GET", "/api/v2/admin/nodes").catch(() => [])]);
  const accessLabel = { summary: fa ? "فقط خلاصه" : "summary only", read: fa ? "دیدن جزئیات" : "may look", write: fa ? "دیدن و تغییر" : "may look and change" };
  const lab = (l, el, hint) => h("label", {}, h("span", {}, l), el, hint ? h("small", { class: "muted" }, hint) : null);
  const edit = (x) => {
    const id = h("input", { value: x ? x.id : "", disabled: !!x, required: true, dir: "ltr", pattern: "[a-z0-9][a-z0-9._\\-]{0,23}" });
    const name = h("input", { value: x ? x.name : "", required: true });
    const on = h("input", { type: "checkbox", checked: x ? x.enabled : true });
    const maxC = h("input", { type: "number", min: "0", value: x ? x.max_clients : 0, dir: "ltr" });
    const maxG = h("input", { type: "number", min: "0", step: "1", value: x ? Math.round(x.max_bytes / 2 ** 30) : 0, dir: "ltr" });
    const acc = h("select", {}, ["summary", "read", "write"].map((a) => h("option", { value: a, selected: (x ? x.owner_access : "summary") === a }, accessLabel[a])));
    const note = h("textarea", { rows: "2" }, x ? x.note : "");
    const ins = h("div", { class: "chips" }, (inbounds || []).filter((i) => !["socks", "http", "dokodemo-door", "tunnel", "mtproto"].includes(i.protocol)).map((i) =>
      h("label", { class: "check" }, h("input", { type: "checkbox", value: i.tag, checked: !!(x && x.inbounds.includes(i.tag)) }), h("span", {}, i.tag))));
    const err = h("div", { class: "error", role: "alert" });
    const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
      e.preventDefault(); err.textContent = "";
      try {
        await api("PUT", `/api/v2/admin/tenants/${encodeURIComponent(id.value.trim())}`, { name: name.value, enabled: on.checked, max_clients: +maxC.value || 0,
          max_bytes: (+maxG.value || 0) * 2 ** 30, owner_access: acc.value, note: note.value, inbounds: [...ins.querySelectorAll("input:checked")].map((c) => c.value) });
        document.getElementById("modal").close("save"); toast(t("saved")); route();
      } catch (y) { err.textContent = y.message; }
    } }, t("save"));
    openModal(x ? x.name : (fa ? "فروشگاه متصل جدید" : "New connected shop"), h("div", { class: "form" },
      h("p", { class: "small muted" }, fa ? "یک سایت فروش بیرونی که کاربرهایش را روی این پنل نگه می‌دارد. حساب مالی و نماینده‌هایش با خود سایت است؛ کاربرهایش با کاربرهای شما و نماینده‌ها قاطی نمی‌شوند و ربات و فروشگاه پنل به آن‌ها پیامی نمی‌دهند."
        : "An outside sales site that keeps its customers on this panel. Its money and resellers stay with the site; its clients never mix with yours or your resellers', and the panel's bots and shop never message them."),
      h("div", { class: "two" }, lab(fa ? "شناسه (ثابت، انگلیسی)" : "ID (fixed)", id, fa ? "پیشوند نام کاربرهایش هم همین است" : "also the prefix of its clients' names"), lab(t("name"), name)),
      h("label", { class: "check" }, on, h("span", {}, fa ? "فعال (خاموش = فقط خواندن)" : "On (off = read only)")),
      h("div", { class: "two" }, lab(fa ? "سقف تعداد کاربر (۰ = بدون سقف)" : "Most clients (0 = no cap)", maxC), lab(fa ? "سقف حجم همهٔ کاربرها (گیگ، ۰ = بدون سقف)" : "Volume cap of all clients (GB, 0 = none)", maxG)),
      lab(fa ? "دسترسی مالک به جزئیات کاربرها" : "Owner access to client details", acc, fa ? "هر بار دیدن یا تغییر در گزارش تغییرات ثبت می‌شود" : "every look or change is logged"),
      h("div", { class: "small muted" }, fa ? "اینباندهای مجاز (هیچ = همهٔ اینباندهای قابل فروش)" : "Allowed inbounds (none = every sellable inbound)"), ins,
      lab(fa ? "یادداشت" : "Note", note), err), [save]);
  };
  const keys = async (x) => {
    const box = h("div", { class: "form" });
    const draw = async () => {
      const ks = await api("GET", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/keys`);
      const name = h("input", { placeholder: fa ? "مثلاً: سرور سایت" : "e.g. website server" });
      const ips = h("input", { dir: "ltr", placeholder: "203.0.113.7, 10.0.0.0/8" });
      const days = h("input", { type: "number", min: "0", max: "3650", value: "0", dir: "ltr" });
      const out = h("div", {});
      fill(box, h("p", { class: "small muted" }, fa ? "سایت با این کلید به /api/tenant/v1/ وصل می‌شود: Authorization: Bearer <کلید>. فقط کاربرهای همین فروشگاه را می‌بیند." : "The site uses this key on /api/tenant/v1/: Authorization: Bearer <key>. It only ever sees this shop's clients."),
        ks.length ? h("div", { class: "table-wrap" }, h("table", { class: "dtable" }, h("tbody", {}, ks.map((k) => h("tr", {},
          h("td", { class: "name" }, k.name), h("td", { class: "muted small", dir: "ltr" }, `dt_${k.id}_…`),
          h("td", { class: "small", dir: "ltr" }, k.allow_ips.length ? k.allow_ips.join(", ") : (fa ? "همهٔ آدرس‌ها" : "any address")),
          h("td", { class: "small muted" }, k.last_used ? fmtDT(k.last_used) : "—"),
          h("td", { class: "small" }, k.expires_unix ? `${t("keyUntil")} ${fmtDT(k.expires_unix)}` : ""),
          h("td", {}, h("button", { class: "small danger", onclick: async (e) => {
            e.preventDefault();
            await guarded(() => api("DELETE", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/keys/${encodeURIComponent(k.id)}`)); draw();
          } }, t("delete")))))))) : h("p", { class: "muted small" }, t("keyNone")),
        h("div", { class: "two" }, lab(t("keyName"), name), lab(t("keyDays"), days)),
        lab(fa ? "فقط از این آدرس‌ها (خالی = همه)" : "Only from these addresses (empty = any)", ips),
        h("button", { class: "primary", onclick: async (e) => {
          e.preventDefault();
          try {
            const r = await api("POST", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/keys`, { name: name.value, days: +days.value || 0, allow_ips: ips.value.split(/[\s,]+/).filter(Boolean) });
            fill(out, h("p", { class: "small" }, t("keyOnce")), h("code", { dir: "ltr", class: "tfakey keyout" }, r.key), h("button", { onclick: (ev) => { ev.preventDefault(); copy(r.key); } }, t("copy")));
            box.append(out);
          } catch (y) { fill(out, h("p", { class: "error" }, y.message)); box.append(out); }
        } }, t("addKey")));
    };
    openModal(`${fa ? "کلیدهای API" : "API keys"} · ${x.name}`, box);
    await draw();
  };
  const hook = async (x) => {
    const w = await api("GET", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/webhook`);
    const on = h("input", { type: "checkbox", checked: w.enabled });
    const url = h("input", { dir: "ltr", value: w.url, placeholder: "https://shop.example.com/dark-webhook" });
    const sec = h("input", { dir: "ltr", type: "password", placeholder: w.has_secret ? t("whSecretSaved") : "" });
    const err = h("div", { class: "error", role: "alert" });
    const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
      e.preventDefault(); err.textContent = "";
      try { await api("PUT", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/webhook`, { enabled: on.checked, url: url.value, secret: sec.value }); document.getElementById("modal").close("save"); toast(t("saved")); }
      catch (y) { err.textContent = y.message; }
    } }, t("save"));
    const test = h("button", { onclick: async (e) => { e.preventDefault(); err.textContent = ""; try { await api("POST", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}/webhook/test`); toast(t("whTestOK")); } catch (y) { err.textContent = y.message; } } }, t("whTest"));
    openModal(`${t("whTitle")} · ${x.name}`, h("div", { class: "form" },
      h("p", { class: "small muted" }, fa ? "فقط رویدادهای کاربرهای همین فروشگاه (انقضا، اتمام حجم، IP زیاد، ریست، برنامهٔ بعدی، دستگاه ردشده) با امضای X-Dark-Signature فرستاده می‌شود." : "Only this shop's clients' events (expired, out of volume, too many IPs, resets, next plan, refused devices), signed in X-Dark-Signature."),
      h("label", { class: "check" }, on, h("span", {}, t("whOn"))), lab(t("whURL"), url), lab(t("whSecret"), sec),
      w.last_ok ? h("p", { class: "small muted" }, `${t("whLastOK")}: ${fmtDT(w.last_ok)}`) : null,
      w.last_error ? h("p", { class: "small error" }, `${t("whLastErr")}: ${w.last_error}`) : null, err), [test, save]);
  };
  // The shop's own subscription domains: old links keep working here.
  const domains = async (x) => {
    const base = `/api/v2/admin/tenants/${encodeURIComponent(x.id)}`;
    const box = h("div", { class: "form" });
    const draw = async () => {
      const [v, rep] = await Promise.all([api("GET", `${base}/sub-domains`), api("GET", `${base}/sub-fetches`).catch(() => null)]);
      const dom = h("input", { dir: "ltr", placeholder: "sub.example.com" });
      const port = h("input", { type: "number", min: "1", max: "65535", value: "2096", dir: "ltr" });
      const prefix = h("input", { dir: "ltr", placeholder: "/2fmgkwhmfhyat8zr/" });
      const fb = h("input", { dir: "ltr", placeholder: "https://203.0.113.5:2096" });
      const tlsOn = h("input", { type: "checkbox", checked: true });
      const err = h("div", { class: "error", role: "alert" });
      const ip = (v.hub_ips || [])[0] || "";
      const row = (d) => {
        const msg = h("span", { class: "small", role: "status" });
        return h("div", { class: "card item" },
          h("div", { class: "head" }, h("span", { class: "name", dir: "ltr" }, `${d.domain}:${d.port}${d.prefix || "/…"}`),
            h("span", { class: `badge ${d.dns_unix ? "good" : ""}` }, d.dns_unix ? (fa ? "به این پنل اشاره می‌کند" : "points here") : (fa ? "هنوز به این پنل اشاره نمی‌کند" : "not pointing here yet")),
            d.tls ? h("span", { class: `badge ${d.cert_status === "ok" || d.cert_status === "issued" ? "good" : d.cert_status === "failed" ? "bad" : ""}` }, `${fa ? "گواهی" : "cert"}: ${d.cert_status || "—"}`) : null,
            d.enabled ? null : h("span", { class: "badge bad" }, fa ? "خاموش" : "off")),
          h("p", { class: "small muted", dir: "ltr" }, d.fallback ? `fallback → ${d.fallback}` : (fa ? "بدون fallback: شناسهٔ ناشناس = 404" : "no fallback: unknown ids get 404")),
          d.port_error ? h("p", { class: "small error" }, `${fa ? "پورت" : "port"} ${d.port}: ${d.port_error}`) : null,
          h("div", { class: "row" },
            h("button", { onclick: async (e) => { e.preventDefault(); msg.textContent = "…";
              try { const r = await api("POST", `${base}/sub-domains/${encodeURIComponent(d.domain)}/check`); msg.textContent = r.ok ? (r.reason || (fa ? "درست است؛ گواهی درخواست شد" : "OK; certificate ordered")) : r.reason; draw(); }
              catch (y) { msg.textContent = y.message; } } }, fa ? "بررسی DNS و گواهی" : "Check DNS and certificate"),
            h("button", { class: "ghost", onclick: async (e) => { e.preventDefault();
              await guarded(() => api("PUT", `${base}/sub-domains/${encodeURIComponent(d.domain)}`, { ...d, enabled: !d.enabled })); draw(); } }, d.enabled ? (fa ? "خاموش" : "Turn off") : (fa ? "روشن" : "Turn on")),
            h("button", { class: "ghost danger spacer", onclick: async (e) => { e.preventDefault();
              if (await confirmAction(t("confirmDelete", { x: d.domain }))) await guarded(() => api("DELETE", `${base}/sub-domains/${encodeURIComponent(d.domain)}`)); domains(x); } }, t("delete")), msg));
      };
      fill(box,
        h("p", { class: "small muted" }, fa ? `لینک‌های ساب قدیمی مشتری‌های این فروشگاه با همان دامنه، پورت و مسیر از این پنل جواب می‌گیرند. رکورد A دامنه را روی ${ip || "IP این سرور"} بگذارید و «بررسی» را بزنید. شناسه‌ای که اینجا نیست به پنل قدیمی (fallback) داده می‌شود و اگر آنجا هم نبود 404 می‌گیرد، هیچ‌وقت لیست خالی.`
          : `Old subscription links of this shop's customers answer here on the same domain, port and path. Point the domain's A record at ${ip || "this server"} and press Check. An id not here goes to the old panel (fallback), and is a 404 if it is not there either — never an empty list.`),
        rep ? h("div", { class: "chips" },
          h("span", { class: "chip good" }, `${fa ? "ساب را آپدیت کرده‌اند" : "fetched"}: ${fmtNum(rep.fetched)}`),
          h("span", { class: "chip" }, `${fa ? "هنوز نه" : "not yet"}: ${fmtNum(rep.not_fetched)}`),
          h("span", { class: "chip muted" }, `${fa ? "از" : "since"} ${fmtDT(rep.since)}`)) : null,
        ...(v.domains || []).map(row),
        h("h3", {}, fa ? "دامنهٔ تازه" : "New domain"),
        h("div", { class: "two" }, lab(fa ? "دامنه" : "Domain", dom), lab(fa ? "پورت" : "Port", port)),
        lab(fa ? "پیشوند مسیر (خالی = هر مسیری که به شناسه ختم شود)" : "Path prefix (empty = any path ending in the id)", prefix),
        lab(fa ? "پنل قدیمی برای شناسه‌های منتقل‌نشده (اختیاری)" : "Old panel for ids not moved yet (optional)", fb),
        h("label", { class: "check" }, tlsOn, h("span", {}, "HTTPS")), err,
        h("button", { class: "primary", onclick: async (e) => {
          e.preventDefault(); err.textContent = "";
          try {
            await api("PUT", `${base}/sub-domains/${encodeURIComponent(dom.value.trim())}`, { port: +port.value || 0, tls: tlsOn.checked, prefix: prefix.value.trim(), fallback: fb.value.trim(), enabled: true });
            toast(t("saved")); draw();
          } catch (y) { err.textContent = y.message; }
        } }, t("add")));
    };
    const d = openModal(`${fa ? "دامنه‌های ساب" : "Subscription domains"} · ${x.name}`, box);
    d.classList.add("wide");
    await draw();
  };
  const move = (x) => {
    const sides = [["owner", "", fa ? "مالک" : "Owner"], ...(reps || []).map((r) => ["rep", r.ID, `${fa ? "نماینده" : "Reseller"}: ${r.Name || r.ID}`]), ...(list || []).map((tn) => ["tenant", tn.id, `${fa ? "فروشگاه" : "Shop"}: ${tn.name}`])];
    const sel = (pick) => h("select", {}, sides.map(([k, id, l]) => h("option", { value: `${k}:${id}`, selected: pick === `${k}:${id}` }, l)));
    const from = sel("owner:"), to = sel(`tenant:${x.id}`);
    const names = h("textarea", { rows: "4", dir: "ltr", placeholder: fa ? "یک نام در هر خط — خالی یعنی همهٔ کاربرهای مبدأ" : "one name per line — empty means every client of the source" });
    const out = h("div", {});
    const req = (dry) => { const [fk, fid] = from.value.split(":"); const [tk, tid] = to.value.split(":");
      return { from: { kind: fk, id: fid }, to: { kind: tk, id: tid }, names: names.value.split(/\s+/).filter(Boolean), dry_run: dry }; };
    const show = (r) => fill(out, h("p", { class: "small" }, h("b", {}, `${r.dry_run ? (fa ? "پیش‌نمایش" : "Preview") : (fa ? "انجام شد" : "Done")}: ${fmtNum(r.moved)} ${fa ? "کاربر" : "clients"}`)),
      r.skipped.length ? h("details", {}, h("summary", { class: "small" }, `${t("migSkipped")} (${fmtNum(r.skipped.length)})`), h("ul", { class: "small muted", dir: "ltr" }, r.skipped.map((y) => h("li", {}, y)))) : null,
      r.notes.length ? h("ul", { class: "small muted" }, r.notes.map((y) => h("li", {}, y))) : null);
    const preview = h("button", { onclick: async (e) => { e.preventDefault(); try { show(await api("POST", "/api/v2/admin/clients/move", req(true))); } catch (y) { fill(out, h("p", { class: "error" }, y.message)); } } }, t("migPreview"));
    const go = h("button", { class: "primary", onclick: async (e) => {
      e.preventDefault();
      if (!await confirmAction(fa ? "کاربرها جابه‌جا شوند؟ لینک و کانفیگ‌هایشان عوض نمی‌شود." : "Move the clients? Their links and configs stay the same.", fa ? "جابه‌جایی" : "Move")) return;
      try { show(await api("POST", "/api/v2/admin/clients/move", req(false))); toast(t("saved")); } catch (y) { toast(t("failed", { e: y.message })); }
    } }, fa ? "جابه‌جایی" : "Move");
    openModal(fa ? "جابه‌جایی گروهی کاربرها" : "Move clients", h("div", { class: "form" },
      h("p", { class: "small muted" }, fa ? "بین مالک، نماینده‌ها و فروشگاه‌های متصل. UUID، لینک اشتراک، مصرف و سابقه حفظ می‌شود؛ اعتبار نماینده خودکار عوض نمی‌شود." : "Between the owner, resellers and connected shops. UUID, subscription link, usage and history stay; reseller credit is not changed automatically."),
      h("div", { class: "two" }, lab(fa ? "از" : "From", from), lab(fa ? "به" : "To", to)), lab(fa ? "نام‌ها" : "Names", names), out), [preview, go]);
  };
  const card = (x) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, x.name), h("span", { class: "badge", dir: "ltr" }, x.id),
      h("span", { class: `badge ${x.enabled ? "good" : "bad"}` }, x.enabled ? (fa ? "فعال" : "on") : (fa ? "خاموش" : "off")),
      h("span", { class: "badge spacer" }, accessLabel[x.owner_access])),
    h("div", { class: "chips" },
      h("span", { class: "chip" }, `${fmtNum(x.clients)} ${fa ? "کاربر" : "clients"}${x.max_clients ? ` / ${fmtNum(x.max_clients)}` : ""}`),
      h("span", { class: "chip" }, `${fmtNum(x.active)} ${fa ? "فعال" : "active"}`),
      h("span", { class: "chip" }, `${fmtNum(x.online)} ${fa ? "آنلاین" : "online"}`),
      h("span", { class: "chip" }, `${fa ? "مصرف روی این پنل" : "used on this panel"} ${bytes(x.used)}`),
      x.imported ? h("span", { class: "chip muted", title: fa ? "مصرفی که کاربرها از پنل قبلی آوردند؛ فقط از حجمشان کم می‌شود" : "use brought from the previous panel; counts against their volume only" }, `${fa ? "منتقل‌شده از پنل قبلی" : "brought from the old panel"} ${bytes(x.imported)}`) : null,
      x.volume ? h("span", { class: "chip" }, `${fa ? "حجم فروخته‌شده" : "volume sold"} ${bytes(x.volume)}${x.max_bytes ? ` / ${bytes(x.max_bytes)}` : ""}`) : null,
      h("span", { class: "chip" }, `${fmtNum(x.keys)} ${fa ? "کلید" : "keys"}`),
      h("span", { class: `chip ${x.webhook_on ? "good" : ""}` }, `${t("whTitle")}: ${x.webhook_on ? (fa ? "روشن" : "on") : (fa ? "خاموش" : "off")}`)),
    x.note ? h("p", { class: "small muted", style: "margin:0" }, x.note) : null,
    h("div", { class: "row" },
      h("button", { onclick: () => edit(x) }, t("edit")),
      h("button", { class: "ghost", onclick: () => keys(x) }, fa ? "کلیدهای API" : "API keys"),
      h("button", { class: "ghost", onclick: () => hook(x) }, t("whTitle")),
      h("button", { class: "ghost", onclick: () => { const d = openModal(x.name, repBrandCard(`/api/v2/admin/tenants/${encodeURIComponent(x.id)}/brand`, x.name)); d.classList.add("wide"); } }, fa ? "برند و صفحهٔ اشتراک" : "Brand and page"),
      h("button", { class: "ghost", onclick: () => domains(x) }, fa ? "دامنه‌های ساب" : "Subscription domains"),
      h("button", { class: "ghost", onclick: () => move(x) }, fa ? "جابه‌جایی کاربرها" : "Move clients"),
      h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: x.name }))) { await guarded(() => api("DELETE", `/api/v2/admin/tenants/${encodeURIComponent(x.id)}`)); route(); }
      } }, t("delete"))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("tenants")), h("button", { class: "primary spacer", onclick: () => edit(null) }, t("add"))),
    (list || []).length ? tenantTrafficCard(list, nodes || []) : null,
    h("p", { class: "small muted" }, fa ? "کاربرهای فروشگاه‌های متصل در لیست کاربرها، آمار فروش، یادآوری‌ها و ربات‌های پنل نمی‌آیند. سایت با API خودش (/api/tenant/v1) آن‌ها را می‌سازد و مدیریت می‌کند."
      : "Connected shops' clients are not in your client list, sales reports, reminders or the panel's bots. The site makes and manages them with its own API (/api/tenant/v1)."),
    h("div", { class: "list" }, (list || []).length ? list.map(card) : h("p", { class: "muted" }, t("empty"))));
}

// ---------- Panel resale: resellers selling panels in their own bot ----------
// The owner sets the wholesale price per TB and, per reseller, whether it may
// sell and owe a 24-hour bill; the reseller sets its prices and sees its
// wallet at the main panel and its bills.
const tomanTxt = (v) => `${fmtNum(v || 0)} ${lang === "fa" ? "تومان" : "toman"}`;

function billRows(bills, actions) {
  const fa = lang === "fa";
  if (!bills || !bills.length) return h("p", { class: "muted small" }, fa ? "فاکتوری نیست." : "No bills.");
  const st = (b) => b.status === "paid" ? (fa ? "پرداخت‌شده" : "paid") : b.status === "cancelled" ? (fa ? "بخشیده" : "forgiven") : b.overdue ? (fa ? "⛔️ مهلت گذشته" : "⛔️ overdue") : (fa ? "باز" : "open");
  return h("div", { class: "table-wrap" }, h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, "#"), h("th", {}, fa ? "مبلغ" : "Amount"), h("th", {}, fa ? "مهلت" : "Due"), h("th", {}, fa ? "وضعیت" : "Status"), h("th", {}, ""))),
    h("tbody", {}, bills.map((b) => h("tr", {}, h("td", {}, fmtNum(b.id)), h("td", {}, tomanTxt(b.amount)), h("td", {}, fmtWhen(b.due_unix)),
      h("td", {}, h("span", { class: `badge ${b.overdue ? "bad" : ""}` }, st(b))), h("td", {}, b.status === "open" ? actions(b) : null))))));
}

function walletHistory(list) {
  const fa = lang === "fa";
  if (!list || !list.length) return null;
  return h("details", {}, h("summary", {}, fa ? "تراکنش‌های کیف پول" : "Wallet history"),
    h("ul", { class: "small plain" }, list.map((e) => h("li", {}, h("b", { dir: "ltr" }, `${e.delta > 0 ? "+" : "−"}${fmtNum(Math.abs(e.delta))}`), " · ", fmtWhen(e.unix), e.note ? ` · ${e.note}` : ""))));
}

// resaleOwner: one reseller's resale, wallet and bills (owner).
async function resaleOwner(r) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const path = `/api/v2/admin/reps/${encodeURIComponent(r.ID)}`;
  const box = h("div", { class: "form" });
  const err = h("div", { class: "error", role: "alert" });
  async function draw() {
    let v;
    try { v = await api("GET", `${path}/resale`); } catch (x) { err.textContent = x.message; return; }
    const amount = h("input", { type: "number", step: "1000", dir: "ltr", placeholder: "1300000" });
    const note = h("input", { placeholder: T("توضیح (اختیاری)", "Note (optional)") });
    const close = (b, paid) => h("button", { type: "button", class: paid ? "" : "ghost", onclick: async () => {
      if (!(await confirmAction(paid ? T(`فاکتور #${b.id} پرداخت‌شده ثبت شود؟`, `Mark bill #${b.id} paid?`) : T(`فاکتور #${b.id} بخشیده شود؟`, `Forgive bill #${b.id}?`)))) return;
      await guarded(() => api("POST", `/api/v2/admin/rep-bills/${b.id}/close`, { paid })); draw();
    } }, paid ? T("پرداخت شد", "Paid") : T("بخشیدن", "Forgive"));
    fill(box, err,
      h("div", { class: "mz-badges" },
        h("span", { class: `badge ${v.on ? "good" : ""}` }, !v.resale.allowed ? T("اجازهٔ فروش پنل ندارد (محدودیت‌ها)", "not allowed to sell panels (Limits)") : v.on ? T("در رباتش فعال است", "on in its bot") : T("در رباتش فعال نیست", "not on in its bot")),
        h("span", { class: "badge" }, T("قیمت عمده هر ترابایت: ", "Wholesale per TB: "), tomanTxt(v.wholesale_tb)),
        v.resale.allowed && !v.has_card ? h("span", { class: "badge" }, T("کارتش ثبت نشده", "no card set")) : null,
        v.locked ? h("span", { class: "badge bad" }, T("قفل: فاکتور پرداخت‌نشده", "locked: unpaid bill")) : null),
      h("form", { class: "card form", onsubmit: async (e) => {
        e.preventDefault(); err.textContent = "";
        try {
          const res = await api("POST", `${path}/wallet`, { delta: +amount.value || 0, note: note.value });
          toast(T(`موجودی: ${tomanTxt(res.wallet)}`, `Balance: ${tomanTxt(res.wallet)}`)); draw();
        } catch (x) { err.textContent = x.message; }
      } },
        h("h3", {}, T("کیف پول", "Wallet"), " · ", tomanTxt(v.wallet)),
        h("p", { class: "small muted" }, T("نماینده خودش از ربات اصلی با رسید شارژ می‌کند. اینجا می‌توانید دستی اضافه یا کم کنید (عدد منفی = کسر). فاکتور باز با شارژ کافی خودکار پرداخت می‌شود.", "The reseller charges it in the main bot with a receipt. Here you can add or take by hand (negative = take). An open bill is paid automatically once covered.")),
        h("div", { class: "two" }, h("label", {}, h("span", {}, T("مبلغ (تومان)", "Amount (toman)")), amount), h("label", {}, h("span", {}, T("توضیح", "Note")), note)),
        h("div", { class: "row" }, h("button", { type: "submit" }, T("ثبت", "Apply"))),
        walletHistory(v.history)),
      h("div", { class: "card form" }, h("h3", {}, T("فاکتورها", "Bills")), billRows(v.bills, (b) => h("div", { class: "row tight" }, close(b, true), close(b, false)))));
  }
  await draw();
  openModal(`${T("کیف پول و فاکتورها", "Wallet and bills")} · ${r.Name || r.ID}`, box);
}

// resaleRepCard: the reseller's own prices, wallet and bills.
function resaleRepCard() {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const card = h("div", { class: "card form" });
  const draw = async () => {
    let v;
    try { v = await api("GET", "/api/v2/rep/resale"); } catch { card.hidden = true; return; }
    if (!v.resale.allowed) { card.hidden = true; return; }
    const on = h("input", { type: "checkbox", checked: !!v.resale.enabled });
    const ins = {};
    const err = h("div", { class: "error", role: "alert" });
    const rows = v.packages.map((p) => {
      ins[p.id] = h("input", { type: "number", min: String(p.cost), step: "1000", dir: "ltr", value: p.own || "", placeholder: String(p.owner > p.cost ? p.owner : p.cost) });
      return h("tr", {}, h("td", {}, p.name || `${fmtNum(p.gb)} GB`), h("td", {}, tomanTxt(p.cost)), h("td", {}, ins[p.id]), h("td", { class: "small" }, tomanTxt(p.price - p.cost)));
    });
    const pay = (b) => v.wallet >= b.amount ? h("button", { type: "button", onclick: async () => { await guarded(() => api("POST", `/api/v2/rep/resale/bills/${b.id}/pay`)); toast(t("saved")); draw(); } }, T("پرداخت از کیف پول", "Pay from wallet")) : null;
    fill(card,
      h("h2", {}, "🏪 ", T("فروش پنل نمایندگی در ربات من", "Selling reseller panels in my bot")),
      h("p", { class: "small muted" }, T(`مشتری در ربات شما پنل می‌خرد و به کارت شما واریز می‌کند. با تأیید رسید، قیمت عمده (هر ترابایت ${tomanTxt(v.wholesale_tb)}) از کیف پول شما در پنل اصلی کم می‌شود${v.resale.credit ? "؛ اگر کافی نباشد، فاکتور ۲۴ ساعته صادر می‌شود" : ""}. پنل فروخته‌شده زیرمجموعهٔ شما می‌شود.`,
        `A customer buys a panel in your bot and pays your card. When you approve the receipt, the wholesale price (${tomanTxt(v.wholesale_tb)} per TB) comes off your wallet at the main panel${v.resale.credit ? "; if it is short, a 24-hour bill is opened" : ""}. The panel becomes your sub-seller.`)),
      h("div", { class: "mz-badges" },
        h("span", { class: "badge" }, T("کیف پول: ", "Wallet: "), tomanTxt(v.wallet)),
        v.on ? h("span", { class: "badge good" }, T("در ربات شما فعال است", "on in your bot")) : null,
        v.has_card ? null : h("span", { class: "badge bad" }, T("اول کارت خود را ثبت کنید", "set your card first")),
        v.locked ? h("span", { class: "badge bad" }, T("قفل: فاکتور پرداخت‌نشده", "locked: unpaid bill")) : null),
      v.main_bot ? h("a", { class: "btn", href: `https://t.me/${v.main_bot}?start=wallet`, target: "_blank", rel: "noopener" }, T("➕ شارژ کیف پول در ربات اصلی", "➕ Charge the wallet in the main bot")) : null,
      h("label", { class: "check" }, on, h("span", {}, T("نمایش در ربات من", "Show in my bot"))),
      h("div", { class: "table-wrap" }, h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, T("بسته", "Package")), h("th", {}, T("قیمت عمده", "Wholesale")), h("th", {}, T("قیمت فروش شما", "Your price")), h("th", {}, T("سود", "Profit")))), h("tbody", {}, rows))),
      h("p", { class: "small muted" }, T("قیمت خالی = قیمت فروش پنل اصلی. کمتر از قیمت عمده قبول نمی‌شود.", "Empty = the main panel's price. Below wholesale is refused.")),
      err,
      h("div", { class: "row" }, h("button", { class: "primary", type: "button", onclick: async () => {
        err.textContent = "";
        const prices = {};
        for (const [id, el] of Object.entries(ins)) if (+el.value) prices[id] = +el.value;
        try { await api("PUT", "/api/v2/rep/resale", { enabled: on.checked, prices }); toast(t("saved")); draw(); } catch (x) { err.textContent = x.message; }
      } }, t("save"))),
      h("h3", {}, T("فاکتورها", "Bills")), billRows(v.bills, pay), walletHistory(v.history));
  };
  draw();
  return card;
}

// apiDocs: a guide to the partner or reseller API, with the route list
// from the panel's own OpenAPI description.
async function apiDocs(kind) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const base = location.origin;
  const P = kind === "partner" ? "/api/v2/partner" : "/api/v2/reseller";
  const key = kind === "partner" ? "dp_xxxx_yyyy" : "dr_xxxx_yyyy";
  const pre = (txt) => h("div", {}, h("pre", { class: "code-block", dir: "ltr" }, txt), h("button", { type: "button", class: "ghost small", onclick: () => copy(txt) }, T("کپی", "Copy")));
  const sec = (title, ...kids) => h("details", { class: "api-sec" }, h("summary", {}, title), h("div", { class: "form" }, ...kids));
  const php = kind === "partner"
    ? `<?php\n$ch = curl_init("${base}${P}/reps");\ncurl_setopt_array($ch, [\n  CURLOPT_POST => true,\n  CURLOPT_RETURNTRANSFER => true,\n  CURLOPT_HTTPHEADER => [\n    "Authorization: Bearer ${key}",\n    "Content-Type: application/json",\n    "Idempotency-Key: order-" . $orderId,\n  ],\n  CURLOPT_POSTFIELDS => json_encode(["name" => "Shop A", "package" => "gold", "external_ref" => "order-" . $orderId]),\n]);\n$panel = json_decode(curl_exec($ch), true);\n$code = curl_getinfo($ch, CURLINFO_HTTP_CODE);\nif ($code !== 200) { die($panel["code"] . ": " . $panel["detail"]); }\necho $panel["message"]; // address, username and password, ready to send`
    : `<?php\n$ch = curl_init("${base}${P}/clients");\ncurl_setopt_array($ch, [\n  CURLOPT_POST => true,\n  CURLOPT_RETURNTRANSFER => true,\n  CURLOPT_HTTPHEADER => [\n    "Authorization: Bearer ${key}",\n    "Content-Type: application/json",\n    "Idempotency-Key: order-" . $orderId,\n  ],\n  CURLOPT_POSTFIELDS => json_encode(["name" => "ali", "gb" => 30, "days" => 30]),\n]);\n$r = json_decode(curl_exec($ch), true);\n$code = curl_getinfo($ch, CURLINFO_HTTP_CODE);\nif ($code !== 201) { die($r["code"] . ": " . $r["detail"]); }\necho $r["client"]["sub_url"];`;
  const py = kind === "partner"
    ? `import requests\n\nAPI = "${base}${P}"\nH = {"Authorization": "Bearer ${key}"}\n\nr = requests.post(f"{API}/reps", headers={**H, "Idempotency-Key": f"order-{order_id}"},\n                  json={"name": "Shop A", "credit_gb": 100, "external_ref": f"order-{order_id}"}, timeout=30)\nr.raise_for_status()\npanel = r.json()\nprint(panel["message"])`
    : `import requests\n\nAPI = "${base}${P}"\nH = {"Authorization": "Bearer ${key}"}\n\nq = requests.post(f"{API}/quote", headers=H, json={"gb": 30, "days": 30}, timeout=30).json()\nif not q["allowed"]:\n    raise SystemExit(q["reason"])\nr = requests.post(f"{API}/clients", headers={**H, "Idempotency-Key": f"order-{order_id}"},\n                  json={"name": "ali", "gb": 30, "days": 30}, timeout=30)\nr.raise_for_status()\nprint(r.json()["client"]["sub_url"])`;
  const verifyPHP = `<?php\n$body = file_get_contents("php://input");\n$ts = $_SERVER["HTTP_X_DARK_TIMESTAMP"] ?? "";\n$sig = $_SERVER["HTTP_X_DARK_SIGNATURE"] ?? "";\n$want = "sha256=" . hash_hmac("sha256", $ts . "." . $body, $SECRET);\nif (!hash_equals($want, $sig) || abs(time() - (int)$ts) > 300) { http_response_code(401); exit; }\nforeach (json_decode($body, true)["events"] as $e) {\n  // $e["id"], $e["event"], $e["data"] — keep the last id to skip repeats\n}\nhttp_response_code(200);`;
  const verifyPy = `import hmac, hashlib, time\n\ndef verify(secret: str, ts: str, body: bytes, sig: str) -> bool:\n    want = "sha256=" + hmac.new(secret.encode(), ts.encode() + b"." + body, hashlib.sha256).hexdigest()\n    return hmac.compare_digest(want, sig) and abs(time.time() - int(ts)) < 300`;
  const table = h("div", { class: "table-wrap apiref" }, h("p", { class: "muted small" }, "…"));
  const body = h("div", { class: "form api-docs" },
    h("p", { class: "small" }, kind === "partner"
      ? T("API پارتنر برای فروش پنل نمایندگی از یک ربات یا سایت دیگر است: ساخت پنل با پکیج یا اعتبار، افزودن/پس گرفتن اعتبار، قوانین، رمز، لینک ورود یک‌بارمصرف، آمار، دفتر حساب و رویدادها.",
        "The partner API sells reseller panels from another bot or site: make panels from a package or with credit, add or take back credit, rules, passwords, one-time sign-in links, numbers, history and events.")
      : T("API نماینده برای ربات یا سایت خود نماینده است: مشتری بسازد، تمدید و ریست کند، لینک و QR بگیرد، قیمت را قبل از خرید استعلام کند و از رویدادها (انقضا، اتمام حجم، ۳ روز مانده…) خبردار شود.",
        "The reseller API is for a reseller's own bot or site: make, renew and reset customers, get links and QR codes, quote a price before buying, and hear about events (expired, out of volume, 3 days left…).")),
    sec(T("۱. آدرس و احراز هویت", "1. Address and authentication"),
      h("p", { class: "small" }, T("همهٔ درخواست‌ها JSON هستند و کلید در هدر Authorization (یا X-API-Key) می‌آید:", "Everything is JSON; send the key in Authorization (or X-API-Key):")),
      pre(`${base}${P}\nAuthorization: Bearer ${key}`)),
    sec(T("۲. تکرار امن درخواست (Idempotency-Key)", "2. Safe retries (Idempotency-Key)"),
      h("p", { class: "small" }, T("روی هر درخواست تغییری (POST/PATCH/DELETE) هدر Idempotency-Key با شناسهٔ سفارش خودتان بفرستید. اگر اتصال قطع شد و دوباره فرستادید، همان جواب قبلی برمی‌گردد (Idempotent-Replayed: true) و پنل دوتایی ساخته یا اعتبار دو بار کم نمی‌شود. جواب‌های موفق ۲۴ ساعت نگه داشته می‌شوند؛ درخواست ردشده را می‌توانید اصلاح کنید و با همان کلید دوباره بفرستید.",
        "Send an Idempotency-Key (your own order id) with every change (POST/PATCH/DELETE). A retry after a dropped connection gets the first answer again (Idempotent-Replayed: true): nothing is made twice and credit is never taken twice. Successful answers are kept for 24 hours; a refused request can be fixed and sent again with the same key.")),
      pre(`Idempotency-Key: order-1001`)),
    sec(T("۳. خطاها و محدودیت نرخ", "3. Errors and rate limits"),
      h("p", { class: "small" }, T("هر خطا این شکل را دارد و code برای برنامه ثابت است. سقف پیش‌فرض ۱۲۰ درخواست در دقیقه است؛ هدرهای X-RateLimit-Remaining و (در 429) Retry-After را ببینید.", "Every error looks like this and code is stable for programs. The default rate is 120 requests a minute; see X-RateLimit-Remaining and, on 429, Retry-After.")),
      pre(`{"detail": "not enough credit", "code": "insufficient_credit", "request_id": "3f2a9c1b7d4e8a60"}\n\nunauthorized · key_expired · ip_not_allowed · scope_missing · rate_limited\ninvalid_json · validation_failed · not_found · conflict\ninsufficient_credit · credit_limit · policy_denied · account_disabled\nidempotency_key_reused · idempotency_in_progress · internal_error`)),
    sec(T("۴. نمونه با PHP", "4. PHP example"), pre(php)),
    sec(T("۵. نمونه با Python", "5. Python example"), pre(py)),
    sec(T("۶. رویدادها و وب‌هوک", "6. Events and webhook"),
      h("p", { class: "small" }, T(`رویدادها را می‌توانید با GET ${P}/events?after=<آخرین id> بخوانید، یا با PUT ${P}/webhook یک آدرس بدهید تا برایتان POST شوند (حداکثر ۵۰ رویداد در هر ارسال، به ترتیب، با تلاش دوباره تا یک ساعت فاصله). امضا را حتماً بررسی کنید:`,
        `Read events with GET ${P}/events?after=<last id>, or give an address with PUT ${P}/webhook to have them POSTed (up to 50 per POST, in order, retried with up to an hour between tries). Always check the signature:`)),
      pre(verifyPHP), pre(verifyPy)),
    sec(T("۷. همهٔ مسیرها", "7. Every route"), table,
      h("a", { class: "btn ghost", href: `/api/v2/openapi.json?api=${kind}`, download: `dark-${kind}-openapi.json` }, T("دانلود OpenAPI (برای Postman و تولید کد)", "Download OpenAPI (for Postman and code generators)"))));
  openModal(kind === "partner" ? T("راهنمای API پارتنر", "Partner API guide") : T("راهنمای API نماینده", "Reseller API guide"), body);
  document.getElementById("modal").classList.add("wide");
  try {
    const spec = await api("GET", `/api/v2/openapi.json?api=${kind}`);
    const rows = [];
    for (const [p, ops] of Object.entries(spec.paths || {})) for (const [m, o] of Object.entries(ops)) rows.push([m.toUpperCase(), p, o]);
    rows.sort((a, b) => (a[1] === b[1] ? a[0].localeCompare(b[0]) : a[1].localeCompare(b[1])));
    fill(table, h("table", { class: "dtable" }, h("tbody", {}, rows.map(([m, p, o]) => h("tr", {},
      h("td", {}, h("span", { class: `badge method m-${m.toLowerCase()}` }, m)),
      h("td", {}, h("code", { dir: "ltr", class: "path" }, p), h("div", { class: "small muted", dir: "ltr" }, o.summary)),
      h("td", {}, o.description ? h("span", { class: "badge", dir: "ltr" }, o.description.replace(/^Scope: `|`$/g, "")) : null))))));
  } catch (x) { fill(table, h("p", { class: "error" }, x.message)); }
}

// ---------- Old users: a reseller brings its customers by their links ----------
async function viewLegacy() {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const v = await api("GET", "/api/v2/rep/legacy");
  const err = h("div", { class: "error", role: "alert" });
  const steps = [T("لینک‌های ساب", "Subscription links"), T("دامنه", "Domain"), T("پایان", "Done")];
  const stepper = h("ol", { class: "lg-steps" }, steps.map((s, i) => h("li", { class: i === v.step ? "on" : i < v.step ? "done" : "" }, h("b", {}, fmtNum(i + 1)), " ", s)));
  const gb = (b) => `${fmtNum(+(b / 2 ** 30).toFixed(1))} GB`;
  const facts = h("div", { class: "mz-badges" },
    h("span", { class: "badge" }, T("کاربر آمده: ", "Users brought: "), fmtNum(v.clients)),
    v.domain ? h("span", { class: "badge", dir: "ltr" }, `${v.domain}:${v.sub_port}`) : null,
    v.fwd_ports && v.fwd_ports.length ? h("span", { class: "badge", dir: "ltr" }, T("پورت کانفیگ‌ها: ", "Config ports: "), v.fwd_ports.join(", ")) : null);
  let body;
  if (v.step === 0) {
    const ta = h("textarea", { rows: "10", dir: "ltr", placeholder: "https://sub.example.com:2096/sub/abc123\nhttps://sub.example.com:2096/sub/def456" });
    const j = v.job || {};
    const pct = j.total ? Math.round((j.done / j.total) * 100) : 0;
    if (j.running) setTimeout(() => { if (location.hash.startsWith("#/legacy")) route(); }, 2000);
    body = h("div", { class: "card form" },
      h("h2", {}, T("۱. لینک ساب کاربرها", "1. The users' subscription links")),
      h("p", { class: "small muted" }, T("لینک ساب هر کاربر را از پنل قبلی‌تان (ثنایی، مرزبان، پاسارگاد…) کپی کنید؛ هر خط یک لینک. پنل هر لینک را می‌خواند و کاربر را با همان مصرف، حجم، انقضا و همان لینک ساب در بخش شما می‌سازد. آوردن کاربرها رایگان است؛ از این به بعد فقط مصرف واقعی‌شان از اعتبار شما کم می‌شود. این کاربرها قابل تمدید نیستند.",
        "Copy each user's subscription link from your old panel (3x-ui, Marzban, PasarGuard…), one per line. Each link is read and the user made under you with the same usage, volume, expiry and subscription link. Bringing them is free; from now on only what they use comes off your credit. Old users cannot be renewed.")),
      ta, err,
      j.running ? h("div", {}, h("progress", { max: "100", value: String(pct) }), h("p", { class: "small" }, T(`${fmtNum(j.done)} از ${fmtNum(j.total)} خوانده شد…`, `${j.done} of ${j.total} read…`))) : null,
      j.total && !j.running ? h("div", { class: "mz-badges" },
        h("span", { class: "badge good" }, T("آمد: ", "Added: "), fmtNum(j.added)),
        j.already ? h("span", { class: "badge" }, T("از قبل بود: ", "Already here: "), fmtNum(j.already)) : null,
        j.failed && j.failed.length ? h("span", { class: "badge bad" }, T("خطا: ", "Failed: "), fmtNum(j.failed.length)) : null) : null,
      j.failed && j.failed.length && !j.running ? h("details", {}, h("summary", {}, T("لینک‌های خطادار", "Failed links")),
        h("ul", { class: "small plain", dir: "ltr" }, j.failed.map((f) => h("li", {}, h("code", {}, f.link), " — ", f.reason)))) : null,
      h("div", { class: "row" },
        h("button", { class: "primary", disabled: j.running, onclick: async () => {
          err.textContent = "";
          try { await api("POST", "/api/v2/rep/legacy/import", { links: ta.value }); route(); } catch (x) { err.textContent = x.message; }
        } }, T("خواندن و ثبت", "Read and add")),
        h("span", { class: "spacer" }),
        h("button", { disabled: j.running || !v.clients, onclick: async () => { await guarded(() => api("POST", "/api/v2/rep/legacy/step", { step: 1 })); route(); } }, T("مرحلهٔ بعد: دامنه", "Next: domain"))));
  } else if (v.step === 1) {
    const ip = (v.hub_ips || [])[0] || "";
    const why = h("p", { class: "small", role: "status" });
    body = h("div", { class: "card form" },
      h("h2", {}, T("۲. دامنهٔ ساب را به این پنل بدهید", "2. Point your subscription domain here")),
      h("p", {}, T(`در تنظیمات DNS دامنهٔ `, "In the DNS of "), h("b", { dir: "ltr" }, v.domain), T(` رکورد A را روی این IP بگذارید (اگر کلودفلر است، ابر نارنجی خاموش):`, ", set the A record to this IP (on Cloudflare, proxy off):")),
      h("div", { class: "mz-copy" }, h("code", { dir: "ltr" }, ip), h("button", { type: "button", class: "ghost", onclick: () => copy(ip) }, T("کپی", "Copy"))),
      h("p", { class: "small muted" }, T(`بعد از تغییر، «بررسی» را بزنید. از همان لحظه لینک‌های ساب قدیمی از این پنل جواب می‌دهند و کانفیگ‌های قدیمی (پورت‌های ${(v.fwd_ports || []).join("، ") || "—"}) تا ۳ روز به سرور قبلی رد می‌شوند تا کسی قطع نشود. سرور قبلی را تا آن موقع روشن نگه دارید.`,
        `After the change press “Check”. From then on the old links answer from here, and the old configs (ports ${(v.fwd_ports || []).join(", ") || "—"}) are passed to the old server for 3 days so nobody is cut. Keep the old server on until then.`)),
      Object.keys(v.port_errors || {}).length ? h("p", { class: "error" }, T("این پورت‌ها روی این سرور قابل استفاده نیستند و کاربرهایشان تا آپدیت ساب قطع می‌شوند: ", "These ports cannot be used on this server; their users are cut until they update: "), Object.entries(v.port_errors).map(([p, e]) => `${p} (${e})`).join("، ")) : null,
      why, err,
      h("div", { class: "row" },
        h("button", { class: "primary", onclick: async () => {
          err.textContent = ""; why.textContent = T("در حال بررسی…", "Checking…");
          try {
            const r = await api("POST", "/api/v2/rep/legacy/check");
            if (r.ok) route(); else why.textContent = T("هنوز نه: ", "Not yet: ") + (r.reason || "");
          } catch (x) { why.textContent = ""; err.textContent = x.message; }
        } }, T("بررسی", "Check")),
        h("span", { class: "spacer" }),
        h("button", { class: "ghost", onclick: async () => { await guarded(() => api("POST", "/api/v2/rep/legacy/step", { step: 0 })); route(); } }, T("بازگشت به لینک‌ها", "Back to links"))));
  } else {
    const left = v.fwd_until - Date.now() / 1000;
    const cert = { ok: T("گواهی SSL آماده است", "certificate ready"), failed: T("گواهی SSL نگرفت: ", "certificate failed: ") + (v.cert_error || ""), issuing: T("در حال گرفتن گواهی SSL…", "getting the certificate…"), pending: T("در صف گرفتن گواهی SSL", "certificate queued") }[v.cert] || "";
    body = h("div", { class: "card form" },
      h("h2", {}, T("۳. انتقال انجام شد", "3. Moved")),
      h("p", {}, T("کاربرها با بروزرسانی ساب در اپ، کانفیگ‌های جدید را می‌گیرند. کاربرهای جدیدی که می‌سازید با آدرس ساب این پنل ساخته می‌شوند.", "Users get the new configs when their app updates the subscription. New users you make use this panel's subscription address.")),
      h("div", { class: "mz-badges" },
        h("span", { class: "badge good" }, T("ساب را آپدیت کرده‌اند: ", "Updated: "), `${fmtNum(v.updated)} / ${fmtNum(v.clients)}`),
        v.off ? h("span", { class: "badge bad" }, T("قطع به‌خاطر اعتبار: ", "Off for credit: "), fmtNum(v.off)) : null,
        h("span", { class: "badge" }, T("مصرف کسرشده از اعتبار: ", "Use paid from credit: "), gb(v.billed)),
        cert ? h("span", { class: `badge ${v.cert === "failed" ? "bad" : ""}` }, cert) : null,
        left > 0 ? h("span", { class: "badge" }, T("عبور کانفیگ‌های قدیمی تا ", "Old configs passed through until "), fmtWhen(v.fwd_until)) : h("span", { class: "badge" }, T("عبور کانفیگ‌های قدیمی تمام شد", "Pass-through ended"))),
      Object.keys(v.port_errors || {}).length ? h("p", { class: "error" }, T("این پورت‌ها روی سرور باز نشدند: ", "These ports could not open: "), Object.entries(v.port_errors).map(([p, e]) => `${p} (${e})`).join("، ")) : null,
      h("a", { class: "btn", href: "#/clients" }, T("دیدن کاربران قدیمی", "See the old users")));
  }
  return h("div", {}, h("div", { class: "row" }, h("h1", {}, T("کاربران قدیمی", "Old users"))), stepper, facts, body);
}

// ---------- Panel sale page: everything about resellers selling panels ----------
function tabsView(tabs, key) {
  let cur = (() => { try { return sessionStorage.getItem(key) || tabs[0][0]; } catch { return tabs[0][0]; } })();
  if (!tabs.find((x) => x[0] === cur)) cur = tabs[0][0];
  const bar = h("div", { class: "chips", role: "tablist" });
  const pane = h("div", {});
  const draw = () => {
    bar.replaceChildren(...tabs.map(([k, label, , badge]) => {
      const b = h("button", { role: "tab", "aria-selected": String(k === cur), class: `chip ${k === cur ? "on" : ""}` }, label, badge ? h("span", { class: "count" }, fmtNum(badge)) : null);
      b.onclick = () => { cur = k; try { sessionStorage.setItem(key, k); } catch {} draw(); };
      return b;
    }));
    const t = tabs.find((x) => x[0] === cur);
    pane.replaceChildren(t[2]());
  };
  draw();
  return h("div", {}, bar, pane);
}

function packageForm(p, inbounds, onSave) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  p = p || { gb: 1024, price: 0, allow_unlimited: false, units: 0, inbounds: [], can_delete: true };
  const inp = (v, attrs) => h("input", { type: "number", min: "0", dir: "ltr", value: v || 0, ...(attrs || {}) });
  const f = { name: h("input", { value: p.name || "", placeholder: T("مثلاً ۱ ترابایت", "e.g. 1 TB") }), gb: inp(p.gb, { min: "1" }), price: inp(p.price, { step: "1000" }),
    au: h("input", { type: "checkbox", checked: !!p.allow_unlimited }), units: inp(p.units), mc: inp(p.max_clients), md: inp(p.max_days), mg: inp(p.max_client_gb),
    cd: h("input", { type: "checkbox", checked: !!p.can_delete }), ru: h("input", { type: "checkbox", checked: !!p.refund_unused }) };
  const chosen = new Set(p.inbounds || []);
  const ibs = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: chosen.has(ib.tag) }), h("span", {}, ib.tag)));
  const lab = (l, el) => h("label", {}, h("span", {}, l), el);
  openModal(p.id ? T("ویرایش بسته", "Edit package") : T("بستهٔ جدید", "New package"), h("div", { class: "form" },
    lab(T("نام", "Name"), f.name),
    h("div", { class: "two" }, lab(T("حجم (گیگ)", "Volume (GB)"), f.gb), lab(T("قیمت فروش شما (تومان)", "Your price (toman)"), f.price)),
    h("label", { class: "check" }, f.au, T("اجازهٔ فروش نامحدود", "May sell unlimited")),
    lab(T("واحد نامحدود همراه بسته", "Unlimited units included"), f.units),
    h("div", { class: "two" }, lab(T("حداکثر کلاینت (۰ = بی‌سقف)", "Max clients (0 = none)"), f.mc), lab(T("حداکثر روز هر کلاینت", "Max days per client"), f.md)),
    lab(T("حداکثر حجم هر کلاینت (گیگ)", "Max GB per client"), f.mg),
    h("label", { class: "check" }, f.cd, T("اجازهٔ حذف کلاینت", "May delete clients")),
    h("label", { class: "check" }, f.ru, T("برگشت حجم مصرف‌نشده هنگام حذف", "Refund unused on delete")),
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, T("اینباندها (هیچ‌کدام = همه)", "Inbounds (none = all)")), h("div", { class: "chips" }, ibs))),
    [h("button", { class: "primary", value: "save", onclick: async (e) => {
      e.preventDefault();
      const np = { ...p, name: f.name.value, gb: +f.gb.value || 0, price: +f.price.value || 0, allow_unlimited: f.au.checked, units: +f.units.value || 0, max_clients: +f.mc.value || 0, max_days: +f.md.value || 0,
        max_client_gb: +f.mg.value || 0, can_delete: f.cd.checked, refund_unused: f.ru.checked, inbounds: ibs.map((l) => l.querySelector("input")).filter((i) => i.checked).map((i) => i.value) };
      if (await onSave(np)) document.getElementById("modal").close("save");
    } }, t("save"))]);
}

function soldPanelsList(panels, actions) {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  if (!panels.length) return h("p", { class: "muted" }, T("هنوز پنلی فروخته نشده.", "No panels sold yet."));
  return h("div", { class: "list" }, panels.map((p) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, p.name || p.id), h("span", { class: "badge", dir: "ltr" }, p.login || p.id),
      h("span", { class: `badge ${p.enabled ? "good" : "bad"} spacer` }, p.enabled ? T("روشن", "On") : T("خاموش", "Off"))),
    h("div", { class: "muted small" }, `${T("اعتبار", "Credit")}: ${bytes(p.credit)}${p.units ? ` · ${T("واحد", "units")}: ${fmtNum(p.units)}` : ""} · ${T("کلاینت", "clients")}: ${fmtNum(p.active)} / ${fmtNum(p.clients)} · ${T("مصرف", "used")}: ${bytes(p.used)}`),
    h("div", { class: "muted small" }, `${T("فروش", "Sold")}: ${p.sold ? fmtWhen(p.sold) : "—"} · ${T("شارژ", "top-ups")}: ${fmtNum(p.top_ups)} · ${T("پرداخت مشتری", "paid")}: ${tomanTxt(p.paid)}${p.last_use ? ` · ${T("آخرین استفاده", "last use")}: ${fmtWhen(p.last_use)}` : ""}`),
    actions ? h("div", { class: "row" }, actions(p)) : null)));
}

async function viewResale() {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const [v, inbounds] = await Promise.all([api("GET", "/api/v2/admin/resale"), api("GET", "/api/v2/admin/inbounds").catch(() => [])]);
  const sale = v.sale || { packages: [] };
  sale.packages = sale.packages || [];
  const saveSale = async (ns) => { try { await api("PUT", "/api/v2/admin/rep-sale", ns); toast(t("saved")); route(); return true; } catch (x) { toast(x.message); return false; } };
  const settings = () => {
    const tb = h("input", { type: "number", min: "0", step: "1000", dir: "ltr", value: sale.wholesale_tb || 0 });
    const on = h("input", { type: "checkbox", checked: !!sale.enabled });
    return h("div", {},
      h("form", { class: "card form", onsubmit: (e) => { e.preventDefault(); saveSale({ ...sale, wholesale_tb: +tb.value || 0, enabled: on.checked }); } },
        h("h2", {}, T("قیمت و فروش", "Price and sale")),
        h("label", {}, h("span", {}, T("قیمت عمدهٔ هر ترابایت برای نماینده‌ها (تومان، ۰ = خاموش)", "Wholesale per TB for resellers (toman, 0 = off)")), tb),
        h("label", { class: "check" }, on, T("فروش پنل نمایندگی در ربات اصلی خودم هم روشن باشد", "Also sell reseller panels in my own main bot")),
        h("p", { class: "small muted" }, T("اجازهٔ هر نماینده: نماینده‌ها ← ⋯ ← محدودیت‌ها.", "Per reseller: Resellers → ⋯ → Limits.")),
        h("div", { class: "row" }, h("button", { class: "primary", type: "submit" }, t("save")))),
      h("div", { class: "card form" },
        h("div", { class: "row" }, h("h2", {}, T("بسته‌ها", "Packages")), h("button", { class: "primary spacer", onclick: () => packageForm(null, inbounds, (np) => saveSale({ ...sale, packages: [...sale.packages, np] })) }, t("add"))),
        h("p", { class: "small muted" }, T("همین بسته‌ها در ربات اصلی، ربات نماینده‌ها و پنل نماینده‌ها فروخته می‌شوند. قیمت نماینده‌ها از قیمت عمدهٔ هر ترابایت حساب می‌شود.", "The same packages sell in your bot, the resellers' bots and their panels. A reseller's cost comes from the wholesale price per TB.")),
        sale.packages.length ? h("div", { class: "list" }, sale.packages.map((p, i) => h("div", { class: "card item" },
          h("div", { class: "head" }, h("span", { class: "name" }, p.name), h("span", { class: "badge" }, `${fmtNum(p.gb)} GB`), h("span", { class: "badge spacer" }, tomanTxt(p.price))),
          h("div", { class: "muted small" }, `${T("عمده", "wholesale")}: ${tomanTxt(Math.ceil(Math.ceil((p.gb * (sale.wholesale_tb || 0)) / 1024) / 1000) * 1000)}${p.allow_unlimited ? ` · ${T("نامحدود", "unlimited")} (${fmtNum(p.units)})` : ""}`),
          h("div", { class: "row" },
            h("button", { onclick: () => packageForm(p, inbounds, (np) => saveSale({ ...sale, packages: sale.packages.map((x, j) => (j === i ? np : x)) })) }, t("edit")),
            h("button", { class: "ghost danger", onclick: async () => { if (await confirmAction(T(`بستهٔ «${p.name}» حذف شود؟`, `Delete “${p.name}”?`))) saveSale({ ...sale, packages: sale.packages.filter((_, j) => j !== i) }); } }, t("delete")))))) : h("p", { class: "muted" }, T("هنوز بسته‌ای نیست.", "No packages yet."))));
  };
  const reps = () => (v.reps || []).length ? h("div", { class: "list" }, v.reps.map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.name || r.id), h("span", { class: "badge" }, r.id),
      h("span", { class: `badge ${r.on ? "good" : ""} spacer` }, r.on ? T("در رباتش فعال", "on in its bot") : T("در رباتش خاموش", "off in its bot"))),
    h("div", { class: "mz-badges" },
      h("span", { class: "badge" }, T("کیف پول: ", "Wallet: "), tomanTxt(r.wallet)),
      r.open_bill ? h("span", { class: `badge ${r.overdue ? "bad" : ""}` }, T("فاکتور باز: ", "Open bill: "), tomanTxt(r.open_bill), r.overdue ? T(" (مهلت گذشته)", " (overdue)") : "") : null,
      h("span", { class: "badge" }, T("پنل: ", "Panels: "), `${fmtNum(r.stats.active)} / ${fmtNum(r.stats.panels)}`),
      h("span", { class: "badge" }, T("فروش این ماه: ", "This month: "), `${fmtNum(r.stats.month_sales)} · ${tomanTxt(r.stats.month_cost)}`)),
    h("div", { class: "row" },
      h("button", { onclick: () => resaleOwner({ ID: r.id, Name: r.name }) }, T("کیف پول و فاکتورها", "Wallet and bills")),
      h("button", { class: "ghost", onclick: async () => {
        const x = await api("GET", `/api/v2/admin/reps/${encodeURIComponent(r.id)}/panels`);
        openModal(`${T("پنل‌های فروخته‌شده", "Sold panels")} · ${r.name || r.id}`, soldPanelsList(x.panels || []));
      } }, T("پنل‌ها", "Panels")))))) : h("p", { class: "muted" }, T("هنوز به هیچ نماینده‌ای اجازهٔ فروش پنل نداده‌اید (نماینده‌ها ← ⋯ ← محدودیت‌ها).", "No reseller may sell panels yet (Resellers → ⋯ → Limits)."));
  const receipts = () => (v.pending || []).length ? h("div", { class: "list" }, v.pending.map((o) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, T("شارژ کیف پول #", "Wallet top-up #"), fmtNum(o.id)), h("span", { class: "badge" }, o.rep_id), h("span", { class: "badge spacer" }, tomanTxt(o.price))),
    h("div", { class: "muted small" }, `${o.buyer_name || ""} · ${fmtWhen(o.unix)} · ${T("عکس رسید در ربات اصلی آمده است", "the receipt photo is in the main bot")}`),
    h("div", { class: "row" },
      h("button", { class: "primary", onclick: async () => { await guarded(() => api("POST", `/api/v2/admin/rep-orders/${o.id}/approve`)); toast(t("saved")); route(); } }, T("تأیید و شارژ", "Approve")),
      h("button", { class: "ghost danger", onclick: async () => { if (await confirmAction(T("این رسید رد شود؟", "Reject this receipt?"))) { await guarded(() => api("POST", `/api/v2/admin/rep-orders/${o.id}/reject`)); route(); } } }, T("رد", "Reject")))))) : h("p", { class: "muted" }, T("رسیدی منتظر نیست.", "No receipts waiting."));
  return h("div", {}, h("div", { class: "row" }, h("h1", {}, T("فروش پنل", "Panel sale"))),
    tabsView([["reps", T("نماینده‌ها", "Resellers"), reps], ["receipts", T("رسیدها", "Receipts"), receipts, (v.pending || []).length], ["settings", T("تنظیمات و بسته‌ها", "Settings and packages"), settings]], "resale-tab"));
}

async function viewResaleRep() {
  const fa = lang === "fa";
  const T = (f, e) => (fa ? f : e);
  const [x, v] = await Promise.all([api("GET", "/api/v2/rep/resale/panels").catch(() => ({ panels: [], stats: {} })), api("GET", "/api/v2/rep/resale")]);
  const panels = x.panels || [], st = x.stats || {};
  const creds = (res) => {
    const text = `${T("آدرس پنل", "Panel")}: ${res.address}\n${T("نام کاربری", "Username")}: ${res.username}\n${T("رمز", "Password")}: ${res.password}`;
    openModal(T("اطلاعات ورود", "Login"), h("div", { class: "form" },
      h("pre", { class: "code-block", dir: "ltr" }, text),
      h("button", { class: "primary", type: "button", onclick: () => copy(text) }, T("کپی برای ارسال به مشتری", "Copy for the customer"))));
  };
  const pkgSelect = () => h("select", {}, v.packages.map((p) => h("option", { value: p.id }, `${p.name || fmtNum(p.gb) + " GB"} · ${T("فروش", "price")} ${tomanTxt(p.price)} · ${T("عمده", "cost")} ${tomanTxt(p.cost)}`)));
  const payNote = h("p", { class: "small muted" }, T(`قیمت عمده از کیف پول شما (موجودی ${tomanTxt(v.wallet)}) کم می‌شود${v.resale.credit ? "؛ اگر کافی نباشد، فاکتور ۲۴ ساعته صادر می‌شود" : ""}.`,
    `The wholesale price comes off your wallet (balance ${tomanTxt(v.wallet)})${v.resale.credit ? "; if short, a 24-hour bill is opened" : ""}.`));
  const sellNew = () => {
    const user = h("input", { dir: "ltr", required: true, placeholder: "shop-ali", autocomplete: "off" });
    const name = h("input", { placeholder: T("نام مشتری (اختیاری)", "Customer name (optional)") });
    const pkg = pkgSelect();
    const err = h("div", { class: "error", role: "alert" });
    openModal(T("فروش پنل جدید", "Sell a new panel"), h("div", { class: "form" },
      h("label", {}, h("span", {}, T("نام کاربری پنل", "Panel username")), user), h("label", {}, h("span", {}, T("نام", "Name")), name),
      h("label", {}, h("span", {}, T("بسته", "Package")), pkg), payNote.cloneNode(true), err),
      [h("button", { class: "primary", value: "save", onclick: async (e) => {
        e.preventDefault(); err.textContent = "";
        try {
          const res = await api("POST", "/api/v2/rep/resale/sell", { name: user.value, buyer: name.value, package: pkg.value });
          document.getElementById("modal").close("save");
          creds(res);
          if (res.bill) toast(T(`فاکتور #${res.bill} با مهلت ۲۴ ساعت صادر شد`, `Bill #${res.bill} opened, due in 24 hours`));
          route();
        } catch (x2) { err.textContent = x2.message; }
      } }, T("فروش", "Sell"))]);
  };
  const topUp = (p) => {
    const pkg = pkgSelect();
    const err = h("div", { class: "error", role: "alert" });
    openModal(`${T("شارژ پنل", "Top up")} · ${p.name || p.id}`, h("div", { class: "form" }, h("label", {}, h("span", {}, T("بسته", "Package")), pkg), payNote.cloneNode(true), err),
      [h("button", { class: "primary", value: "save", onclick: async (e) => {
        e.preventDefault(); err.textContent = "";
        try {
          const res = await api("POST", "/api/v2/rep/resale/sell", { target: p.id, package: pkg.value });
          document.getElementById("modal").close("save");
          toast(res.bill ? T(`شارژ شد؛ فاکتور #${res.bill} صادر شد`, `Done; bill #${res.bill} opened`) : T("شارژ شد", "Done"));
          route();
        } catch (x2) { err.textContent = x2.message; }
      } }, T("شارژ", "Top up"))]);
  };
  const mine = () => h("div", {},
    h("div", { class: "mz-badges" },
      h("span", { class: "badge" }, T("پنل‌ها: ", "Panels: "), `${fmtNum(st.active)} / ${fmtNum(st.panels)}`),
      h("span", { class: "badge" }, T("اعتبار باقی پنل‌ها: ", "Their credit: "), bytes(st.credit)),
      h("span", { class: "badge" }, T("فروش این ماه: ", "This month: "), `${fmtNum(st.month_sales)} · ${tomanTxt(st.month_paid)}`),
      h("span", { class: "badge good" }, T("سود این ماه: ", "Profit this month: "), tomanTxt(st.month_paid - st.month_cost)),
      h("span", { class: "badge" }, T("کیف پول: ", "Wallet: "), tomanTxt(v.wallet)),
      v.locked ? h("span", { class: "badge bad" }, T("قفل: فاکتور پرداخت‌نشده", "locked: unpaid bill")) : null),
    h("div", { class: "row" }, h("button", { class: "primary", disabled: !v.packages.length || v.locked, onclick: sellNew }, T("➕ فروش پنل جدید", "➕ Sell a new panel"))),
    soldPanelsList(panels, (p) => [
      h("button", { disabled: v.locked, onclick: () => topUp(p) }, T("شارژ با بسته", "Top up")),
      h("button", { class: "ghost", onclick: async () => { await guarded(() => api("POST", `/api/v2/rep/resale/panels/${encodeURIComponent(p.id)}/enabled`, { enabled: !p.enabled })); route(); } }, p.enabled ? T("خاموش کردن", "Turn off") : T("روشن کردن", "Turn on")),
      h("button", { class: "ghost", onclick: async () => { if (await confirmAction(T("رمز تازه ساخته شود؟ رمز قبلی دیگر کار نمی‌کند.", "Make a new password? The old one stops working."))) { const res = await guarded(() => api("POST", `/api/v2/rep/resale/panels/${encodeURIComponent(p.id)}/password`)); creds({ ...res, address: location.origin }); } } }, T("رمز تازه", "New password"))]));
  return h("div", {}, h("div", { class: "row" }, h("h1", {}, T("فروش پنل", "Panel sale"))),
    tabsView([["mine", T("پنل‌های من", "My panels"), mine], ["prices", T("قیمت‌ها و کیف پول", "Prices and wallet"), resaleRepCard]], "resale-rep-tab"));
}

// Owner: resellers as a table with this month's numbers; actions in a menu.
async function viewReps() {
  const fa = lang === "fa";
  const [reps, inbounds, ov] = await Promise.all([api("GET", "/api/v2/admin/reps"), api("GET", "/api/v2/admin/inbounds"), api("GET", "/api/v2/admin/reps/overview").catch(() => [])]);
  const byID = Object.fromEntries((ov || []).map((o) => [o.rep_id, o]));
  const del = async (r) => {
    const msg = r.Clients
      ? (fa ? `نمایندهٔ ${r.Name || r.ID} همراه زیرفروشنده‌ها و ${fmtNum(r.Clients)} کلاینتش حذف شود؟ کلاینت‌ها قطع می‌شوند و این کار برگشت ندارد.` : `Delete ${r.Name || r.ID}, its sub-sellers and its ${r.Clients} clients? The clients stop working; this cannot be undone.`)
      : (fa ? `نمایندهٔ ${r.Name || r.ID} و زیرفروشنده‌هایش حذف شود؟` : `Delete ${r.Name || r.ID} and its sub-sellers?`);
    if (!(await confirmAction(msg, t("delete")))) return;
    await guarded(() => api("DELETE", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}?clients=1`));
    toast(fa ? "حذف شد" : "Deleted"); route();
  };
  const menu = (r) => (e) => popMenu(e.currentTarget, (m) => {
    const item = (label, fn, cls) => h("button", { class: `pm-item ${cls || ""}`, onclick: () => { m.remove(); fn(); } }, label);
    m.append(
      item(t("policy"), () => policyForm(r, inbounds)),
      item(t("payment"), () => paymentForm(r)),
      item(fa ? "صفحهٔ اشتراک" : "Sub page", () => openModal(`${fa ? "برند و صفحهٔ اشتراک" : "Brand & subscription page"} · ${r.Name || r.ID}`, repBrandCard(`/api/v2/admin/reps/${encodeURIComponent(r.ID)}/brand`))),
      item(fa ? "ربات و کارت" : "Bot & card", () => openModal(`${fa ? "ربات و کارت" : "Bot & card"} · ${r.Name || r.ID}`, h("div", { class: "form" }, repBotCard(`/api/v2/admin/reps/${encodeURIComponent(r.ID)}/bot`), sellerCardCard(`/api/v2/shop/card?seller=${encodeURIComponent(r.ID)}`)))),
      item(t("activity"), () => { location.hash = `#/activity?rep=${encodeURIComponent(r.ID)}`; }),
      ...(fullOwner() ? [item(fa ? "کلیدهای API نماینده" : "Reseller API keys", () => openModal(`${fa ? "کلیدهای API" : "API keys"} · ${r.Name || r.ID}`,
        resKeysCard(`/api/v2/admin/reps/${encodeURIComponent(r.ID)}/apikeys`)))] : []),
      item(t("edit"), () => repForm(r)),
      item(t("delete"), () => del(r), "bad"));
  });
  const rows = (reps || []).map((r) => {
    const o = byID[r.ID] || {};
    return h("tr", {},
      h("td", {}, h("a", { class: "name", href: `#/repview?rep=${encodeURIComponent(r.ID)}` }, r.Name || r.ID),
        h("div", { class: "small muted" }, r.ID, r.ParentID ? ` · ${t("subOf")} ${r.ParentID}` : "", r.Enabled ? "" : ` · ${t("disabled")}`)),
      h("td", { dir: "ltr" }, h("b", {}, gbs(r.VolumeCredit)), r.UnlimitedUnits ? h("div", { class: "small muted" }, `${fmtNum(r.UnlimitedUnits)} ${t("units")}`) : null),
      h("td", { dir: "ltr" }, gbs(o.month_sold || 0), h("div", { class: "small muted" }, `${fmtNum(o.month_sales || 0)} ${fa ? "فروش" : "sales"}`)),
      h("td", {}, h("span", { dir: "ltr" }, `${fmtNum(o.active || 0)} / ${fmtNum(r.Clients)}`), o.expiring ? h("div", { class: "small" }, h("span", { class: "badge warn" }, `${fmtNum(o.expiring)} ${fa ? "رو به اتمام" : "ending"}`)) : null),
      h("td", {}, fmtNum(o.online || 0)),
      h("td", {}, h("span", { dir: "ltr" }, bytes(r.UsedBytes || 0)), movedHere(r.UsedBytes, r.UsedHere) ? h("div", { class: "small", title: fa ? "فقط مصرف روی سرورهای این پنل، از ساخته‌شدن یا کوچ نماینده به اینجا؛ مصرف پنل قبلی حساب نیست" : "Only traffic on this panel's servers, since the reseller was made or moved here; the old panel's usage is left out" }, h("span", { class: "badge" }, fa ? "در پنل ما: " : "here: ", h("b", { dir: "ltr" }, bytes(r.UsedHere || 0)))) : null),
      h("td", { class: "small muted" }, o.last_login ? fmtWhen(o.last_login) : (fa ? "هرگز" : "never")),
      h("td", {}, h("div", { class: "row tight" },
        h("button", { onclick: () => creditForm(r) }, t("addCredit")),
        h("button", { class: "ghost", "aria-label": fa ? "بیشتر" : "More", onclick: menu(r) }, "⋯"))));
  });
  const tot = (k) => (ov || []).reduce((a, o) => a + (o[k] || 0), 0);
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("reps")), h("span", { class: "spacer" }),
      fullOwner() ? h("button", { onclick: () => partnerKeys(reps) }, "🔑 ", fa ? "API فروش پنل" : "Panel sales API") : null,
      fullOwner() ? h("button", { class: "ghost", onclick: () => openModal(fa ? "جذب نماینده از تلگرام" : "Resellers from Telegram", repGrowthBox()) }, "🧩 ", fa ? "جذب نماینده" : "Grow") : null,
      h("button", { class: "primary", onclick: () => repForm(null) }, t("add"))),
    (reps || []).length ? h("div", { class: "kpis" },
      h("div", { class: "card kpi" }, h("div", { class: "k" }, icon("reps"), fa ? "نماینده" : "Resellers"), h("div", { class: "v" }, fmtNum(reps.length))),
      h("div", { class: "card kpi" }, h("div", { class: "k" }, icon("finance"), fa ? "اعتبار دست نماینده‌ها" : "Credit held"), h("div", { class: "v", dir: "ltr" }, gbs(reps.reduce((a, r) => a + r.VolumeCredit, 0)))),
      h("div", { class: "card kpi" }, h("div", { class: "k" }, icon("shop"), fa ? "فروش این ماه" : "Sold this month"), h("div", { class: "v", dir: "ltr" }, gbs(tot("month_sold")))),
      h("div", { class: "card kpi" }, h("div", { class: "k" }, icon("clients"), fa ? "کلاینت فعال" : "Active clients"), h("div", { class: "v" }, fmtNum(tot("active")))),
      h("div", { class: "card kpi" }, h("div", { class: "k" }, icon("online"), fa ? "آنلاین" : "Online"), h("div", { class: "v" }, fmtNum(tot("online"))))) : null,
    rows.length ? h("div", { class: "card table-wrap" }, h("table", { class: "table reps-table" },
      h("thead", {}, h("tr", {}, ...[fa ? "نماینده" : "Reseller", t("volumeCredit"), fa ? "فروش این ماه" : "Sold this month", fa ? "فعال / کل" : "Active / all", fa ? "آنلاین" : "Online", fa ? "مصرف کل" : "Total traffic", fa ? "آخرین ورود" : "Last sign-in", ""].map((x) => h("th", {}, x)))),
      h("tbody", {}, ...rows))) : h("p", { class: "muted" }, t("empty")));
}

// Daily traffic (or any two daily series) on the chart kit: the first series
// as a filled line, the second dashed. Missing days are filled with zero so
// the x axis is real time; today's point is marked as not finished yet.
function barsSvg(days, W = 600, opt = {}) {
  const fa = lang === "fa";
  if (!days.length) return h("p", { class: "muted small" }, t("noData"));
  const by = Object.fromEntries(days.map((d) => [d.Day, d]));
  const first = days.map((d) => d.Day).sort()[0], today = todayUTC();
  const xs = [];
  for (let d = Date.parse(first + "T00:00:00Z"); xs.length < 400; d += 86400000) { const k = new Date(d).toISOString().slice(0, 10); if (k > today && xs.length) break; xs.push(k); }
  const series = [{ id: "d", name: opt.la || `↓ ${t("inRate")}`, color: "var(--ion)", area: true, values: xs.map((d) => by[d]?.Down || 0) }];
  if (!opt.single) series.push({ id: "u", name: opt.lb || `↑ ${t("outRate")}`, color: "var(--ember)", dash: true, values: xs.map((d) => by[d]?.Up || 0) });
  return chartKit({ xs, series, mode: "lines", partial: xs.at(-1) === today ? xs.length - 1 : -1, height: W >= 1000 ? 250 : 220, label: opt.la || t("usageChart"),
    tipExtra: opt.single ? null : (i) => h("div", { class: "ct-row tot" }, h("span", {}, fa ? "مجموع" : "Total"), h("span", { class: "v" }, bytes(series[0].values[i] + series[1].values[i]))) });
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

// A reseller's own subscription page and link address. path is
// /api/v2/rep/brand for the reseller, /api/v2/admin/reps/<id>/brand for owners.
function repBrandCard(path, repName) {
  const fa = lang === "fa";
  const err = h("div", { class: "error", role: "alert" });
  const card = h("div", { class: "card form" }, h("h2", {}, fa ? "صفحهٔ اشتراک اختصاصی" : "Own subscription page"), h("p", { class: "small muted" }, "…"));
  api("GET", path).then((v) => {
    const inp = (k, attrs = {}) => h("input", { value: v[k] || "", ...attrs });
    const title = inp("title", { placeholder: fa ? "مثلاً: فروشگاه علی VPN" : "e.g. Ali VPN shop" });
    const sup = inp("support_url", { dir: "ltr", placeholder: "https://t.me/your_support" });
    const tgId = inp("telegram", { dir: "ltr", placeholder: "@myshop" });
    const sub = me.role === "rep" && !me.can_sell; // a sub-seller
    const tpl = inp("remark_template", { dir: "ltr", placeholder: "{remark} · {left}" });
    const base = inp("base_url", { dir: "ltr", placeholder: "https://sub.myshop.com", list: "subdomain-choices" });
    // The panel's subscription addresses as suggestions (subdomains.go).
    const choices = h("datalist", { id: "subdomain-choices" });
    api("GET", "/api/v2/shop/sub-domain" + (path.includes("/reps/") ? `?rep=${encodeURIComponent(decodeURIComponent(path.split("/reps/")[1].split("/")[0]))}` : ""))
      .then((r) => choices.replaceChildren(...(r.choices || []).map((d) => h("option", { value: d.url }, d.label || d.url)))).catch(() => {});
    document.getElementById("subdomain-choices")?.remove(); document.body.append(choices);
    const ann = h("textarea", { rows: "2" }, v.announce || "");
    let logo = v.logo || "";
    const accentOn = h("input", { type: "checkbox", checked: !!v.accent });
    const accent = h("input", { type: "color", value: v.accent || "#2de2e6" });
    const logoPrev = h("img", { class: "logoprev", alt: "", hidden: !logo, src: logo || null });
    const logoIn = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp,image/svg+xml" });
    logoIn.addEventListener("change", () => {
      const f = logoIn.files[0];
      if (!f) return;
      if (f.size > 150000) { err.textContent = fa ? "لوگو باید کمتر از ۱۵۰ کیلوبایت باشد." : "The logo must be under 150 KB."; return; }
      const rd = new FileReader();
      rd.onload = () => { logo = rd.result; logoPrev.src = logo; logoPrev.hidden = false; };
      rd.readAsDataURL(f);
    });
    const save = h("button", { class: "primary", type: "button", onclick: async () => {
      err.textContent = "";
      try {
        await api("PUT", path, { title: title.value, support_url: sup.value, telegram: tgId.value, remark_template: tpl.value, base_url: base.value, announce: ann.value, accent: accentOn.checked ? accent.value : "", logo });
        toast(t("saved"));
      } catch (x) { err.textContent = x.message; }
    } }, fa ? "ذخیره" : "Save");
    const lab = (l, el, hint) => h("label", {}, h("span", {}, l), el, hint ? h("small", { class: "muted" }, hint) : null);
    fill(card, h("h2", {}, fa ? "صفحهٔ اشتراک اختصاصی" : "Own subscription page", repName ? h("span", { class: "badge", style: "margin-inline-start:8px" }, repName) : null),
      h("p", { class: "small muted" }, fa ? "مشتری‌های این نماینده (و زیرفروشنده‌هایش) صفحهٔ اشتراک و نام کانفیگ‌ها را با برند خود او می‌بینند. فیلدهای خالی از تنظیمات اصلی پنل می‌آیند." : "This reseller's customers (and their sub-sellers') see the subscription page and config names with this brand. Empty fields come from the panel's settings."),
      sub ? h("p", { class: "small", style: "color:var(--ion);margin:0" }, fa ? "مشتری‌های شما ربات نماینده یا پنل را نمی‌بینند؛ در صفحهٔ اشتراکشان دکمهٔ «پیام به فروشنده» با آیدی تلگرام شما و برند شما نشان داده می‌شود." : "Your customers never see the reseller's or the panel's bot; their subscription page shows a \"Message the seller\" button with your Telegram id and your brand.") : null,
      h("div", { class: "two" }, lab(fa ? "عنوان (نام برند)" : "Title (brand name)", title), lab(fa ? "آیدی تلگرام (پیوی شما)" : "Telegram id (your chat)", tgId, fa ? "مثلاً @myshop — مشتری با یک کلیک به پیوی شما می‌آید" : "e.g. @myshop — customers reach your chat in one tap")),
      lab(fa ? "لینک پشتیبانی دیگر (اگر آیدی تلگرام خالی است)" : "Other support link (when no Telegram id)", sup),
      lab(fa ? "اعلان (در صفحه و برنامه‌ها)" : "Announcement (page and apps)", ann),
      lab(fa ? "نام کانفیگ‌ها" : "Config names", tpl, fa ? "متغیرها: {remark} {left} {used} {total} {days} {email} {proto} {tag}" : "Variables: {remark} {left} {used} {total} {days} {email} {proto} {tag}"),
      h("div", { class: "row" }, h("label", { class: "check" }, accentOn, h("span", {}, fa ? "رنگ برند" : "Brand colour")), accent),
      h("div", { class: "row" }, logoPrev, h("label", {}, h("span", {}, fa ? "لوگو (زیر ۱۵۰ کیلوبایت)" : "Logo (under 150 KB)"), logoIn),
        h("button", { type: "button", class: "ghost", onclick: () => { logo = ""; logoPrev.hidden = true; logoIn.value = ""; } }, fa ? "حذف لوگو" : "Remove logo")),
      lab(fa ? "آدرس اختصاصی لینک‌ها (اختیاری)" : "Own link address (optional)", base,
        fa ? "دامنهٔ خودتان را (مثلاً از طریق Cloudflare) به آدرس اشتراک هاب وصل کنید؛ لینک‌های مشتری‌ها با این آدرس ساخته می‌شوند و نام پنل اصلی دیده نمی‌شود." : "Point your own domain (e.g. through Cloudflare) at the HUB's subscription address; customers' links use it and the main panel's name never shows."),
      err, h("div", { class: "row" }, save));
  }).catch((x) => fill(card, h("p", { class: "error" }, x.message)));
  return card;
}

// subDomainsCard: the subscription addresses (every one reaches the HUB's
// subscription port), the default one for new links, and moving everyone off
// a filtered address.
function subDomainsCard() {
  const fa = lang === "fa";
  const T = (a, b) => (fa ? a : b);
  const list = h("div", { class: "form" });
  const rows = [];
  let fallback = "";
  const addRow = (d) => {
    d = d || {};
    const url = h("input", { dir: "ltr", placeholder: "https://sub.example.com:2096", value: d.url || "" });
    const label = h("input", { placeholder: T("مثلاً: اصلی", "e.g. main"), value: d.label || "" });
    const def = h("input", { type: "radio", name: "subdom-default", checked: !!d.default });
    const ret = h("input", { type: "checkbox", checked: !!d.retired });
    const r = { url, label, def, ret };
    const box = h("div", { class: "card" },
      h("div", { class: "two" }, h("label", {}, h("span", {}, T("آدرس", "Address")), url), h("label", {}, h("span", {}, T("نام", "Label")), label)),
      h("div", { class: "row" },
        h("label", { class: "check" }, def, h("span", {}, T("پیش‌فرض لینک‌های جدید", "Default for new links"))),
        h("label", { class: "check" }, ret, h("span", {}, T("بازنشسته (کار می‌کند، ولی لینک جدید با آن ساخته نمی‌شود)", "Retired (still answers; no new links)"))),
        d.reps ? h("span", { class: "badge" }, T(`${fmtNum(d.reps)} نماینده`, `${d.reps} resellers`)) : null,
        h("button", { class: "ghost", type: "button", onclick: () => { rows.splice(rows.indexOf(r), 1); box.remove(); } }, T("حذف", "Remove"))));
    rows.push(r); list.append(box);
  };
  const moveFrom = h("select", {}), moveTo = h("select", {});
  const load = async () => {
    const r = await api("GET", "/api/v2/admin/sub-domains").catch(() => null);
    if (!r) return;
    fallback = r.fallback || "";
    list.replaceChildren(); rows.length = 0;
    (r.domains || []).forEach(addRow);
    if (!rows.length) addRow();
    const opts = (r.domains || []).map((d) => d.url);
    moveFrom.replaceChildren(...opts.map((u) => h("option", { value: u }, u)));
    moveTo.replaceChildren(...(r.domains || []).filter((d) => !d.retired).map((d) => h("option", { value: d.url }, d.url)));
    hint.textContent = T(`بدون آدرس پیش‌فرض، لینک‌ها با آدرس عمومی هاب ساخته می‌شوند: ${fallback}`, `Without a default, links use the HUB's public address: ${fallback}`);
  };
  const hint = h("p", { class: "small muted" });
  load();
  return h("div", { class: "card form" },
    h("h2", {}, "🌐 ", T("دامنه‌های ساب", "Subscription addresses")),
    h("p", { class: "small muted" }, T("همهٔ آدرس‌هایی که به پورت ساب هاب می‌رسند. لینک هر مشتری روی همهٔ آن‌ها کار می‌کند؛ لینک‌های جدید با آدرس پیش‌فرض ساخته می‌شوند و هر نماینده می‌تواند یکی را برای مشتری‌هایش انتخاب کند. فروشگاه‌های متصل آدرس خودشان را دارند و تغییری نمی‌کنند.",
      "Every address that reaches the HUB's subscription port. A customer's link works on all of them; new links use the default, and each reseller may pick one for its customers. Connected shops keep their own addresses.")),
    hint, list,
    h("div", { class: "row" }, h("button", { class: "ghost", type: "button", onclick: () => rows.length < 20 && addRow() }, T("+ آدرس", "+ Address")),
      h("button", { class: "primary", type: "button", onclick: async () => {
        const domains = rows.filter((r) => r.url.value.trim()).map((r) => ({ url: r.url.value, label: r.label.value, default: r.def.checked, retired: r.ret.checked }));
        await guarded(() => api("PUT", "/api/v2/admin/sub-domains", { domains })); toast(t("saved")); load();
      } }, t("save"))),
    h("h3", {}, T("آدرسی فیلتر شد؟", "An address got filtered?")),
    h("p", { class: "small muted" }, T("همهٔ نماینده‌هایی که روی آن آدرس‌اند (و پیش‌فرض، اگر همان باشد) به آدرس دیگر می‌روند و آدرس قبلی بازنشسته می‌شود. توکن لینک‌ها عوض نمی‌شود.",
      "Every reseller on that address (and the default, if it is that one) moves to another address, and the old one is retired. Link tokens do not change.")),
    h("div", { class: "row" }, h("span", {}, T("از", "From")), moveFrom, h("span", {}, T("به", "to")), moveTo,
      h("button", { class: "danger", type: "button", onclick: async () => {
        if (!moveFrom.value || !moveTo.value || moveFrom.value === moveTo.value) return;
        if (!(await confirmAction(T(`همه از ${moveFrom.value} به ${moveTo.value} منتقل شوند؟`, `Move everyone from ${moveFrom.value} to ${moveTo.value}?`)))) return;
        const r = await guarded(() => api("POST", "/api/v2/admin/sub-domains/move", { from: moveFrom.value, to: moveTo.value }));
        toast(T(`${fmtNum(r.moved)} نماینده منتقل شد`, `${r.moved} resellers moved`)); load();
      } }, T("انتقال", "Move"))));
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
    const smart = h("input", { type: "checkbox", checked: v.smart_order !== false });
    const hideDown = h("input", { type: "checkbox", checked: !!v.hide_down });
    const miniURL = h("input", { value: v.miniapp_url || "", dir: "ltr", placeholder: "https://panel.example.com/app/" });
    // Branding of the customer page: accent colour and a small logo (stored inline).
    let logo = v.logo || "";
    const accentOn = h("input", { type: "checkbox", checked: !!v.accent });
    const accent = h("input", { type: "color", value: v.accent || "#2de2e6" });
    const logoPrev = h("img", { class: "logoprev", alt: "", hidden: !logo, src: logo || null });
    const logoIn = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp,image/svg+xml" });
    logoIn.addEventListener("change", () => {
      const f = logoIn.files[0];
      if (!f) return;
      if (f.size > 150000) { err.textContent = fa ? "لوگو باید کمتر از ۱۵۰ کیلوبایت باشد." : "The logo must be under 150 KB."; return; }
      const rd = new FileReader();
      rd.onload = () => { logo = rd.result; logoPrev.src = logo; logoPrev.hidden = false; };
      rd.readAsDataURL(f);
    });
    const logoDel = h("button", { type: "button", class: "ghost", onclick: () => { logo = ""; logoPrev.hidden = true; logoIn.value = ""; } }, fa ? "حذف لوگو" : "Remove logo");
    fill(card, h("h2", {}, L.h), h("p", { class: "small muted" }, L.hint),
      h("label", {}, h("span", {}, L.title), title),
      h("label", {}, h("span", {}, L.tpl), tpl), h("div", { class: "small muted" }, L.vars), prev,
      h("label", { class: "check" }, info, h("span", {}, L.info)),
      h("label", {}, h("span", {}, L.ann), ann), h("label", {}, h("span", {}, L.sup), sup),
      h("label", { class: "check" }, frag, h("span", {}, L.frag)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, L.fl), fl), h("label", {}, h("span", {}, L.fi), fi)),
      h("label", { class: "check" }, noise, h("span", {}, L.noise)),
      h("h3", { class: "section" }, fa ? "ترتیب کانفیگ‌ها" : "Config order"),
      h("label", { class: "check" }, smart, h("span", {}, fa ? "بهترین سرور اول: کانفیگ‌ها بر اساس سلامت و بار نودها مرتب شوند" : "Best server first: order configs by node health and load")),
      h("label", { class: "check" }, hideDown, h("span", {}, fa ? "کانفیگ نودهای قطع از اشتراک کنار برود (وقتی دوباره وصل شد برمی‌گردد؛ هیچ‌وقت همه پنهان نمی‌شوند)" : "Leave out configs of nodes that are down (back when they recover; never all of them)")),
      h("h2", { class: "section" }, fa ? "ظاهر صفحهٔ مشتری" : "Customer page look"),
      h("div", { class: "row" }, h("label", { class: "check" }, accentOn, h("span", {}, fa ? "رنگ برند" : "Brand colour")), accent),
      h("div", { class: "row" }, logoPrev, h("label", {}, h("span", {}, fa ? "لوگو (PNG، JPG، WebP یا SVG، زیر ۱۵۰ کیلوبایت)" : "Logo (PNG, JPG, WebP or SVG, under 150 KB)"), logoIn), logoDel),
      h("h2", { class: "section" }, t("miniTitle")), h("p", { class: "small muted" }, t("miniHint")),
      h("label", { class: "check" }, mini, h("span", {}, t("miniOn"))),
      h("label", {}, h("span", {}, t("miniURL")), miniURL), err,
      h("button", { class: "primary", onclick: async () => {
        err.textContent = "";
        try { await guarded(() => api("PUT", "/api/v2/admin/sub-settings", { title: title.value, remark_template: tpl.value, info_lines: info.checked, announce: ann.value, support_url: sup.value,
          fragment: frag.checked, fragment_length: fl.value.trim(), fragment_interval: fi.value.trim(), noise: noise.checked, smart_order: smart.checked, hide_down: hideDown.checked, miniapp: mini.checked, miniapp_url: miniURL.value.trim(), accent: accentOn.checked ? accent.value : "", logo })); toast(t("saved")); }
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
    const user = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._\\-]{0,62}", value: l ? l.username : "", disabled: !!l, dir: "ltr" });
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

// refStatsView: how invitations are doing, for the owner.
function refStatsView(st) {
  const fa = lang === "fa";
  if (!st || !st.invited) return h("p", { class: "muted small" }, t("tcNoStats"));
  const pct = (a, b) => (b ? Math.round((100 * a) / b) : 0);
  const gb = (n) => fmtNum(+(n / 1073741824).toFixed(1));
  const tile = (v, l) => h("div", { class: "ref-tile" }, h("b", {}, v), h("span", {}, l));
  return h("div", {},
    h("div", { class: "ref-tiles" },
      tile(fmtNum(st.invited), fa ? "دعوت‌شده" : "Invited"),
      tile(`${fmtNum(st.trial)} (${fmtNum(pct(st.trial, st.invited))}${fa ? "٪" : "%"})`, fa ? "تست گرفتند" : "Took a trial"),
      tile(`${fmtNum(st.bought)} (${fmtNum(pct(st.bought, st.invited))}${fa ? "٪" : "%"})`, fa ? "خرید کردند" : "Bought"),
      tile(fmtNum(st.sales), fa ? "فروش از دعوت‌ها (تومان)" : "Sales from invites (toman)"),
      tile(fmtNum(st.toman + st.discounts), fa ? "هزینهٔ پاداش و تخفیف (تومان)" : "Rewards + discounts (toman)"),
      st.bytes ? tile(gb(st.bytes), fa ? "گیگ هدیه داده شد" : "GB given") : null,
      st.stars ? tile(fmtNum(st.stars) + "⭐", fa ? "پاداش استارز" : "Stars rewards") : null),
    (st.top || []).length ? h("div", { class: "table-wrap" }, h("table", { class: "ref-top" },
      h("thead", {}, h("tr", {}, ["#", fa ? "دعوت‌کننده" : "Inviter", fa ? "دعوت" : "Invited", fa ? "خرید" : "Bought", fa ? "پاداش (تومان)" : "Reward (toman)", fa ? "هدیه (گیگ)" : "Gift (GB)"].map((x) => h("th", {}, x)))),
      h("tbody", {}, st.top.map((v, i) => h("tr", {},
        h("td", {}, fmtNum(i + 1)), h("td", {}, h("span", {}, v.name || "—"), " ", h("span", { class: "small muted", dir: "ltr" }, String(v.chat))),
        h("td", {}, fmtNum(v.invited)), h("td", {}, fmtNum(v.bought)), h("td", {}, fmtNum(v.toman)), h("td", {}, v.bytes ? gb(v.bytes) : "—")))))) : null);
}

function tgCommerceCard() {
  const out = h("div", { class: "form" }, h("p", { class: "muted small" }, "…"));
  const card = h("div", { class: "card form" }, h("h2", {}, t("tcTitle")), out);
  Promise.all([api("GET", "/api/v2/admin/tg/commerce"), api("GET", "/api/v2/admin/inbounds")]).then(([c, inbounds]) => {
    const tr = c.trial || {}, rf = c.referral || {};
    const num = (v, attrs = {}) => h("input", { type: "number", min: "0", value: String(v ?? 0), dir: "ltr", ...attrs });
    const trOn = h("input", { type: "checkbox", checked: !!tr.enabled }), trGB = num(tr.mb ? +(tr.mb / 1024).toFixed(3) : tr.gb, { step: "any" }), trH = num(tr.hours || 24, { min: "1" }), trIPs = num(tr.max_ips ?? 1);
    const trIns = h("div", { class: "chips" }, (inbounds || []).map((i) => h("label", { class: "check" }, h("input", { type: "checkbox", value: i.tag, checked: (tr.inbounds || []).includes(i.tag) }), h("span", {}, i.tag))));
    const rfOn = h("input", { type: "checkbox", checked: !!rf.enabled }), rfPct = num(rf.percent ?? 10, { max: "100" }), rfFix = num(rf.fixed), rfMax = num(rf.max_reward);
    const rfFirst = h("input", { type: "checkbox", checked: !!rf.first_only });
    const rfInv = num(rf.invitee_percent, { max: "90" }), rfGB = num(rf.bonus_gb, { max: "1000" });
    const rfStats = h("div", { class: "ref-stats", role: "status" });
    api("GET", "/api/v2/admin/tg/referral/stats").then((st) => fill(rfStats, refStatsView(st))).catch(() => {});
    const err = h("div", { class: "error" });
    const chat = h("input", { inputmode: "numeric", dir: "ltr", placeholder: "123456789" }), delta = h("input", { dir: "ltr", placeholder: "+50" }), wout = h("div", { class: "small" });
    fill(out,
      h("h3", {}, t("tcTrial")), h("label", { class: "check" }, trOn, h("span", {}, t("tcTrialOn"))),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcGB")), trGB), h("label", {}, h("span", {}, t("tcHours")), trH)),
      h("label", {}, h("span", {}, t("maxIPs")), trIPs), trIns,
      h("h3", {}, t("tcRef")), h("label", { class: "check" }, rfOn, h("span", {}, t("tcRefOn"))),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcPercent")), rfPct), h("label", {}, h("span", {}, t("tcFixed")), rfFix)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcMax")), rfMax), h("label", { class: "check" }, rfFirst, h("span", {}, t("tcFirst")))),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tcInvitee")), rfInv), h("label", {}, h("span", {}, t("tcBonus")), rfGB)), err,
      h("button", { class: "primary", onclick: async () => {
        err.textContent = "";
        try {
          await api("PUT", "/api/v2/admin/tg/trial", { enabled: trOn.checked, ...(Number.isInteger(+trGB.value || 0) ? { gb: +trGB.value || 0, mb: 0 } : { gb: 0, mb: Math.max(1, Math.round(+trGB.value * 1024)) }), hours: +trH.value || 24, max_ips: +trIPs.value || 0, inbounds: [...trIns.querySelectorAll("input:checked")].map((x) => x.value) });
          await api("PUT", "/api/v2/admin/tg/referral", { enabled: rfOn.checked, percent: +rfPct.value || 0, fixed: +rfFix.value || 0, max_reward: +rfMax.value || 0, first_only: rfFirst.checked,
            invitee_percent: +rfInv.value || 0, bonus_gb: +rfGB.value || 0 });
          toast(t("saved"));
        } catch (x) { err.textContent = x.message; }
      } }, t("save")),
      h("h3", {}, t("tcStats")), rfStats,
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
  const dest = h("select", {}, h("option", { value: "" }, lang === "fa" ? "مالک (کاربرهای خود پنل)" : "Owner (the panel's own clients)"));
  api("GET", "/api/v2/admin/tenants").then((list) => (list || []).forEach((x) => dest.append(h("option", { value: x.id }, `${lang === "fa" ? "فروشگاه متصل" : "Connected shop"}: ${x.name}`)))).catch(() => {});
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
      inbounds: [...ins.querySelectorAll("input:checked")].map((x) => x.value).join(","), tenant: dest.value });
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
    h("label", {}, h("span", {}, lang === "fa" ? "مقصد" : "Destination"), dest),
    h("div", { class: "row" }, preview, imp), out);
}

function panelNameCard() {
  const fa = lang === "fa";
  const input = h("input", { value: panelName, maxlength: "40", required: true });
  const err = h("div", { class: "error", role: "alert" });
  return h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault(); err.textContent = "";
    try {
      const r = await api("PUT", "/api/v2/admin/panel-name", { name: input.value.trim() });
      panelName = r.name; safeSet("panelName", r.name); document.title = r.name;
      document.querySelectorAll(".brand-text").forEach((el) => el.replaceChildren(brandText()));
      toast(t("saved"));
    } catch (x) { err.textContent = x.message; }
  } },
    h("h2", {}, fa ? "نام پنل" : "Panel name"),
    h("p", { class: "small muted" }, fa ? "بالای پنل، صفحهٔ ورود و عنوان مرورگر نشان داده می‌شود. صفحهٔ اشتراک مشتری‌ها برند جداگانهٔ خودش را دارد." : "Shown at the top of the panel, on the sign-in page and as the browser title. Customers' subscription pages keep their own brand."),
    h("label", {}, h("span", {}, fa ? "نام" : "Name"), input), err,
    h("div", { class: "row" }, h("button", { class: "primary" }, t("save"))));
}

function panelPathCard() {
  const card = h("div", { class: "card form" });
  const draw = (cur) => {
    const input = h("input", { dir: "ltr", value: cur || "", placeholder: "my-door-82kq", pattern: "[A-Za-z0-9_\\-]{6,64}" });
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
    botOn: "وصل است", botOff: "وصل نیست", botToken: "توکن ربات", botTokenKeep: "ذخیره شده ({x}) — خالی یعنی بدون تغییر", botOwners: "شناسهٔ عددی مالک‌ها (با ＋ شناسهٔ بعدی را اضافه کنید)",
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
    botOn: "Connected", botOff: "Not connected", botToken: "Bot token", botTokenKeep: "saved ({x}) — empty keeps it", botOwners: "Owners' numeric ids (＋ adds another)",
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
    const owners = idList(st.owners || []);
    const err = h("div", { class: "error" });
    const save = h("button", { class: "primary", onclick: async () => {
      err.textContent = "";
      const ids = owners.get();
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
      h("label", {}, h("span", {}, s2("botToken")), tok), h("label", {}, h("span", {}, s2("botOwners")), owners.el), err,
      h("div", { class: "row" }, save,
        st.running ? h("button", { onclick: async () => { try { await api("POST", "/api/v2/admin/telegram/test"); toast(s2("botTestOK")); } catch (x) { err.textContent = x.message; } } }, s2("botTest")) : null,
        st.token_set ? h("button", { class: "ghost danger spacer", onclick: async () => {
          if (!(await confirmAction(s2("botOffAsk"), s2("botOffBtn")))) return;
          try { await api("PUT", "/api/v2/admin/telegram", { token: "-", owners: [] }); route(); } catch (x) { toast(t("failed", { e: x.message })); }
        } }, s2("botOffBtn")) : null),
      noticesBox(), alertsBox(),
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
function alertsBox() {
  const fa = lang === "fa";
  const login = h("input", { type: "checkbox" });
  const nodesOn = h("input", { type: "checkbox", checked: true });
  const partnerOn = h("input", { type: "checkbox", checked: true });
  const cpu = h("input", { type: "number", min: "0", max: "100", dir: "ltr", placeholder: "0" });
  const ram = h("input", { type: "number", min: "0", max: "100", dir: "ltr", placeholder: "0" });
  const rep = h("select", {}, [[1, fa ? "هر ساعت" : "Every hour"], [3, fa ? "هر ۳ ساعت" : "Every 3 hours"], [6, fa ? "هر ۶ ساعت" : "Every 6 hours"], [12, fa ? "هر ۱۲ ساعت" : "Every 12 hours"], [24, fa ? "روزانه" : "Daily"], [168, fa ? "هفتگی" : "Weekly"]].map(([v, l]) => h("option", { value: v }, l)));
  api("GET", "/api/v2/admin/telegram/alerts").then((r) => { login.checked = r.login; nodesOn.checked = r.nodes !== false; partnerOn.checked = r.partner !== false; cpu.value = r.cpu_percent || ""; ram.value = r.ram_percent || ""; rep.value = r.report_hours; }).catch(() => {});
  const save = h("button", { type: "button", onclick: async () => {
    await guarded(() => api("PUT", "/api/v2/admin/telegram/alerts", { login: login.checked, nodes: nodesOn.checked, partner: partnerOn.checked, cpu_percent: +cpu.value || 0, ram_percent: +ram.value || 0, report_hours: +rep.value }));
    toast(t("saved"));
  } }, t("save"));
  return h("div", { class: "form" }, h("h2", { class: "section" }, fa ? "هشدار به شما" : "Alerts for you"),
    h("p", { class: "small muted" }, fa ? "ربات این‌ها را برای شما (مالک) می‌فرستد. هشدار پردازنده و حافظه برای هاب و همهٔ نودهاست: وقتی دو بار پشت سر هم از حد بالاتر برود، حداکثر ساعتی یک بار، و وقتی عادی شد هم خبر می‌دهد." : "The bot sends these to you (the owner). CPU and memory alerts cover the HUB and every node: after two high readings in a row, at most once an hour, with a note when it calms down."),
    h("label", { class: "check" }, nodesOn, fa ? "قطع شدن نود یا Xray و وصل شدن دوباره" : "A node or its Xray going down, and coming back"),
    h("label", { class: "check" }, login, fa ? "خبر هر ورود به پنل (نام، IP و مرورگر)" : "Tell me about every panel sign-in (name, IP, browser)"),
    h("label", { class: "check" }, partnerOn, fa ? "کارهای API پارتنر (ساخت و حذف پنل، افزودن و پس گرفتن اعتبار)" : "Partner API activity (panels made or deleted, credit added or taken back)"),
    h("div", { class: "three" }, h("label", {}, h("span", {}, fa ? "هشدار پردازنده از (٪، ۰ = خاموش)" : "CPU alert at (%, 0 = off)"), cpu),
      h("label", {}, h("span", {}, fa ? "هشدار حافظه از (٪، ۰ = خاموش)" : "Memory alert at (%, 0 = off)"), ram),
      h("label", {}, h("span", {}, fa ? "گزارش وضعیت" : "Status report"), rep)),
    h("div", { class: "row" }, save));
}

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

// Moving from the old DARK XRAY panel: export on the old server, preview, import.
function darkOldSection() {
  const fa = lang === "fa";
  const box = h("div", { class: "form" });
  let timer = 0;
  const draw = async () => {
    clearTimeout(timer);
    if (!box.isConnected && timer) return;
    let st, ins;
    try { [st, ins] = await Promise.all([api("GET", "/api/v2/admin/migrate/darkold"), api("GET", "/api/v2/admin/inbounds")]); } catch { return; }
    if (!st.uploaded) {
      const out = h("div", { class: "form" });
      fill(box, h("p", { class: "small muted" }, fa ? "یک دستور می‌گیرید و روی سرور پنل قبلی اجرا می‌کنید. دیتابیس فقط خوانده می‌شود و روی آن سرور چیزی تغییر نمی‌کند؛ نماینده‌ها، کلاینت‌ها، لینک‌های Restore، فروشگاه، کیف پول‌ها و ربات‌ها یک بار به این‌جا فرستاده می‌شوند. بعد پیش‌نمایش می‌بینید و تصمیم می‌گیرید." : "You get a command to run on the old panel's server. Its database is only read and nothing there changes; resellers, clients, Restore links, shop, wallets and bots are sent here once. Then you preview and decide."),
        out, h("div", { class: "row" }, h("button", { onclick: async () => {
          const r = await guarded(() => api("POST", "/api/v2/admin/migrate/darkold/start"));
          fill(out, h("code", {}, r.command), h("div", { class: "row" }, h("button", { onclick: () => copy(r.command) }, t("copy")), h("span", { class: "small muted" }, fa ? "منتظر ارسال از سرور قبلی… (لینک ۲ ساعت اعتبار دارد)" : "Waiting for the upload… (the link works for 2 hours)")));
          timer = setTimeout(draw, 5000);
        } }, fa ? "ساخت دستور خروجی" : "Get the export command")));
      return;
    }
    const pick = (sel) => h("div", { class: "row" }, (ins || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: sel }), ib.tag)));
    const main = pick(true), rest = pick(true);
    const ownerBot = h("input", { type: "checkbox", checked: true });
    const dropBox = h("div", { class: "row" }, (st.resellers || []).map((id) => h("label", { class: "check" }, h("input", { type: "checkbox", value: id }), id)));
    const withRestore = h("input", { type: "checkbox" });
    const restLabel = h("label", { hidden: true }, h("span", {}, fa ? "اینباندهای کلاینت‌های Restore" : "Inbounds for the Restore clients"), rest);
    withRestore.addEventListener("change", () => { restLabel.hidden = !withRestore.checked; });
    const tags = (el) => [...el.querySelectorAll("input:checked")].map((x) => x.value);
    const rep = h("div", { class: "form" });
    const c = st.counts || {};
    const show = (r) => fill(rep,
      h("div", { class: "row" }, ...[[fa ? "نماینده" : "resellers", r.reps], [fa ? "ورود" : "logins", r.logins], [fa ? "کلاینت" : "clients", r.clients], ["Restore", r.restore], [fa ? "لینک ساب" : "links", r.links], [fa ? "مصرف" : "usage", r.usage], [fa ? "تلگرام" : "Telegram", r.telegram], [fa ? "غیرفعال" : "disabled", r.disabled], [fa ? "تمام‌شده (قابل تمدید)" : "ended (renewable)", r.ended], [fa ? "ربات نماینده" : "reseller bots", r.bots], [fa ? "کارت" : "cards", r.cards], [fa ? "کیف پول" : "wallets", r.wallets], [fa ? "پلن" : "plans", r.products], [fa ? "کلاینت جامانده" : "left behind", r.dropped], [fa ? "سفارش (سابقه)" : "orders (history)", r.orders], [fa ? "شارژ کیف پول (سابقه)" : "top-ups (history)", r.topups], [fa ? "ردیف کیف پول" : "wallet rows", r.wallet_rows], [fa ? "ردیف اعتبار" : "credit rows", r.ledger_rows]].map(([k, v]) => h("span", { class: "badge" }, `${k}: ${fmtNum(v || 0)}`))),
      (r.credit || []).length ? h("div", { class: "table-wrap" }, h("table", {}, h("thead", {}, h("tr", {}, ...(fa ? ["نماینده", "سقف", "در حال استفاده", "برگشتی منقضی‌ها", "اعتبار نهایی", "نامحدود قبلی (منتقل نمی‌شود)", "مصرف کل"] : ["Reseller", "Capacity", "In use", "Expired back", "Credit", "Old unlimited (not moved)", "Lifetime use"]).map((x) => h("th", {}, x)))),
        h("tbody", {}, ...r.credit.map((x) => h("tr", {}, h("td", {}, x.rep), h("td", {}, bytes(x.cap)), h("td", {}, bytes(x.in_use)), h("td", {}, bytes(x.expired_back)), h("td", {}, h("b", {}, bytes(x.credit))), h("td", {}, fmtNum(x.old_units)), h("td", {}, bytes(x.lifetime))))))) : null,
      r.unsettled ? h("p", { class: "small", style: "color:var(--warn)" }, fa ? `${fmtNum(r.unsettled)} سفارش یا شارژِ بازمانده در پنل قبلی (دستی تسویه شده) منتقل نمی‌شود.` : `${fmtNum(r.unsettled)} orders or top-ups left open in the old panel (settled by hand) are not moved.`) : null,
      r.main_bot ? h("p", { class: "small" }, (fa ? "ربات اصلی: " : "Main bot: ") + r.main_bot) : null,
      (r.domains || []).length ? h("p", { class: "small" }, (fa ? "این دامنه‌ها را بعد از انتقال به IP همین هاب ببرید تا لینک‌های قدیمی کار کنند: " : "Point these domains here after the move so old links keep working: ") + r.domains.join("، ")) : null,
      ...(r.notes || []).slice(0, 20).map((n) => h("div", { class: "small muted" }, "• " + n)),
      ...(r.skipped || []).slice(0, 20).map((n) => h("div", { class: "small", style: "color:var(--warn)" }, "• " + n)));
    const run = async (dry) => {
      const body = { inbounds: tags(main), restore_inbounds: tags(rest), include_restore: withRestore.checked, owner_bot: ownerBot.checked, drop_reps: tags(dropBox), dry_run: dry };
      if (!body.inbounds.length) { toast(fa ? "حداقل یک اینباند انتخاب کنید." : "Pick at least one inbound."); return; }
      if (!dry && !(await confirmAction(fa ? "انتقال انجام شود؟ کلاینت‌ها، نماینده‌ها و ورودها به این پنل اضافه می‌شوند." : "Import now? Clients, resellers and logins are added to this panel."))) return;
      const r = await guarded(() => api("POST", "/api/v2/admin/migrate/darkold/run", body));
      show(r);
      if (!dry) toast(fa ? "انتقال انجام شد ✅" : "Imported ✅");
    };
    fill(box,
      h("p", { class: "small" }, `${fa ? "فایل خروجی رسید" : "Export received"}: ${st.origin || ""} ${st.version ? "· " + st.version : ""}`),
      h("div", { class: "row" }, ...Object.entries(c).map(([k, v]) => h("span", { class: "badge" }, `${k}: ${fmtNum(v)}`))),
      h("label", {}, h("span", {}, fa ? "اینباندهای کلاینت‌ها" : "Inbounds for the clients"), main),
      h("label", { class: "check" }, withRestore, fa ? "کلاینت‌های Dark Restore هم منتقل شوند (منسوخ؛ معمولاً لازم نیست)" : "Also move the Dark Restore clients (retired; usually not needed)"),
      restLabel,
      (st.resellers || []).length ? h("label", {}, h("span", {}, fa ? "این نماینده‌ها با همهٔ کلاینت‌هایشان منتقل نشوند" : "Leave these resellers behind with all their clients"), dropBox) : null,
      h("label", { class: "check" }, ownerBot, fa ? "ربات خودِ مالک در پنل قبلی، ربات اصلی این پنل شود (ربات نماینده‌ها همیشه منتقل و روشن می‌شوند)" : "The owner's old bot becomes this panel's main bot (resellers' bots are always moved and turned on)"),
      h("p", { class: "small", style: "color:var(--warn)" }, fa ? "⚠️ یک ربات هم‌زمان روی دو پنل کار نمی‌کند: قبل از «انتقال»، پنل قبلی را خاموش کنید." : "⚠️ A bot cannot run on two panels at once: stop the old panel before importing."),
      h("div", { class: "row" }, h("button", { onclick: () => run(true) }, fa ? "پیش‌نمایش" : "Preview"), h("button", { class: "primary", onclick: () => run(false) }, fa ? "انتقال" : "Import"),
        h("button", { class: "ghost danger", onclick: async () => { await guarded(() => api("DELETE", "/api/v2/admin/migrate/darkold")); draw(); } }, fa ? "حذف فایل خروجی" : "Discard the export")),
      rep);
  };
  draw();
  return h("div", { class: "form" }, h("h2", { class: "section" }, fa ? "انتقال از پنل DARK XRAY قبلی" : "Move from the old DARK XRAY panel"), box);
}

// Full backup: the database plus the HUB's keys and settings, encrypted,
// so a new server can take over without reinstalling nodes.
function fullBackupSection() {
  const fa = lang === "fa";
  const pass = h("input", { type: "password", dir: "ltr", autocomplete: "new-password", minlength: "8", placeholder: fa ? "رمز بکاپ (حداقل ۸ کاراکتر)" : "Backup password (8+ characters)" });
  const err = h("div", { class: "error" });
  const btn = h("button", { class: "primary", onclick: async () => {
    err.textContent = "";
    if (pass.value.length < 8) { err.textContent = fa ? "رمز حداقل ۸ کاراکتر باشد." : "The password needs 8+ characters."; return; }
    btn.disabled = true;
    try {
      const res = await fetch("/api/v2/admin/backup/full", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-Dark-Request": "1" }, body: JSON.stringify({ password: pass.value }) });
      if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
      const blob = await res.blob();
      const name = (res.headers.get("Content-Disposition") || "").match(/filename="([^"]+)"/)?.[1] || "dark-v2-full.dkb";
      const a = h("a", { href: URL.createObjectURL(blob), download: name });
      document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 5000);
      pass.value = "";
    } catch (x) { err.textContent = x.message; }
    btn.disabled = false;
  } }, fa ? "دانلود بکاپ کامل" : "Download full backup");
  const drill = h("div", { class: "drill-box small" });
  const showDrill = (d) => {
    const r = d.last || {};
    const when = r.unix ? new Intl.DateTimeFormat(fa ? "fa-IR" : "en-GB", { dateStyle: "medium", timeStyle: "short" }).format(new Date(r.unix * 1000)) : "";
    const run = h("button", { class: "ghost", disabled: !d.has_password, onclick: async () => {
      run.disabled = true; run.textContent = fa ? "در حال آزمایش…" : "Testing…";
      try { const x = await api("POST", "/api/v2/admin/backup/drill"); showDrill({ last: x, has_password: true }); } catch (e) { toast(e.message); run.disabled = false; }
    } }, fa ? "آزمایش بازیابی الان" : "Test restore now");
    fill(drill,
      h("div", {}, h("b", {}, fa ? "آزمایش بازیابی: " : "Restore test: "),
        !r.unix ? h("span", { class: "muted" }, fa ? "هنوز انجام نشده" : "never run")
          : r.ok ? h("span", { class: "good" }, fa ? `✅ موفق · ${when} · ${fmtNum((r.counts?.clients || [0, 0])[1])} کاربر برگشت` : `✅ passed · ${when}`)
          : h("span", { class: "bad" }, `🔴 ${fa ? "ناموفق" : "failed"} · ${when} · ${r.error || ""}`)),
      h("div", { class: "muted" }, d.has_password ? (fa ? "هر هفته خودکار: بکاپ واقعی روی یک کپی جدا بازیابی و با پنل مقایسه می‌شود؛ به پنل اصلی دست نمی‌زند." : "Weekly: a real backup is restored into a throwaway copy and compared; the live panel is untouched.")
        : (fa ? "برای آزمایش خودکار، رمز بکاپ تلگرام را تنظیم کنید." : "Set the Telegram backup password to enable it.")),
      run);
  };
  api("GET", "/api/v2/admin/backup/drill").then(showDrill).catch(() => {});
  return h("div", { class: "form" },
    h("h2", { class: "section" }, fa ? "بکاپ کامل (برای سرور جدید)" : "Full backup (for a new server)"),
    drill,
    h("p", { class: "small muted" }, fa ? "دیتابیس به‌همراه گواهی هاب، توکن‌ها و تنظیمات سرویس، رمزگذاری‌شده. اگر سرور هاب از دست برود، روی سرور جدید با «dark-hub2 restore -file … -new-ip …» برمی‌گردد و نودها بدون نصب دوباره وصل می‌شوند. رمز را جایی امن نگه دارید؛ بدون آن فایل باز نمی‌شود." : "The database plus the HUB certificate, tokens and service settings, encrypted. If the HUB server is lost, restore it on a new one with “dark-hub2 restore -file … -new-ip …” and nodes reconnect without a reinstall. Keep the password safe; the file cannot be opened without it."),
    h("div", { class: "row" }, pass, btn), err);
}

// The address nodes use to reach the HUB (a domain survives a server move).
function hubAddrSection() {
  const fa = lang === "fa";
  const url = h("input", { dir: "ltr", placeholder: "https://hub.example.com:28443" });
  const out = h("div", { class: "form" });
  api("GET", "/api/v2/admin/hub-address").then((a) => { if (a && a.url) url.value = a.url; }).catch(() => {});
  const btn = h("button", { onclick: async () => {
    btn.disabled = true;
    try {
      const res = await api("POST", "/api/v2/admin/hub-address", { url: url.value.trim() });
      fill(out, ...(res || []).map((m) => h("div", { class: "small" }, m.task ? `✅ ${m.node}: ${fa ? "آدرس فرستاده شد؛ نود اول اتصال را چک می‌کند و بعد جابه‌جا می‌شود" : "sent; the node checks the address, then moves"}` : `⚠️ ${m.node}: ${m.reason}`)));
    } catch (x) { fill(out, h("div", { class: "error" }, x.message)); }
    btn.disabled = false;
  } }, fa ? "ذخیره و ارسال به نودها" : "Save and send to nodes");
  return h("div", { class: "form" },
    h("h2", { class: "section" }, fa ? "آدرس هاب برای نودها" : "HUB address for nodes"),
    h("p", { class: "small muted" }, fa ? "بهتر است یک دامنه باشد (رکورد A به IP هاب). هر نود قبل از جابه‌جایی چک می‌کند آدرس جدید واقعاً همین هاب است؛ اگر نبود، سر جایش می‌ماند. با دامنه، برای انتقال هاب فقط DNS را عوض می‌کنید." : "Best as a domain (an A record to the HUB's IP). Each node checks the new address really is this HUB before moving; if not, it stays. With a domain, moving the HUB is only a DNS change."),
    h("div", { class: "row" }, url, btn), out);
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
    fullBackupSection(), hubAddrSection(),
    h("h2", { class: "section" }, t("exportImport")), h("p", { class: "small muted" }, t("exportHint")),
    h("div", { class: "row" }, h("a", { class: "btn", href: "/api/v2/admin/clients/export", download: "" }, t("exportClients")), h("a", { class: "btn", href: "/api/v2/admin/clients/export.csv", download: "" }, lang === "fa" ? "خروجی اکسل (CSV)" : "Excel export (CSV)"),
      h("button", { onclick: () => importIn.click() }, t("importClients")), importIn), restoreSection(), darkOldSection()) : null;
  const tgOut = h("div", { class: "form" });
  const tgCard = me.role === "rep" ? h("div", { class: "card form" }, h("h2", {}, t("telegram")), h("p", { class: "small muted" }, t("telegramHint")),
    tgOut, h("button", { onclick: async () => {
      const r = await guarded(() => api("POST", "/api/v2/rep/telegram"));
      fill(tgOut, r.link ? h("a", { class: "btn", href: r.link, target: "_blank", rel: "noopener" }, t("openBot")) : null,
        h("code", {}, `/start rep_${r.code}`), h("p", { class: "small muted" }, t("codeExpires")));
    } }, t("connectTelegram"))) : null;
  const fa = lang === "fa";
  const owner = me.role === "owner";
  const lookCard = h("div", { class: "card form" }, h("h2", {}, fa ? "زبان و ظاهر" : "Language and look"),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("language")), langSel), h("label", {}, h("span", {}, t("theme")), themeSel)),
    h("div", { class: "row" }, h("button", { class: "danger", onclick: async () => { await api("POST", "/api/v2/auth/logout"); location.hash = "#/login"; } }, t("logout"))));
  // Settings are grouped; one group shows at a time so the page stays short.
  const groups = [
    owner && ["sub", "link", fa ? "اشتراک مشتری" : "Customer subscription", fa ? "عنوان، نام کانفیگ‌ها و صفحهٔ مشتری" : "Title, config names, customer page", () => [subSettingsCard(), subDomainsCard()]],
    (owner || fullOwner()) && ["panel", "globe", fa ? "آدرس و دسترسی پنل" : "Panel address and access", fa ? "HTTPS، مسیر مخفی، IPهای مجاز" : "HTTPS, secret path, trusted IPs", () => [fullOwner() ? panelNameCard() : null, fullOwner() ? panelHTTPSCard() : null, fullOwner() ? panelPathCard() : null, owner ? trustedCard() : null]],
    me.role === "rep" && ["repsub", "link", fa ? "صفحهٔ اشتراک من" : "My subscription page", fa ? "برند، لوگو و آدرس اختصاصی" : "Brand, logo and own address", () => [repBrandCard("/api/v2/rep/brand")]],
    me.role === "rep" && ["repapi", "link", fa ? "API فروش من" : "My sales API", fa ? "کلید برای ربات و سایت خودتان" : "Keys for your own bot and site", () => [resKeysCard("/api/v2/rep/apikeys")]],
    me.role === "rep" && ["repbot", "bell", fa ? "ربات فروش و کارت من" : "My sales bot and card", fa ? "ربات تلگرام اختصاصی، کارت به کارت" : "Own Telegram bot, card payment", () => [me.sub_seller ? null : mirzaCard("/api/v2/rep/pg-api", true), repBotCard("/api/v2/rep/bot"), botLookCard("/api/v2/rep/bot"), sellerCardCard("/api/v2/shop/card")]],
    ["security", "staff", fa ? "امنیت حساب" : "Account security", fa ? "رمز، ورود دومرحله‌ای، نشست‌ها" : "Password, two-step sign-in, sessions", () => [pwForm, tfaCard(), sessionsCard()]],
    (fullOwner() || owner || tgCard) && ["telegram", "bell", fa ? "تلگرام" : "Telegram", fa ? "ربات، هشدارها، فروش و دعوت" : "Bot, alerts, sales and referrals", () => [fullOwner() ? telegramBotCard() : null, fullOwner() ? botLookCard("/api/v2/admin/telegram") : null, owner ? tgCommerceCard() : null, fullOwner() ? mirzaCard("/api/v2/admin/pg-api", false) : null, tgCard]],
    fullOwner() && ["backup", "traffic", fa ? "پشتیبان و انتقال" : "Backup and migration", fa ? "دانلود، بازگردانی، ورود از پنل‌های دیگر" : "Download, restore, import from other panels", () => [backupCard, migrateCard(), foreignCard()]],
    ["look", "moon", fa ? "زبان و ظاهر" : "Language and look", fa ? "زبان، تم، خروج" : "Language, theme, sign out", () => [lookCard]],
  ].filter(Boolean);
  const want = new URLSearchParams(location.hash.split("?")[1] || "").get("s") || sessionStorage.getItem("setTab");
  let tab = groups.some((g) => g[0] === want) ? want : groups[0][0];
  const nav = h("nav", { class: "set-nav card", "aria-label": t("settings") });
  const pane = h("div", { class: "set-pane form" });
  const show = () => {
    try { sessionStorage.setItem("setTab", tab); } catch {}
    nav.replaceChildren(...groups.map(([k, ic, label, sub]) => h("button", { type: "button", class: "set-tab", "aria-current": k === tab ? "page" : null, onclick: () => { tab = k; show(); pane.scrollIntoView({ block: "nearest" }); } },
      icon(ic), h("span", { class: "set-tab-t" }, h("b", {}, label), h("small", {}, sub)))));
    const g = groups.find((x) => x[0] === tab);
    fill(pane, h("h2", { class: "set-title" }, g[2]), ...g[4]().filter(Boolean));
  };
  show();
  return h("div", {}, h("div", { class: "row sticky-head" }, h("h1", {}, t("settings"))), h("div", { class: "set-layout" }, nav, pane));
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
  const hidden = { client: [viewClient, "clients"], repview: [viewRepDashboard, "reps"], ...(me.legacy ? { legacy: [viewLegacy, "clients"] } : {}) };
  const view = hidden[name] ? hidden[name][0] : routes[name] || routes[first];
  try {
    const content = await view();
    if (seq !== routeSeq) return;
    app.replaceChildren(shell(hidden[name] ? hidden[name][1] : routes[name] ? name : first, content));
  } catch (e) {
    if (e.message !== "unauthorized") app.replaceChildren(shell(name, h("p", { class: "error" }, t("failed", { e: e.message }))));
  }
}

// A window (client, node health, confirm…) belongs to the page it was opened
// on: going to another page, or Back, closes it instead of leaving it on top.
window.addEventListener("hashchange", () => { const d = document.getElementById("modal"); if (d?.open) d.close("nav"); });
window.addEventListener("hashchange", route);
window.addEventListener("storage", (e) => {
  if (e.key === "color-scheme") document.querySelector('meta[name="color-scheme"]').content = e.newValue ?? "light dark";
});
applyLang();
route();
// Live refresh, but never under the user's hands: not while typing, with a
// menu open or rows ticked; the scroll position is kept.
function busyUI() {
  const a = document.activeElement;
  return (a && /^(INPUT|SELECT|TEXTAREA)$/.test(a.tagName) && a.type !== "checkbox") ||
    !!document.querySelector("#app details[open], #app .bulkbar:not([hidden])") ||
    (() => { try { return !!document.querySelector("[popover]:popover-open:not(#toast)"); } catch { return false; } })();
}
setInterval(async () => {
  if (document.hidden || document.getElementById("modal").open || busyUI() || !/dashboard|clients|nodes/.test(location.hash || "#/dashboard")) return;
  const y = window.scrollY;
  await route();
  window.scrollTo(0, y);
}, 15000);
window.addEventListener("hashchange", () => { if (!/^#\/clients?\b/.test(location.hash)) clientsMem = null; });

if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {});
