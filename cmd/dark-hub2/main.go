// Command dark-hub2 runs the v2 HUB core: the Node API over TLS plus a small
// admin CLI. The owner panel and business layer attach to the same store in
// a later phase.
//
//	dark-hub2 serve  -db v2.sqlite -listen :8443 -tls-dir /etc/dark-hub2
//	dark-hub2 node   -db v2.sqlite -id de-1 -name Germany        (prints token)
//	dark-hub2 inbound -db v2.sqlite -tag de-vless -node de-1 -file inbound.json
//	dark-hub2 client -db v2.sqlite -email alice -inbounds de-vless -volume-gb 100 -days 30 -first-use -max-ips 2
//	dark-hub2 sub    -db v2.sqlite -email alice                  (share links)
//	dark-hub2 status -db v2.sqlite
package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/tgbot"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/webui"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("dark-hub2", hubcore.HubVersion)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		hubcore.TrialStart(log.Printf)
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: dark-hub2 serve|node|inbound|client|sub|status [flags]")
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/dark-hub2/v2.sqlite", "v2 store")
	listen := fs.String("listen", ":8443", "node API listen address")
	tlsDir := fs.String("tls-dir", "/etc/dark-hub2", "CA and server certificate directory")
	adminListen := fs.String("admin-listen", "127.0.0.1:2097", "owner API and /sub listen address (put HTTPS in front)")
	subListen := fs.String("sub-listen", "", "public subscription-only listen address (optional)")
	distDir := fs.String("dist", "/opt/dark-v2/dist", "dark-node and xray binaries served to one-line installs")
	subBase := fs.String("sub-base", "", "public base URL for subscription links, e.g. http://1.2.3.4:28080")
	adminTokenFile := fs.String("admin-token-file", "/etc/dark-hub2/admin-token", "owner API bearer token file")
	hosts := fs.String("hosts", "", "comma-separated IPs/names for the server certificate")
	tgTokenFile := fs.String("tg-token-file", "/etc/dark-hub2/tg-token", "Telegram bot token file (bot disabled if missing)")
	tgOwners := fs.String("tg-owners", "", "comma-separated Telegram user IDs of the owners")
	id := fs.String("id", "", "node id")
	name := fs.String("name", "", "node display name / owner username")
	tag := fs.String("tag", "", "inbound tag")
	node := fs.String("node", "", "node id owning the inbound")
	file := fs.String("file", "", "inbound template JSON file")
	metaFile := fs.String("meta", "", "inbound client metadata JSON file (endpoints, REALITY public key)")
	email := fs.String("email", "", "client name")
	inbounds := fs.String("inbounds", "", "comma-separated inbound tags")
	volGB := fs.Float64("volume-gb", -1, "volume in GiB (-1 unlimited)")
	days := fs.Int("days", 0, "term in days (0 = none)")
	firstUse := fs.Bool("first-use", false, "term starts at first connection")
	maxIPs := fs.Int("max-ips", 0, "simultaneous IP limit (0 = none)")
	disabled := fs.Bool("disabled", false, "create the client disabled")
	apply := fs.Bool("apply", false, "migrate: import (default is a preview)")
	archived := fs.Bool("archived", false, "migrate: also move archived clients")
	newIP := fs.String("new-ip", "", "restore: this server's IP, replacing the old server's in the service settings")
	_ = fs.Parse(args)

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o700); err != nil {
		log.Fatal(err)
	}
	if cmd == "serve" {
		// A backup staged from the panel is swapped in before anything opens the database.
		if ok, err := hubcore.ApplyPendingRestore(*dbPath); err != nil {
			log.Fatalf("restore: %v", err)
		} else if ok {
			log.Printf("restored the backup staged from the panel; the previous database is kept as %s.before-restore-*", *dbPath)
		}
	}
	s, err := hubcore.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	hubcore.SetHubFiles(*tlsDir, hubcore.DefaultHubFiles...)
	hubcore.SetRestartHook(func() {
		log.Printf("restarting into the staged backup")
		s.Close()
		os.Exit(0)
	})
	s.SetDistDir(*distDir)
	hubcore.SetPublicBase(*subBase)
	ctx := context.Background()

	switch cmd {
	case "serve":
		cert, err := ensureTLS(*tlsDir, *hosts)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			for range time.Tick(time.Hour) {
				if err := s.Prune(ctx, 400); err != nil {
					log.Printf("prune: %v", err)
				}
			}
		}()
		go func() {
			// Daily snapshot, newest 14 kept, next to the database.
			for {
				if p, err := s.AutoBackup(ctx, filepath.Join(filepath.Dir(*dbPath), "backups"), 14); err != nil {
					log.Printf("auto backup: %v", err)
				} else {
					log.Printf("auto backup: %s", p)
				}
				time.Sleep(24 * time.Hour)
			}
		}()
		go s.RunACME(ctx)
		go s.RunGeo(ctx)
		go s.RunResets(ctx)
		go s.RunAutoRenew(ctx)
		go s.RunWebhooks(ctx)
		go s.RunSystemWatch(ctx)
		go s.RunInboundLimits(ctx)
		hubcore.SetHubIPs(*hosts)
		go s.RunLegacy(ctx, cert)
		var cliOwners []int64
		for _, f := range strings.Split(*tgOwners, ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil {
				cliOwners = append(cliOwners, n)
			}
		}
		hubcore.SetTelegramDefaultOwners(cliOwners)
		tgc := &tgController{store: s, file: *tgTokenFile, subBase: *subBase, parent: ctx}
		hubcore.SetTelegramControl(hubcore.TelegramControl{Apply: tgc.apply, Running: tgc.running, BackupNow: tgc.backupNow, Test: tgc.test})
		hubcore.SetOwnerNotify(tgc.alert)
		hubcore.SetCustomerNotify(tgc.tell)
		rbots := &repBots{store: s, subBase: *subBase, parent: ctx, running: map[string]*repBot{}}
		hubcore.SetSellerNotify(rbots.tell)
		hubcore.SetAppReceipt(func(ctx context.Context, seller string, chat int64, name, kind string, id int64, img []byte) error {
			b := tgc.current()
			if seller != "" {
				rbots.mu.Lock()
				if rb := rbots.running[seller]; rb != nil {
					b = rb.bot
				}
				rbots.mu.Unlock()
			}
			if b == nil {
				return fmt.Errorf("bot is off")
			}
			return b.AppReceipt(ctx, chat, name, kind, id, img)
		})
		// The bot a seller's Mini App speaks through: its own, or the main bot.
		sellerBot := func(seller string) *tgbot.Bot {
			if seller == "" {
				return tgc.current()
			}
			rbots.mu.Lock()
			defer rbots.mu.Unlock()
			if rb := rbots.running[seller]; rb != nil {
				return rb.bot
			}
			return nil
		}
		hubcore.SetAppBot(hubcore.AppBot{
			Audience: func(ctx context.Context, seller string) map[string]int {
				if b := sellerBot(seller); b != nil {
					return b.AppAudience(ctx)
				}
				return nil
			},
			Broadcast: func(ctx context.Context, seller string, chat int64, text, audience string) (int, error) {
				b := sellerBot(seller)
				if b == nil {
					return 0, fmt.Errorf("bot is off")
				}
				return b.AppBroadcast(ctx, chat, text, audience), nil
			},
		})
		go rbots.run(ctx)
		if tok, err := os.ReadFile(*tgTokenFile); err == nil && strings.TrimSpace(string(tok)) != "" {
			owners := s.TelegramOwners(ctx)
			if len(owners) == 0 {
				owners = cliOwners
			}
			tgc.start(strings.TrimSpace(string(tok)), owners)
			log.Printf("telegram bot on (%d owners)", len(owners))
		}
		if tok, err := os.ReadFile(*adminTokenFile); err == nil && *adminListen != "" {
			admin := &http.Server{Addr: *adminListen, Handler: hubcore.AdminHandlerWith(s, strings.TrimSpace(string(tok)), *subBase, webui.Handler(), func(m *http.ServeMux) {
				inst := hubcore.InstallHandler(s, hubcore.InstallConfig{DistDir: *distDir, PublicURL: *subBase, HubURL: "https://" + *listen, CAFile: filepath.Join(*tlsDir, "ca.pem")})
				m.Handle("GET /install/", inst)
				mig := hubcore.DarkOldHandler(s)
				m.Handle("GET /migrate/", mig)
				m.Handle("POST /migrate/", mig)
			}), ReadHeaderTimeout: 10 * time.Second}
			go func() {
				log.Printf("owner API and subscriptions on %s", *adminListen)
				log.Fatal(admin.ListenAndServe())
			}()
			// Optional HTTPS listener for the panel itself (set from the panel).
			if ph := s.PanelHTTPSSettings(ctx); ph.Port > 0 {
				psrv := &http.Server{Addr: ":" + strconv.Itoa(ph.Port), Handler: admin.Handler, ReadHeaderTimeout: 10 * time.Second,
					TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: s.PanelCertificate(cert)}}
				hubcore.SetPanelRunning(ph.Port)
				go func() {
					log.Printf("panel HTTPS on :%d", ph.Port)
					if err := psrv.ListenAndServeTLS("", ""); err != nil {
						log.Printf("panel HTTPS on :%d: %v", ph.Port, err)
						hubcore.SetPanelRunning(0)
					}
				}()
			}
		} else {
			log.Printf("owner API disabled: %s not readable", *adminTokenFile)
		}
		if *subListen != "" {
			subSrv := &http.Server{Addr: *subListen, Handler: hubcore.SubHandler(s), ReadHeaderTimeout: 10 * time.Second}
			go func() { log.Printf("subscriptions on %s", *subListen); log.Fatal(subSrv.ListenAndServe()) }()
		}
		srv := &http.Server{Addr: *listen, Handler: hubcore.NodeHandler(s), ReadHeaderTimeout: 10 * time.Second,
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}}
		log.Printf("dark-hub2 node API on %s (CA: %s)", *listen, filepath.Join(*tlsDir, "ca.pem"))
		log.Fatal(srv.ListenAndServeTLS("", ""))
	case "node":
		tok, err := s.CreateNode(ctx, *id, *name)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(tok)
	case "inbound":
		b, err := os.ReadFile(*file)
		if err != nil {
			log.Fatal(err)
		}
		var meta []byte
		if *metaFile != "" {
			if meta, err = os.ReadFile(*metaFile); err != nil {
				log.Fatal(err)
			}
		}
		if err := s.PutInboundMeta(ctx, *tag, *node, b, meta, true); err != nil {
			log.Fatal(err)
		}
	case "sub":
		links, info, err := s.Subscription(ctx, *email)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("# " + info)
		for _, l := range links {
			fmt.Println(l)
		}
	case "client":
		c := hubcore.Client{Email: *email, Enabled: !*disabled, VolumeBytes: -1, MaxIPs: *maxIPs}
		if *volGB >= 0 {
			c.VolumeBytes = int64(*volGB * (1 << 30))
		}
		if *days > 0 {
			if *firstUse {
				c.DurationSeconds = int64(*days) * 86400
			} else {
				c.ExpiresUnix = time.Now().Add(time.Duration(*days) * 24 * time.Hour).Unix()
			}
		}
		for _, t := range strings.Split(*inbounds, ",") {
			if t = strings.TrimSpace(t); t != "" {
				c.Inbounds = append(c.Inbounds, t)
			}
		}
		if err := s.PutClient(ctx, c); err != nil {
			log.Fatal(err)
		}
	case "admin":
		// dark-hub2 admin -db v2.sqlite -name owner   (reads the password from stdin)
		var pass string
		fmt.Fprint(os.Stderr, "password (10+ chars): ")
		if _, err := fmt.Scanln(&pass); err != nil {
			log.Fatal(err)
		}
		if err := s.SetAdmin(ctx, *name, pass); err != nil {
			log.Fatal(err)
		}
		// Server-side reset is also the recovery path for a lost authenticator.
		if err := s.ClearTOTP(ctx, *name); err != nil {
			log.Fatal(err)
		}
		// ...and for a forgotten secret panel address.
		if err := s.SetPanelPath(ctx, ""); err != nil {
			log.Fatal(err)
		}
		fmt.Println("owner login saved (two-step login and secret panel path cleared):", *name)
	case "migrate":
		// dark-hub2 migrate -db v2.sqlite [-file old.sqlite] -inbounds a,b [-archived] [-apply]
		// Reads the old panel's database (a private copy; the original is never
		// written). Without -apply it only reports what it would do.
		dir, err := os.MkdirTemp(filepath.Dir(*dbPath), "migrate-")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(dir)
		srcPath := *file
		if srcPath == "" {
			srcPath = hubcore.DefaultOldPanelDB
		}
		cp, err := hubcore.CopyOldDatabase(srcPath, dir)
		if err != nil {
			log.Fatal(err)
		}
		opt := hubcore.MigrateOptions{DryRun: !*apply, IncludeArchived: *archived}
		for _, t := range strings.Split(*inbounds, ",") {
			if t = strings.TrimSpace(t); t != "" {
				opt.Inbounds = append(opt.Inbounds, t)
			}
		}
		rep, err := s.MigrateFromOld(ctx, cp, opt)
		if err != nil {
			log.Fatal(err)
		}
		out, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(out))
		if opt.DryRun {
			fmt.Println("preview only — run again with -apply to import")
		}
		return
	case "restore":
		// dark-hub2 restore -db v2.sqlite -file backup.sqlite.gz   (stop the service first)
		s.Close()
		src := *file
		if data, err := os.ReadFile(src); err == nil && hubcore.IsEncryptedBackup(data) {
			// Encrypted (.dkb): the password is read from stdin.
			fmt.Fprint(os.Stderr, "backup password: ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			plain, err := hubcore.DecryptFile(src, strings.TrimRight(line, "\r\n"))
			if err != nil {
				log.Fatal(err)
			}
			defer os.Remove(plain)
			src = plain
		}
		if hubcore.IsFullBackup(src) {
			done, err := hubcore.RestoreFull(src, *dbPath, *tlsDir, *newIP)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("restored the database into", *dbPath, "and HUB files into", *tlsDir+":", strings.Join(done, ", "))
			unit := "/etc/systemd/system/dark-hub2.service"
			if _, err := os.Stat(unit); os.IsNotExist(err) {
				if err := os.WriteFile(unit, []byte(hubcore.HubUnit), 0o644); err == nil {
					fmt.Println("wrote", unit)
				}
			}
			fmt.Println("next: systemctl daemon-reload && systemctl enable --now dark-hub2")
			fmt.Println("nodes reconnect by themselves once the HUB address they use points here (a domain, or set it in the panel before moving)")
			return
		}
		if err := hubcore.RestoreBackup(src, *dbPath); err != nil {
			log.Fatal(err)
		}
		fmt.Println("restored", *file, "into", *dbPath)
		return
	case "status":
		nodes, err := s.Nodes(ctx)
		if err != nil {
			log.Fatal(err)
		}
		now := time.Now()
		for _, n := range nodes {
			state := "offline"
			if n.Online(now) {
				state = "online"
			}
			fmt.Printf("%-12s %-8s xray=%v restarts=%d rev=%d/%d spooled=%d usage_seq=%d agent=%s err=%q\n",
				n.ID, state, n.XrayRunning, n.XrayRestarts, n.ReportedRevision, n.ConfigRevision, n.Spooled, n.UsageSeq, n.AgentVersion, n.LastError)
		}
	default:
		log.Fatalf("unknown command %q", cmd)
	}
}

// ensureTLS creates a private CA and a server certificate once. Nodes pin
// ca.pem, so no public CA or domain is required.
func ensureTLS(dir, hosts string) (tls.Certificate, error) {
	certPath, keyPath, caPath := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key"), filepath.Join(dir, "ca.pem")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return c, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Dark HUB v2 CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	caCert, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "dark-hub2"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"dark-hub2"}}
	for _, h := range strings.Split(hosts, ",") {
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	write := func(p, typ string, b []byte) error {
		return os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), 0o600)
	}
	if err := write(caPath, "CERTIFICATE", caDER); err != nil {
		return tls.Certificate{}, err
	}
	if err := write(certPath, "CERTIFICATE", der); err != nil {
		return tls.Certificate{}, err
	}
	if err := write(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		return tls.Certificate{}, err
	}
	// The CA key is not kept: certificates are re-issued by deleting dir.
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// tgController runs the Telegram bot and lets the panel connect, change or
// stop it without restarting the HUB. The token stays in its own 0600 file.
type tgController struct {
	store   *hubcore.Store
	file    string
	subBase string
	parent  context.Context

	mu     sync.Mutex
	bot    *tgbot.Bot
	cancel context.CancelFunc
}

func (c *tgController) start(token string, owners []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel, c.bot = nil, nil
	}
	if token == "" {
		hubcore.SetTelegramToken("")
		hubcore.SetInvoiceLinker(nil)
		return
	}
	set := map[int64]bool{}
	for _, id := range owners {
		set[id] = true
	}
	hubcore.SetTelegramToken(token)
	st := c.store
	api := &tgbot.HTTPAPI{Token: token, Emoji: func() map[string]string { return st.EmojiTheme(context.Background()) }}
	hubcore.SetInvoiceLinker(api.InvoiceLink) // Mini App: pay with Telegram Stars
	ctx, cancel := context.WithCancel(c.parent)
	c.bot, c.cancel = &tgbot.Bot{Store: c.store, API: api, Owners: set, SubBase: c.subBase}, cancel
	go c.bot.Run(ctx)
}

func (c *tgController) apply(ctx context.Context, token string, owners []int64) (string, error) {
	if token == "-" {
		if err := os.Remove(c.file); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		c.start("", nil)
		return "", nil
	}
	if token == "" {
		b, err := os.ReadFile(c.file)
		if err != nil || strings.TrimSpace(string(b)) == "" {
			return "", fmt.Errorf("%w: enter the bot token from @BotFather", hubcore.ErrInvalid)
		}
		token = strings.TrimSpace(string(b))
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	name, err := (&tgbot.HTTPAPI{Token: token}).Me(cctx)
	if err != nil {
		return "", fmt.Errorf("%w: Telegram refused this token (%v)", hubcore.ErrInvalid, err)
	}
	if err := os.MkdirAll(filepath.Dir(c.file), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(c.file, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	c.start(token, owners)
	return name, nil
}

func (c *tgController) current() *tgbot.Bot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bot
}

func (c *tgController) running() bool { return c.current() != nil }

func (c *tgController) backupNow(ctx context.Context) error {
	if b := c.current(); b != nil {
		return b.BackupNow(ctx)
	}
	return fmt.Errorf("bot is off")
}

func (c *tgController) test(ctx context.Context) error {
	if b := c.current(); b != nil {
		return b.Test(ctx)
	}
	return fmt.Errorf("bot is off")
}

func (c *tgController) tell(ctx context.Context, chat int64, text string) {
	if b := c.current(); b != nil {
		b.Tell(ctx, chat, text)
	}
}

func (c *tgController) alert(ctx context.Context, text string) {
	if b := c.current(); b != nil {
		b.Alert(ctx, text)
	}
}

// repBots runs the resellers' own sales bots (set in the panel), checking
// every 20 seconds for bots added, changed or removed.
type repBots struct {
	store   *hubcore.Store
	subBase string
	parent  context.Context

	mu      sync.Mutex
	running map[string]*repBot
}

type repBot struct {
	token  string
	bot    *tgbot.Bot
	cancel context.CancelFunc
}

func (r *repBots) run(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		r.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *repBots) reconcile(ctx context.Context) {
	want := map[string]string{}
	for _, c := range r.store.RepBotsAll(ctx) {
		want[c.Rep] = c.Token
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for rep, b := range r.running {
		if want[rep] != b.token {
			b.cancel()
			delete(r.running, rep)
			hubcore.SetRepBotStatus(rep, false, "", "")
		}
	}
	for rep, tok := range want {
		if _, ok := r.running[rep]; ok {
			continue
		}
		st := r.store
		api := &tgbot.HTTPAPI{Token: tok, Emoji: func() map[string]string { return st.EmojiTheme(context.Background()) }}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		user, err := api.Me(cctx)
		cancel()
		if err != nil {
			hubcore.SetRepBotStatus(rep, false, "", err.Error())
			continue // tried again next round
		}
		bctx, bcancel := context.WithCancel(r.parent)
		b := &tgbot.Bot{Store: r.store, API: api, Owners: map[int64]bool{}, SubBase: r.subBase, Seller: rep}
		r.running[rep] = &repBot{token: tok, bot: b, cancel: bcancel}
		hubcore.SetRepBotStatus(rep, true, user, "")
		log.Printf("reseller bot for %s on (@%s)", rep, user)
		go b.Run(bctx)
	}
}

// tell messages a customer through a reseller's bot; false if it is off.
func (r *repBots) tell(ctx context.Context, seller string, chat int64, text string) bool {
	r.mu.Lock()
	b := r.running[seller]
	r.mu.Unlock()
	if b == nil {
		return false
	}
	go b.bot.Tell(context.WithoutCancel(ctx), chat, text)
	return true
}
