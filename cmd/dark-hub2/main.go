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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func main() {
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
	adminTokenFile := fs.String("admin-token-file", "/etc/dark-hub2/admin-token", "owner API bearer token file")
	hosts := fs.String("hosts", "", "comma-separated IPs/names for the server certificate")
	id := fs.String("id", "", "node id")
	name := fs.String("name", "", "node display name")
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
	_ = fs.Parse(args)

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o700); err != nil {
		log.Fatal(err)
	}
	s, err := hubcore.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
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
		if tok, err := os.ReadFile(*adminTokenFile); err == nil && *adminListen != "" {
			admin := &http.Server{Addr: *adminListen, Handler: hubcore.AdminHandler(s, strings.TrimSpace(string(tok))), ReadHeaderTimeout: 10 * time.Second}
			go func() {
				log.Printf("owner API and subscriptions on %s", *adminListen)
				log.Fatal(admin.ListenAndServe())
			}()
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
		fmt.Println("owner login saved:", *name)
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
