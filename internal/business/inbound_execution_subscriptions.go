package business

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

type executionSubscriptionHead struct {
	client                   string
	generation               uint64
	state, credential, event string
}

func executionTokenHash(token string) string {
	return ix.BytesSHA([]byte("DARK-PRO-MANAGED-INBOUND-SUBSCRIPTION-v1\x00" + token))
}
func readExecutionSubscriptionHead(ctx context.Context, tx *sql.Tx, client string) (executionSubscriptionHead, error) {
	h := executionSubscriptionHead{client: client, state: "absent"}
	err := tx.QueryRowContext(ctx, `SELECT generation,state,credential_id,event_id FROM inbound_execution_subscription_heads WHERE client_id=?`, client).Scan(&h.generation, &h.state, &h.credential, &h.event)
	if errors.Is(err, sql.ErrNoRows) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	var latest, count uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0),COUNT(*) FROM inbound_execution_subscription_events WHERE client_id=?`, client).Scan(&latest, &count); err != nil {
		return h, err
	}
	if h.generation != latest || count != latest || h.generation == 0 {
		return h, ErrUnavailable
	}
	var state, credential, event string
	if err = tx.QueryRowContext(ctx, `SELECT state,credential_id,event_id FROM inbound_execution_subscription_events WHERE client_id=? AND generation=?`, client, latest).Scan(&state, &credential, &event); err != nil || state != h.state || credential != h.credential || event != h.event {
		return h, ErrUnavailable
	}
	return h, nil
}
func revealExecutionCredential(ctx context.Context, tx *sql.Tx, key []byte, h executionSubscriptionHead) (string, error) {
	var b subscriptionCredentialBinding
	var enc []byte
	if err := tx.QueryRowContext(ctx, `SELECT id,client_id,generation,token_sha,token_enc,created FROM inbound_execution_subscription_credentials WHERE id=? AND client_id=?`, h.credential, h.client).Scan(&b.ID, &b.ClientID, &b.Generation, &b.TokenSHA, &enc, &b.Created); err != nil {
		return "", err
	}
	raw, err := openSubscription(key, "managed-credential", b, enc)
	if err != nil {
		return "", err
	}
	token := string(raw)
	if !validSubscriptionToken(token) || subtle.ConstantTimeCompare([]byte(executionTokenHash(token)), []byte(b.TokenSHA)) != 1 {
		return "", ErrUnavailable
	}
	return token, nil
}

func (s *Store) MutateInboundExecutionSubscription(ctx context.Context, key []byte, actor, client string, in SubscriptionTokenMutation) (SubscriptionTokenReceipt, error) {
	var out SubscriptionTokenReceipt
	if !eventRE.MatchString(in.EventID) || in.ExpectedGeneration >= routeMax-1 || (in.Action != "issue" && in.Action != "rotate" && in.Action != "revoke") {
		return out, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := subscriptionActor(ctx, tx, actor, client, true, false)
	if err != nil {
		return out, err
	}
	h, err := readExecutionSubscriptionHead(ctx, tx, client)
	if err != nil {
		return out, err
	}
	fp := fingerprint("managed-inbound-subscription-token-v1", struct {
		Actor, Client string
		Input         SubscriptionTokenMutation
	}{actor, client, in})
	var old string
	err = tx.QueryRowContext(ctx, `SELECT event_id,client_id,generation,state,fingerprint FROM inbound_execution_subscription_events WHERE event_id=?`, in.EventID).Scan(&out.EventID, &out.ClientID, &out.Generation, &out.State, &old)
	if err == nil {
		if old != fp || out.ClientID != client {
			return out, ErrConflict
		}
		out.Replayed = true
		out.Superseded = out.Generation != h.generation
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err = executionEventAvailable(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	if h.generation != in.ExpectedGeneration || in.Action == "issue" && h.state != "absent" || in.Action == "rotate" && h.state == "absent" || in.Action == "revoke" && h.state != "active" || c.Archived && in.Action != "revoke" {
		return out, ErrConflict
	}
	if h.generation > 0 {
		if _, err = revealExecutionCredential(ctx, tx, key, h); err != nil {
			return out, err
		}
	}
	gen := h.generation + 1
	state := "revoked"
	credential := h.credential
	now := time.Now().UnixNano()
	if in.Action != "revoke" {
		state = "active"
		credential, err = subscriptionID()
		if err != nil {
			return out, err
		}
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return out, err
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		b := subscriptionCredentialBinding{ID: credential, ClientID: client, Generation: gen, TokenSHA: executionTokenHash(token), Created: now}
		enc, e := sealSubscription(key, "managed-credential", b, []byte(token))
		if e != nil {
			return out, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_subscription_credentials(id,client_id,generation,token_sha,token_enc,created) VALUES(?,?,?,?,?,?)`, credential, client, gen, b.TokenSHA, enc, now); err != nil {
			return out, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_subscription_events(event_id,actor_id,client_id,generation,action,state,credential_id,fingerprint,created) VALUES(?,?,?,?,?,?,?,?,?)`, in.EventID, actor, client, gen, in.Action, state, credential, fp, now); err != nil {
		return out, err
	}
	if h.generation == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_subscription_heads(client_id,generation,state,credential_id,event_id) VALUES(?,?,?,?,?)`, client, gen, state, credential, in.EventID)
	} else {
		var res sql.Result
		res, err = tx.ExecContext(ctx, `UPDATE inbound_execution_subscription_heads SET generation=?,state=?,credential_id=?,event_id=? WHERE client_id=? AND generation=?`, gen, state, credential, in.EventID, client, h.generation)
		if err == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				return out, ErrConflict
			}
		}
	}
	if err != nil {
		return out, err
	}
	return SubscriptionTokenReceipt{EventID: in.EventID, ClientID: client, Generation: gen, State: state}, tx.Commit()
}

func (s *Store) RevealInboundExecutionSubscription(ctx context.Context, key []byte, actor, client string, generation uint64) (SubscriptionSecret, error) {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return SubscriptionSecret{}, err
	}
	defer tx.Rollback()
	c, err := subscriptionActor(ctx, tx, actor, client, true, false)
	if err != nil {
		return SubscriptionSecret{}, err
	}
	if c.Archived {
		return SubscriptionSecret{}, ErrNotFound
	}
	h, err := readExecutionSubscriptionHead(ctx, tx, client)
	if err != nil {
		return SubscriptionSecret{}, err
	}
	if h.generation != generation || h.state != "active" {
		return SubscriptionSecret{}, ErrConflict
	}
	token, err := revealExecutionCredential(ctx, tx, key, h)
	if err != nil {
		return SubscriptionSecret{}, err
	}
	return SubscriptionSecret{ClientID: client, Generation: generation, Token: token}, tx.Commit()
}

type executionSubscriptionRoute struct {
	view     ix.SubscriptionRoute
	spec     inboundspec.Spec
	user     inboundspec.User
	endpoint inboundspec.Endpoint
	expires  int64
	verified bool
	formats  []string
}

func executionSubscriptionRoutes(ctx context.Context, tx *sql.Tx, key []byte, client string) ([]executionSubscriptionRoute, error) {
	out := []executionSubscriptionRoute{}
	var eligible int
	if err := tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, client).Scan(&eligible); err != nil || eligible != 1 {
		return out, ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT inbound_id,active_revision,desired_revision FROM inbound_execution_heads WHERE desired_state='active' AND active_revision>0 ORDER BY inbound_id`)
	if err != nil {
		return out, err
	}
	type ref struct{ id, active, desired uint64 }
	refs := []ref{}
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.id, &r.active, &r.desired); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixNano()
	for _, r := range refs {
		snapshot, e := readExecutionSnapshot(ctx, tx, key, r.id, r.active)
		if e != nil {
			return nil, e
		}
		i := sort.SearchStrings(snapshot.EffectiveClientIDs, client)
		if i >= len(snapshot.EffectiveClientIDs) || snapshot.EffectiveClientIDs[i] != client {
			continue
		}
		if r.active != r.desired || snapshot.Dormant || snapshot.Profile.Spec == nil {
			return nil, ErrConflict
		}
		var user inboundspec.User
		found := false
		for j, id := range snapshot.EffectiveClientIDs {
			if id == client && j < len(snapshot.Profile.Users) {
				u := snapshot.Profile.Users[j]
				user = inboundspec.User{ID: u.ID, UUID: u.UUID, Email: u.Email, Password: u.Password, Addresses: u.Addresses}
				found = true
				break
			}
		}
		if !found {
			return nil, ErrUnavailable
		}
		expires := snapshot.ExpiresUnix * int64(time.Second)
		verified := true
		for node, install := range executionSnapshotNodes(snapshot) {
			var desired, observed uint64
			var enc []byte
			if e = tx.QueryRowContext(ctx, `SELECT desired_generation,observed_generation,observation_enc FROM inbound_execution_nodes WHERE node_id=? AND installation_id=?`, node, install).Scan(&desired, &observed, &enc); e != nil {
				return nil, e
			}
			if desired != observed || len(enc) == 0 {
				return nil, ErrConflict
			}
			b, e := readExecutionBundle(ctx, tx, key, node, install, desired)
			if e != nil {
				return nil, e
			}
			if e = executionBundleAuthority(ctx, tx, key, b); e != nil {
				return nil, e
			}
			present := false
			for _, revision := range b.Revisions {
				if revision.InboundID == r.id && revision.Revision == r.active {
					present = true
				}
			}
			if !present {
				return nil, ErrConflict
			}
			var observation ix.Observation
			if openPlanValue(key, executionAAD("observation", node+"/"+install, observed), enc, &observation) != nil {
				return nil, ErrUnavailable
			}
			if observation.Generation != desired || observation.ConfigSHA != b.ConfigSHA || observation.ManifestSHA != b.ManifestSHA || !observation.ProcessReady || (observation.State != "active" && observation.State != "ready") || observation.LeaseExpires <= now || observation.ObservedUnixNano > now || now-observation.ObservedUnixNano > int64(15*time.Second) || observation.AccountingState != "accepted" || (observation.HealthState != "healthy" && observation.HealthState != "engine_ready") {
				return nil, ErrConflict
			}
			if observation.HealthState != "healthy" {
				verified = false
			}
			if observation.LeaseExpires < expires {
				expires = observation.LeaseExpires
			}
			if freshUntil := observation.ObservedUnixNano + int64(15*time.Second); freshUntil < expires {
				expires = freshUntil
			}
			accountingUntil, e := executionAccountingFreshUntil(ctx, tx, node, install, desired, observation.EpochID, now)
			if e != nil {
				return nil, e
			}
			if accountingUntil < expires {
				expires = accountingUntil
			}
		}
		profile := InboundInput{ID: r.id, Name: snapshot.Name, Spec: snapshot.Profile.Spec, Transport: snapshot.Profile.Spec.Network, Security: snapshot.Profile.Spec.Security}
		plan := NativeInboundPlan{Profile: NativeInbound{InboundInput: profile}, Nodes: snapshot.Nodes}
		formats := []string{}
		for _, info := range inboundspec.Catalog() {
			if info.Protocol == profile.Spec.Protocol {
				formats = append(formats, info.ExportFormats...)
			}
		}
		for _, endpoint := range previewPlanEndpoints(plan) {
			if !endpoint.Enabled {
				continue
			}
			raw, _ := json.Marshal(*snapshot.Profile.Spec)
			var spec inboundspec.Spec
			if json.Unmarshal(raw, &spec) != nil {
				return nil, ErrUnavailable
			}
			if spec.Client == nil {
				spec.Client = map[string]any{}
			}
			if endpoint.ServerName != "" {
				spec.Client["serverName"] = endpoint.ServerName
			}
			if endpoint.Fingerprint != "" {
				spec.Client["fingerprint"] = endpoint.Fingerprint
			}
			if endpoint.SpiderX != "" {
				spec.Client["spiderX"] = endpoint.SpiderX
			}
			out = append(out, executionSubscriptionRoute{view: ix.SubscriptionRoute{InboundID: r.id, Revision: r.active, NodeID: endpoint.NodeID, Path: endpoint.Path, Protocol: spec.Protocol, Name: endpoint.Label}, spec: spec, user: user, endpoint: inboundspec.Endpoint{Address: endpoint.Address, Port: endpoint.Port, Name: endpoint.Label, SNI: endpoint.ServerName}, expires: expires, verified: verified, formats: formats})
		}
	}
	if len(out) == 0 {
		return out, ErrNotFound
	}
	if len(out) > 4096 {
		return nil, ErrInvalid
	}
	return out, nil
}

func executionSubscriptionFormats(routes []executionSubscriptionRoute) []string {
	allowed := map[string]bool{"json": true, "xray": true, "uri": true, "wireguard": true}
	for _, r := range routes {
		supported := map[string]bool{}
		for _, f := range r.formats {
			supported[f] = true
		}
		for f := range allowed {
			if !supported[f] {
				delete(allowed, f)
			}
		}
	}
	if len(routes) != 1 {
		delete(allowed, "wireguard")
	}
	out := []string{}
	for _, f := range []string{"uri", "json", "xray", "wireguard"} {
		if allowed[f] {
			out = append(out, f)
		}
	}
	return out
}

func projectExecutionSubscription(ctx context.Context, tx *sql.Tx, key []byte, client, format string) (ix.Subscription, error) {
	out := ix.Subscription{Version: ix.Version, ClientID: client, Format: format, Routes: []ix.SubscriptionRoute{}, PathVerified: true, ContentType: "application/json"}
	routes, err := executionSubscriptionRoutes(ctx, tx, key, client)
	if err != nil {
		return out, err
	}
	if format == "" {
		format = "uri"
		out.Format = format
	}
	allowed := false
	for _, f := range executionSubscriptionFormats(routes) {
		allowed = allowed || f == format
	}
	if !allowed {
		return out, inboundspec.ErrUnsupportedFormat
	}
	items := []json.RawMessage{}
	outbounds := []map[string]any{}
	lines := []string{}
	for i, r := range routes {
		raw, e := inboundspec.ExportClient(r.spec, format, r.user, r.endpoint)
		if e != nil {
			return out, e
		}
		out.Routes = append(out.Routes, r.view)
		if out.ExpiresUnixNano == 0 || r.expires < out.ExpiresUnixNano {
			out.ExpiresUnixNano = r.expires
		}
		out.PathVerified = out.PathVerified && r.verified
		switch format {
		case "uri", "wireguard":
			lines = append(lines, string(raw))
		case "xray":
			var doc struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if json.Unmarshal(raw, &doc) != nil || len(doc.Outbounds) != 1 {
				return out, ErrUnavailable
			}
			doc.Outbounds[0]["tag"] = fmt.Sprintf("managed-%d-%s-%d", r.view.InboundID, r.view.Path, i+1)
			outbounds = append(outbounds, doc.Outbounds[0])
		default:
			items = append(items, raw)
		}
	}
	switch format {
	case "uri", "wireguard":
		out.Content = []byte(strings.Join(lines, "\n") + "\n")
		out.ContentType = "text/plain; charset=utf-8"
	case "xray":
		out.Content, err = json.Marshal(map[string]any{"outbounds": outbounds})
	default:
		out.Content, err = json.Marshal(items)
	}
	if err != nil || len(out.Content) > 16<<20 {
		return out, ErrInvalid
	}
	if time.Now().UnixNano() >= out.ExpiresUnixNano {
		return out, ErrConflict
	}
	out.SHA256 = ix.BytesSHA(out.Content)
	return out, nil
}

func (s *Store) InboundExecutionSubscription(ctx context.Context, key []byte, actor, client string) (ix.SubscriptionView, error) {
	out := ix.SubscriptionView{ClientID: client, State: "absent", Formats: []string{}, Blockers: []ix.Blocker{}}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = subscriptionActor(ctx, tx, actor, client, false, false); err != nil {
		return out, err
	}
	h, err := readExecutionSubscriptionHead(ctx, tx, client)
	if err != nil {
		return out, err
	}
	out.Generation, out.State = h.generation, h.state
	routes, err := executionSubscriptionRoutes(ctx, tx, key, client)
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
			out.Blockers = append(out.Blockers, ix.Blocker{Code: "current_applied_routes_unavailable", ClientID: client})
			return out, tx.Commit()
		}
		return out, err
	}
	out.Available = true
	out.PathVerified = true
	out.Formats = executionSubscriptionFormats(routes)
	out.RouteCount = len(routes)
	for _, r := range routes {
		if out.ExpiresUnixNano == 0 || r.expires < out.ExpiresUnixNano {
			out.ExpiresUnixNano = r.expires
		}
		out.PathVerified = out.PathVerified && r.verified
	}
	return out, tx.Commit()
}

func (s *Store) ProjectInboundExecutionSubscription(ctx context.Context, key []byte, actor, client, format string) (ix.Subscription, error) {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.Subscription{}, err
	}
	defer tx.Rollback()
	// A private export discloses the same credentials as token reveal.
	if _, err = subscriptionActor(ctx, tx, actor, client, true, false); err != nil {
		return ix.Subscription{}, err
	}
	v, err := projectExecutionSubscription(ctx, tx, key, client, format)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) ReadInboundExecutionSubscription(ctx context.Context, key []byte, token, format string) (ix.Subscription, error) {
	if !validSubscriptionToken(token) {
		return ix.Subscription{}, ErrNotFound
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.Subscription{}, err
	}
	defer tx.Rollback()
	var client string
	err = tx.QueryRowContext(ctx, `SELECT h.client_id FROM inbound_execution_subscription_heads h JOIN inbound_execution_subscription_credentials c ON c.id=h.credential_id WHERE c.token_sha=? AND h.state='active'`, executionTokenHash(token)).Scan(&client)
	if err != nil {
		return ix.Subscription{}, ErrNotFound
	}
	h, err := readExecutionSubscriptionHead(ctx, tx, client)
	if err != nil {
		return ix.Subscription{}, err
	}
	stored, err := revealExecutionCredential(ctx, tx, key, h)
	if err != nil {
		return ix.Subscription{}, err
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(token)) != 1 {
		return ix.Subscription{}, ErrNotFound
	}
	v, err := projectExecutionSubscription(ctx, tx, key, client, format)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}
