package hubcore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Moving from other panels: 3x-ui (x-ui.db) and Marzban / PasarGuard
// (db.sqlite3). The source file is only read. Clients keep their UUID and
// Trojan password, limits, expiry and usage, so their configs keep working
// once the inbound ports match; 3x-ui subscription ids and Marzban/PasarGuard
// subscription links keep answering with the new subscription.

// ForeignOptions says where the moved things go.
type ForeignOptions struct {
	Node     string   `json:"node"`     // node the imported inbounds run on (3x-ui, PasarGuard)
	Address  string   `json:"address"`  // address clients connect to (empty = the node's public IP)
	Inbounds []string `json:"inbounds"` // extra v2 inbounds every moved client gets (Marzban has no inbounds in its database)
	Prefix   string   `json:"prefix"`   // tag prefix for imported inbounds, default the panel kind
	DryRun   bool     `json:"dry_run"`
}

// ForeignReport tells what was (or would be) moved.
type ForeignReport struct {
	Kind     string   `json:"kind"` // 3x-ui | marzban | pasarguard
	DryRun   bool     `json:"dry_run"`
	Inbounds []string `json:"inbounds"`
	Clients  int      `json:"clients"`
	Disabled int      `json:"disabled"`
	Usage    int      `json:"usage"`
	Reps     int      `json:"reps"`
	Logins   int      `json:"logins"`
	Links    int      `json:"links"` // old subscription links that keep working
	Skipped  []string `json:"skipped"`
	Notes    []string `json:"notes"`

	marzbanSecret string   // written as a setting with the import
	subPaths      []string // 3x-ui subscription paths other than /sub/
}

func (r *ForeignReport) skip(f string, a ...any) { r.Skipped = append(r.Skipped, fmt.Sprintf(f, a...)) }
func (r *ForeignReport) note(f string, a ...any) { r.Notes = append(r.Notes, fmt.Sprintf(f, a...)) }

// DetectPanel names the panel a database file belongs to.
func DetectPanel(ctx context.Context, db *sql.DB) string {
	switch {
	case tableExists(ctx, db, "inbounds") && tableExists(ctx, db, "client_traffics"):
		return "3x-ui"
	case tableExists(ctx, db, "users") && tableExists(ctx, db, "proxies"):
		return "marzban"
	case tableExists(ctx, db, "users") && tableExists(ctx, db, "admins") && columnExists(ctx, db, "users", "proxy_settings"):
		return "pasarguard"
	case tableExists(ctx, db, "clients") && tableExists(ctx, db, "representatives"):
		return "dark-old"
	}
	return ""
}

func columnExists(ctx context.Context, db *sql.DB, table, col string) bool {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n == col {
			return true
		}
	}
	return false
}

// foreignClient is one customer gathered from the source panel.
type foreignClient struct {
	name, uuid, secret, note, repID, period string
	enabled                                 bool
	ips                                     int
	volume, expires, duration, created      int64
	up, down, lastSeen                      int64
	inbounds                                []string
	tg                                      int64
	subID                                   string // 3x-ui subId: becomes the v2 subscription token
	legacy                                  string // Marzban/PasarGuard username for old links
}

// foreignInbound is a server inbound converted to a v2 template.
type foreignInbound struct {
	tag, origin string
	port        int
	template    map[string]any
	meta        map[string]any
}

var subIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ImportForeign reads another panel's database copy and moves it here.
func (s *Store) ImportForeign(ctx context.Context, path string, opt ForeignOptions) (ForeignReport, error) {
	rep := ForeignReport{DryRun: opt.DryRun, Inbounds: []string{}, Skipped: []string{}, Notes: []string{}}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	src, err := sql.Open("sqlite", u.String())
	if err != nil {
		return rep, err
	}
	defer src.Close()
	rep.Kind = DetectPanel(ctx, src)
	var ins []foreignInbound
	var clients []foreignClient
	var admins []foreignAdmin
	switch rep.Kind {
	case "3x-ui":
		ins, clients, err = readXUI(ctx, src, &rep)
	case "marzban", "pasarguard":
		ins, clients, admins, err = readMarzban(ctx, src, rep.Kind, &rep)
	case "dark-old":
		return rep, fmt.Errorf("%w: this is the old Dark Xray Pro panel — use “Move from the old panel”", ErrInvalid)
	default:
		return rep, fmt.Errorf("%w: not a 3x-ui, Marzban or PasarGuard database (SQLite). For MySQL setups, export the database to SQLite first", ErrInvalid)
	}
	if err != nil {
		return rep, err
	}
	for _, tag := range opt.Inbounds {
		var n int
		if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, tag).Scan(&n); n == 0 {
			return rep, fmt.Errorf("%w: inbound %q does not exist", ErrInvalid, tag)
		}
	}
	address := strings.TrimSpace(opt.Address)
	if len(ins) > 0 {
		if opt.Node == "" {
			return rep, fmt.Errorf("%w: pick the node the %d imported inbounds run on", ErrInvalid, len(ins))
		}
		var pub string
		if err := s.ro.QueryRowContext(ctx, `SELECT public_ip FROM nodes WHERE id=?`, opt.Node).Scan(&pub); err != nil {
			return rep, fmt.Errorf("%w: node %q does not exist", ErrInvalid, opt.Node)
		}
		if address == "" {
			address = pub
		}
		if address == "" {
			return rep, fmt.Errorf("%w: node %s has not reported its public address yet — enter the address clients connect to", ErrInvalid, opt.Node)
		}
	}
	prefix := opt.Prefix
	if prefix == "" {
		prefix = map[string]string{"3x-ui": "xui", "marzban": "mz", "pasarguard": "pg"}[rep.Kind]
	}
	// Tags: prefix-origin, unique here.
	tagOf := map[string]string{}
	usedPorts := map[int]string{}
	if rows, err := s.ro.QueryContext(ctx, `SELECT tag,direct_port FROM inbound_routes WHERE node_id=?`, opt.Node); err == nil {
		for rows.Next() {
			var t string
			var p int
			if rows.Scan(&t, &p) == nil {
				usedPorts[p] = t
			}
		}
		rows.Close()
	}
	var placed []foreignInbound
	for _, in := range ins {
		tag := v2ID(prefix + "-" + in.origin)
		for i := 2; ; i++ {
			var n int
			_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, tag).Scan(&n)
			if n == 0 {
				break
			}
			tag = v2ID(fmt.Sprintf("%s-%s-%d", prefix, in.origin, i))
		}
		if other, ok := usedPorts[in.port]; ok {
			rep.skip("inbound %s: port %d on %s is already used by %s", in.origin, in.port, opt.Node, other)
			continue
		}
		usedPorts[in.port] = tag
		in.tag = tag
		tagOf[in.origin] = tag
		placed = append(placed, in)
		rep.Inbounds = append(rep.Inbounds, tag)
	}
	// Clients: names must be new here.
	var moved []foreignClient
	for _, c := range clients {
		c.name = v2Email(c.name)
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, c.name).Scan(&n)
		if n > 0 {
			rep.skip("client %s: already exists in this panel", c.name)
			continue
		}
		var tags []string
		for _, o := range c.inbounds {
			if t, ok := tagOf[o]; ok {
				tags = append(tags, t)
			}
		}
		c.inbounds = append(tags, opt.Inbounds...)
		moved = append(moved, c)
		rep.Clients++
		if !c.enabled {
			rep.Disabled++
		}
		if c.up+c.down > 0 {
			rep.Usage++
		}
		if c.subID != "" || c.legacy != "" {
			rep.Links++
		}
	}
	if len(placed) == 0 && len(opt.Inbounds) == 0 && len(moved) > 0 {
		rep.note("the moved clients have no inbound yet: pick inbounds to give them, or they get no connections")
	}
	for _, a := range admins {
		if a.sudo {
			rep.Logins++
		} else {
			rep.Reps++
			rep.Logins++
		}
	}
	if opt.DryRun {
		return rep, nil
	}

	// 1. Inbounds (each in its own write, removed again if anything fails).
	var created []string
	undo := func() {
		for _, t := range created {
			_ = s.DeleteInbound(context.WithoutCancel(ctx), t)
		}
	}
	for _, in := range placed {
		in.meta["endpoints"] = []any{map[string]any{"name": "Direct", "address": address, "port": in.port}}
		tb, _ := json.Marshal(in.template)
		mb, _ := json.Marshal(in.meta)
		if err := s.PutInboundMeta(ctx, in.tag, opt.Node, tb, mb, true); err != nil {
			undo()
			return rep, fmt.Errorf("inbound %s: %w", in.origin, err)
		}
		created = append(created, in.tag)
		if err := s.PutRoutes(ctx, in.tag, []Route{{NodeID: opt.Node, DirectPort: in.port,
			Hosts: []Host{{Path: "direct", Enabled: true, Address: address, Port: in.port}}}}); err != nil {
			undo()
			return rep, fmt.Errorf("inbound %s: %w", in.origin, err)
		}
	}
	// 2. Resellers, logins, clients, usage and links in one transaction.
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		undo()
		return rep, err
	}
	defer tx.Rollback()
	repOf := map[string]string{}
	for _, a := range admins {
		user := v2ID(a.user)
		var n int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE username=?`, user).Scan(&n)
		if n > 0 {
			rep.skip("login %s: already exists here", a.user)
			continue
		}
		role, repID := "admin", "" // a sudo admin of the old panel becomes a staff admin here, not an owner
		if !a.sudo {
			repID, role = v2ID(prefix+"-"+a.user), "rep"
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO reps(id,name,enabled,volume_credit,unlimited_units,created_unix) VALUES(?,?,?,?,?,?)`,
				repID, a.user, true, 0, 0, now); err != nil {
				undo()
				return rep, err
			}
			repOf[a.id] = repID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admins(username,pass_hash,created_unix,role,rep_id) VALUES(?,?,?,?,?)`, user, a.hash, now, role, repID); err != nil {
			undo()
			return rep, fmt.Errorf("login %s: %w", a.user, err)
		}
	}
	links := 0
	for _, c := range moved {
		cl := Client{Email: c.name, UUID: strings.ToLower(c.uuid), Secret: c.secret, Enabled: c.enabled, VolumeBytes: c.volume, ExpiresUnix: c.expires,
			DurationSeconds: c.duration, MaxIPs: max(c.ips, 0), Inbounds: c.inbounds}
		if !uuidRE.MatchString(cl.UUID) {
			cl.UUID = ""
		}
		if c.subID != "" && subIDRE.MatchString(c.subID) {
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE sub_token=?`, c.subID).Scan(&n)
			if n == 0 {
				cl.SubToken = c.subID // the old 3x-ui link /sub/<subId> keeps working
				links++
			}
		}
		if err := putClientTx(ctx, tx, cl, now); err != nil {
			undo()
			return rep, fmt.Errorf("client %s: %w", c.name, err)
		}
		repID := repOf[c.repID]
		plan := "volume"
		if cl.VolumeBytes < 0 {
			plan = "unlimited"
		}
		if c.created <= 0 {
			c.created = now
		}
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=?, plan=?, note=?, created_unix=? WHERE email=?`, repID, plan, c.note, c.created, c.name); err != nil {
			undo()
			return rep, err
		}
		if resetPeriods[c.period] && c.period != "" {
			col, year := resetColumns(c.period)
			_, _ = tx.ExecContext(ctx, `UPDATE clients SET reset_period=?, reset_year=?, last_reset_unix=? WHERE email=?`, col, year, now, c.name)
		}
		if c.up+c.down > 0 {
			// A term that starts at the first connection must not start
			// because of usage carried over, so those get no "seen" time.
			first, last := min(c.created, now), max(c.lastSeen, c.created)
			if c.duration > 0 {
				first, last = 0, 0
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)`, c.name, c.up, c.down, first, last); err != nil {
				undo()
				return rep, err
			}
		}
		if c.tg > 0 {
			_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO tg_links(chat_id,email) VALUES(?,?)`, c.tg, c.name)
		}
		if c.legacy != "" {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO legacy_sub_tokens(token_sha,kind,email) VALUES(?,?,?)`, legacyTokenSHA("marzban-user", c.legacy), "marzban-user", c.name); err != nil {
				undo()
				return rep, err
			}
			links++
		}
	}
	if err := tx.Commit(); err != nil {
		undo()
		return rep, err
	}
	if rep.marzbanSecret != "" {
		if old := s.Setting(ctx, "legacy_marzban_secret"); old != "" && old != rep.marzbanSecret {
			rep.note("links of an earlier Marzban/PasarGuard import stop working: only one old panel's links can be kept")
		}
		_ = s.SetSetting(ctx, "legacy_marzban_secret", rep.marzbanSecret)
	}
	if len(rep.subPaths) > 0 {
		have := strings.Split(s.Setting(ctx, "legacy_sub_paths"), ",")
		for _, p := range rep.subPaths {
			if !contains(have, p) {
				have = append(have, p)
			}
		}
		_ = s.SetSetting(ctx, "legacy_sub_paths", strings.Trim(strings.Join(have, ","), ","))
	}
	rep.Links = links
	return rep, nil
}

type foreignAdmin struct {
	id, user, hash string
	sudo           bool
}

// ---------- 3x-ui ----------

// xuiStream turns a 3x-ui streamSettings into a server template part and
// the client metadata 3x-ui keeps inside it (realitySettings.settings).
func xuiStream(raw string, meta map[string]any, rep *ForeignReport, origin string) map[string]any {
	var st map[string]any
	if json.Unmarshal([]byte(raw), &st) != nil || st == nil {
		return nil
	}
	if rs, _ := st["realitySettings"].(map[string]any); rs != nil {
		if cs, _ := rs["settings"].(map[string]any); cs != nil {
			if v, _ := cs["publicKey"].(string); v != "" {
				meta["public_key"] = v
			}
			if v, _ := cs["fingerprint"].(string); v != "" {
				meta["fingerprint"] = v
			}
			if v, _ := cs["spiderX"].(string); v != "" && v != "/" {
				meta["spider_x"] = v
			}
		}
		delete(rs, "settings")
		if ids, _ := rs["shortIds"].([]any); len(ids) > 0 {
			for _, x := range ids {
				if s, _ := x.(string); s != "" {
					meta["short_id"] = s
					break
				}
			}
		}
		if meta["public_key"] == nil {
			if priv, _ := rs["privateKey"].(string); priv != "" {
				if k, err := GenerateKeys(KeyRequest{Kind: "reality", Private: priv}); err == nil {
					meta["public_key"] = k.Public
				}
			}
		}
	}
	if ts, _ := st["tlsSettings"].(map[string]any); ts != nil {
		if cs, _ := ts["settings"].(map[string]any); cs != nil {
			if v, _ := cs["fingerprint"].(string); v != "" {
				meta["fingerprint"] = v
			}
			if v, _ := cs["allowInsecure"].(bool); v {
				meta["insecure"] = true
			}
		}
		delete(ts, "settings")
		if certs, _ := ts["certificates"].([]any); len(certs) > 0 {
			for _, c := range certs {
				if m, _ := c.(map[string]any); m != nil && (m["certificateFile"] != nil || m["keyFile"] != nil) {
					rep.note("inbound %s: its TLS certificate is a file on the old server — upload it under Inbounds → Certificates and pick it in the inbound's settings", origin)
					break
				}
			}
		}
	}
	delete(st, "externalProxy")
	return st
}

func readXUI(ctx context.Context, db *sql.DB, rep *ForeignReport) ([]foreignInbound, []foreignClient, error) {
	traffic := map[string][3]int64{} // email -> up, down, last online (unix)
	hasLast := columnExists(ctx, db, "client_traffics", "last_online")
	q := `SELECT email,up,down,0 FROM client_traffics`
	if hasLast {
		q = `SELECT email,up,down,COALESCE(last_online,0) FROM client_traffics`
	}
	if rows, err := db.QueryContext(ctx, q); err == nil {
		for rows.Next() {
			var e string
			var up, down, last int64
			if rows.Scan(&e, &up, &down, &last) == nil {
				if last > 1e12 {
					last /= 1000
				}
				traffic[e] = [3]int64{up, down, last}
			}
		}
		rows.Close()
	}
	rows, err := db.QueryContext(ctx, `SELECT id,COALESCE(remark,''),enable,COALESCE(listen,''),port,protocol,COALESCE(settings,''),COALESCE(stream_settings,''),COALESCE(sniffing,''),COALESCE(tag,'') FROM inbounds ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	type xin struct {
		id                                                  int
		remark, listen, proto, settings, stream, sniff, tag string
		enable                                              bool
		port                                                int
	}
	var list []xin
	for rows.Next() {
		var x xin
		if err := rows.Scan(&x.id, &x.remark, &x.enable, &x.listen, &x.port, &x.proto, &x.settings, &x.stream, &x.sniff, &x.tag); err != nil {
			rows.Close()
			return nil, nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	if tableExists(ctx, db, "settings") {
		for _, k := range []string{"subPath", "subJsonPath"} {
			var v string
			if db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, k).Scan(&v) == nil {
				v = "/" + strings.Trim(v, "/") + "/"
				if v != "/sub/" && v != "//" && legacyPathRE.MatchString(v) {
					if k == "subJsonPath" {
						v = "json:" + v
					}
					rep.subPaths = append(rep.subPaths, v)
				}
			}
		}
	}
	var ins []foreignInbound
	bySub := map[string]*foreignClient{}
	var order []string
	for _, x := range list {
		origin := strconv.Itoa(x.port)
		switch x.proto {
		case "vless", "vmess", "trojan", "shadowsocks":
		case "socks", "http", "dokodemo-door", "tunnel", "mixed":
			// Plain proxies and port forwards move as written.
			var set map[string]any
			_ = json.Unmarshal([]byte(x.settings), &set)
			if set == nil {
				set = map[string]any{}
			}
			proto := x.proto
			if proto == "mixed" {
				proto = "socks" // Xray's SOCKS inbound also answers HTTP proxy requests
			}
			meta := map[string]any{"remark": or2(x.remark, origin)}
			if accs, _ := set["accounts"].([]any); len(accs) > 0 {
				if a, _ := accs[0].(map[string]any); a != nil {
					meta["proxy_user"], meta["proxy_pass"] = a["user"], a["pass"]
				}
			}
			if (proto == "socks" || proto == "http") && meta["proxy_user"] == nil {
				rep.note("inbound %s: a %s proxy without a password is open to anyone — add an account in its settings", or2(x.remark, origin), proto)
			}
			if addr, _ := set["address"].(string); addr != "" {
				meta["target"] = addr + ":" + strconv.Itoa(intOf(set["port"]))
			}
			tpl := map[string]any{"protocol": proto, "port": x.port, "listen": or2(x.listen, "0.0.0.0"), "settings": set}
			ins = append(ins, foreignInbound{origin: origin, port: x.port, template: tpl, meta: meta})
			continue
		default:
			rep.skip("inbound %s (%s on port %d): %s inbounds are not moved yet", or2(x.remark, x.tag), x.proto, x.port, x.proto)
			continue
		}
		var set map[string]any
		if json.Unmarshal([]byte(x.settings), &set) != nil || set == nil {
			rep.skip("inbound %s: unreadable settings", or2(x.remark, x.tag))
			continue
		}
		meta := map[string]any{"remark": or2(x.remark, "Dark VPN"), "fingerprint": "chrome"}
		stream := xuiStream(x.stream, meta, rep, or2(x.remark, origin))
		tpl := map[string]any{"protocol": x.proto, "port": x.port, "listen": or2(x.listen, "0.0.0.0")}
		if stream != nil {
			tpl["streamSettings"] = stream
		}
		var sniff map[string]any
		if json.Unmarshal([]byte(x.sniff), &sniff) == nil && sniff != nil {
			tpl["sniffing"] = sniff
		}
		clientsRaw, _ := set["clients"].([]any)
		delete(set, "clients")
		flow := ""
		ssMethod, _ := set["method"].(string)
		if x.proto == "shadowsocks" {
			if !strings.HasPrefix(ssMethod, "2022-") {
				rep.skip("inbound %s: Shadowsocks %s (not 2022) cannot be shared per client here", or2(x.remark, origin), ssMethod)
				continue
			}
			rep.note("inbound %s: Shadowsocks client keys are made new here — those customers refresh their subscription once", or2(x.remark, origin))
		}
		for _, cr := range clientsRaw {
			m, _ := cr.(map[string]any)
			if m == nil {
				continue
			}
			email, _ := m["email"].(string)
			if email == "" {
				continue
			}
			if f, _ := m["flow"].(string); f != "" && flow == "" {
				flow = f
			}
			sub, _ := m["subId"].(string)
			key := "s:" + sub
			if sub == "" {
				key = "e:" + email
			}
			c := bySub[key]
			if c == nil {
				c = &foreignClient{name: email, enabled: true, volume: -1, subID: sub}
				bySub[key] = c
				order = append(order, key)
			}
			if id, _ := m["id"].(string); id != "" && c.uuid == "" {
				c.uuid = id
			}
			if pw, _ := m["password"].(string); pw != "" && x.proto == "trojan" && c.secret == "" {
				c.secret = pw
			}
			if en, ok := m["enable"].(bool); ok && !en {
				c.enabled = false
			}
			if !x.enable {
				c.enabled = false
			}
			if n := intOf(m["limitIp"]); n > c.ips {
				c.ips = n
			}
			if v := int64(floatOf(m["totalGB"])); v > 0 {
				if c.volume < 0 {
					c.volume = 0
				}
				c.volume += v
			}
			switch e := int64(floatOf(m["expiryTime"])); {
			case e > 0:
				c.expires = max(c.expires, e/1000)
			case e < 0:
				c.duration = max(c.duration, -e/1000)
			}
			if tg := fmt.Sprint(m["tgId"]); tg != "" && tg != "<nil>" && tg != "0" {
				if n, err := strconv.ParseInt(strings.TrimSpace(tg), 10, 64); err == nil && n > 0 {
					c.tg = n
				}
			}
			if cm, _ := m["comment"].(string); cm != "" && c.note == "" {
				c.note = cm
			}
			if t, ok := traffic[email]; ok {
				c.up += t[0]
				c.down += t[1]
				c.lastSeen = max(c.lastSeen, t[2])
			}
			c.inbounds = append(c.inbounds, origin)
		}
		if flow != "" {
			set["flow"] = flow
		}
		tpl["settings"] = set
		ins = append(ins, foreignInbound{origin: origin, port: x.port, template: tpl, meta: meta})
	}
	clients := make([]foreignClient, 0, len(order))
	for _, k := range order {
		c := bySub[k]
		if c.expires > 0 && c.duration > 0 {
			c.duration = 0
		}
		clients = append(clients, *c)
	}
	return ins, clients, nil
}

func floatOf(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// ---------- Marzban / PasarGuard ----------

func readMarzban(ctx context.Context, db *sql.DB, kind string, rep *ForeignReport) ([]foreignInbound, []foreignClient, []foreignAdmin, error) {
	// Admins: sudo admins become staff admins; the others own their users
	// and become resellers. bcrypt hashes keep working.
	var admins []foreignAdmin
	if rows, err := db.QueryContext(ctx, `SELECT id,username,hashed_password,COALESCE(is_sudo,0) FROM admins`); err == nil {
		for rows.Next() {
			var a foreignAdmin
			var id int64
			if rows.Scan(&id, &a.user, &a.hash, &a.sudo) == nil && strings.HasPrefix(a.hash, "$2") {
				a.id = strconv.FormatInt(id, 10)
				admins = append(admins, a)
			}
		}
		rows.Close()
	}
	cols := []string{"id", "username", "status", "COALESCE(used_traffic,0)", "COALESCE(data_limit,0)", "expire", "COALESCE(note,'')", "COALESCE(admin_id,0)", "COALESCE(data_limit_reset_strategy,'')"}
	hasHold := columnExists(ctx, db, "users", "on_hold_expire_duration")
	if hasHold {
		cols = append(cols, "COALESCE(on_hold_expire_duration,0)")
	} else {
		cols = append(cols, "0")
	}
	proxyCol := "''"
	if kind == "pasarguard" {
		proxyCol = "COALESCE(proxy_settings,'')"
	}
	cols = append(cols, proxyCol, "COALESCE(CAST(created_at AS TEXT),'')")
	rows, err := db.QueryContext(ctx, `SELECT `+strings.Join(cols, ",")+` FROM users ORDER BY id`)
	if err != nil {
		return nil, nil, nil, err
	}
	type mu struct {
		id                         int64
		name, status, note, period string
		used, limit, admin, hold   int64
		expire                     sql.NullString
		proxies, created           string
	}
	var users []mu
	for rows.Next() {
		var u mu
		if err := rows.Scan(&u.id, &u.name, &u.status, &u.used, &u.limit, &u.expire, &u.note, &u.admin, &u.period, &u.hold, &u.proxies, &u.created); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		users = append(users, u)
	}
	rows.Close()
	// Marzban keeps credentials in proxies(user_id,type,settings).
	creds := map[int64][2]string{} // uuid, trojan password
	if kind == "marzban" {
		if pr, err := db.QueryContext(ctx, `SELECT user_id,type,settings FROM proxies`); err == nil {
			for pr.Next() {
				var uid int64
				var typ, set string
				if pr.Scan(&uid, &typ, &set) != nil {
					continue
				}
				var m map[string]any
				_ = json.Unmarshal([]byte(set), &m)
				c := creds[uid]
				switch strings.ToLower(typ) {
				case "vless", "vmess":
					if id, _ := m["id"].(string); id != "" && c[0] == "" {
						c[0] = id
					}
				case "trojan":
					if pw, _ := m["password"].(string); pw != "" {
						c[1] = pw
					}
				}
				creds[uid] = c
			}
			pr.Close()
		}
	}
	var clients []foreignClient
	for _, u := range users {
		c := foreignClient{name: u.name, enabled: u.status == "active" || u.status == "on_hold", volume: -1, note: u.note, legacy: u.name}
		if u.status == "limited" || u.status == "expired" {
			c.enabled = true // the limit itself stops them here too; renewing just works
		}
		if u.limit > 0 {
			c.volume = u.limit
		}
		c.down = u.used
		if u.expire.Valid && u.expire.String != "" {
			c.expires = parseMarzbanTime(u.expire.String)
		}
		if u.status == "on_hold" && u.hold > 0 {
			c.expires, c.duration = 0, u.hold
		}
		c.created = parseMarzbanTime(u.created)
		switch u.period {
		case "day", "week", "month", "year":
			c.period = u.period
		}
		if u.admin > 0 {
			c.repID = strconv.FormatInt(u.admin, 10)
		}
		cr := creds[u.id]
		if kind == "pasarguard" && u.proxies != "" {
			var ps map[string]map[string]any
			if json.Unmarshal([]byte(u.proxies), &ps) == nil {
				for _, k := range []string{"vless", "vmess"} {
					if id, _ := ps[k]["id"].(string); id != "" && cr[0] == "" {
						cr[0] = id
					}
				}
				if pw, _ := ps["trojan"]["password"].(string); pw != "" {
					cr[1] = pw
				}
			}
		}
		c.uuid, c.secret = cr[0], cr[1]
		clients = append(clients, c)
	}
	// Old subscription links: Marzban/PasarGuard sign them with the secret
	// in the jwt table; keep it so they can still be checked.
	if tableExists(ctx, db, "jwt") {
		var secret string
		if db.QueryRowContext(ctx, `SELECT secret_key FROM jwt LIMIT 1`).Scan(&secret) == nil && secret != "" {
			rep.marzbanSecret = secret
			rep.note("old subscription links keep working when this server answers on the old subscription address (point the old domain here)")
		}
	}
	if rep.marzbanSecret == "" {
		for i := range clients {
			clients[i].legacy = ""
		}
	}
	// PasarGuard keeps its Xray config (with inbounds) in core_configs.
	var ins []foreignInbound
	if kind == "pasarguard" && tableExists(ctx, db, "core_configs") {
		var cfg string
		if db.QueryRowContext(ctx, `SELECT config FROM core_configs ORDER BY id LIMIT 1`).Scan(&cfg) == nil {
			ins = inboundsFromXrayConfig(cfg, rep)
			for i := range clients {
				for _, in := range ins {
					clients[i].inbounds = append(clients[i].inbounds, in.origin)
				}
			}
		}
	} else if kind == "marzban" {
		rep.note("Marzban keeps its inbounds in xray_config.json, not in the database: create matching inbounds here (same ports keep old configs working) and pick them below")
	}
	return ins, clients, admins, nil
}

func parseMarzbanTime(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999", "2006-01-02 15:04:05", "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// inboundsFromXrayConfig converts the managed inbounds of an Xray config.
func inboundsFromXrayConfig(cfg string, rep *ForeignReport) []foreignInbound {
	var c struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if json.Unmarshal([]byte(cfg), &c) != nil {
		rep.note("the Xray config could not be read; create the inbounds here by hand")
		return nil
	}
	var out []foreignInbound
	for _, in := range c.Inbounds {
		proto, _ := in["protocol"].(string)
		tag, _ := in["tag"].(string)
		port := intOf(in["port"])
		switch proto {
		case "vless", "vmess", "trojan":
		default:
			if tag != "api" && proto != "dokodemo-door" {
				rep.skip("inbound %s (%s): not moved", tag, proto)
			}
			continue
		}
		if port <= 0 {
			rep.skip("inbound %s: no single port", tag)
			continue
		}
		meta := map[string]any{"remark": or2(tag, "Dark VPN"), "fingerprint": "chrome"}
		tpl := map[string]any{"protocol": proto, "port": port, "listen": "0.0.0.0"}
		if l, _ := in["listen"].(string); l != "" && !strings.HasPrefix(l, "@") {
			tpl["listen"] = l
		}
		set, _ := in["settings"].(map[string]any)
		if set == nil {
			set = map[string]any{}
		}
		delete(set, "clients")
		tpl["settings"] = set
		if st, _ := in["streamSettings"].(map[string]any); st != nil {
			b, _ := json.Marshal(st)
			tpl["streamSettings"] = xuiStream(string(b), meta, rep, tag)
		}
		if sn, ok := in["sniffing"]; ok {
			tpl["sniffing"] = sn
		}
		origin := v2ID(or2(tag, strconv.Itoa(port)))
		out = append(out, foreignInbound{origin: origin, port: port, template: tpl, meta: meta})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].port < out[j].port })
	return out
}

// marzbanToken checks a Marzban/PasarGuard subscription token against the
// stored secret and returns the username it names.
func marzbanToken(tok, secret string) (string, bool) {
	if secret == "" || len(tok) < 15 || strings.HasPrefix(tok, "ey") {
		return "", false
	}
	body, sig := tok[:len(tok)-10], tok[len(tok)-10:]
	sum := sha256.Sum256([]byte(body + secret))
	want := base64.URLEncoding.EncodeToString(sum[:])[:10]
	if sig != want {
		return "", false
	}
	raw, err := base64.URLEncoding.DecodeString(body + strings.Repeat("=", (4-len(body)%4)%4))
	if err != nil {
		return "", false
	}
	user, _, ok := strings.Cut(string(raw), ",")
	return user, ok && user != ""
}

// foreignSubToken resolves an old Marzban/PasarGuard link to a v2 token.
func (s *Store) foreignSubToken(ctx context.Context, tok string) (string, error) {
	user, ok := marzbanToken(tok, s.Setting(ctx, "legacy_marzban_secret"))
	if !ok {
		return "", ErrNotFound
	}
	var v2 string
	err := s.ro.QueryRowContext(ctx, `SELECT c.sub_token FROM legacy_sub_tokens l JOIN clients c ON c.email=l.email WHERE l.token_sha=? AND l.kind='marzban-user'`,
		legacyTokenSHA("marzban-user", user)).Scan(&v2)
	if err != nil {
		return "", ErrNotFound
	}
	return v2, nil
}

var legacyPathRE = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,60}/$`)

func contains(list []string, x string) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}

// legacyPath maps an old 3x-ui subscription address (custom subPath, or
// subJsonPath for Xray JSON) to this panel's /sub/<token>.
func (s *Store) legacyPath(r *http.Request) (token, format string, ok bool) {
	for _, p := range strings.Split(s.Setting(r.Context(), "legacy_sub_paths"), ",") {
		f := ""
		if strings.HasPrefix(p, "json:") {
			p, f = strings.TrimPrefix(p, "json:"), "xray"
		}
		if p == "" || !strings.HasPrefix(r.URL.Path, p) {
			continue
		}
		tok := strings.TrimPrefix(r.URL.Path, p)
		if subIDRE.MatchString(tok) {
			return tok, f, true
		}
	}
	return "", "", false
}
