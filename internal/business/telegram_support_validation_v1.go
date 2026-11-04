package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// validateTelegramEngagementData is read-only and accepts an older native DB
// with no optional engagement family. A present family must be complete and
// its rows must remain bound to the same customer, bot, approved audience and
// shared financial ledger after backup/restore. Foreign keys alone do not
// express all of these relationships.
func validateTelegramEngagementData(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, family := range []struct {
		prefix   string
		ddl      []string
		validate func(context.Context, *sql.Tx) error
	}{
		{"telegram_support_", telegramSupportSchemaV1, validateTelegramSupportRowsV1},
		{"telegram_campaign_", telegramCampaignSchemaV1, validateTelegramCampaignRowsV1},
		{"telegram_rewards_", telegramRewardsSchemaV1, validateTelegramRewardsRowsV1},
	} {
		present, e := telegramExtensionState(ctx, tx, family.prefix, family.ddl)
		if e != nil {
			return e
		}
		if !present {
			continue
		}
		if e = telegramCommerceReady(ctx, tx); e != nil {
			return e
		}
		if e = family.validate(ctx, tx); e != nil {
			return fmt.Errorf("%sdata integrity: %w", family.prefix, ErrUnavailable)
		}
	}
	return nil
}

func telegramNoInvalidRows(ctx context.Context, tx *sql.Tx, queries ...string) error {
	for _, query := range queries {
		if err := telegramExtensionRowsValid(ctx, tx, query); err != nil {
			return err
		}
	}
	return nil
}

func validateTelegramSupportRowsV1(ctx context.Context, tx *sql.Tx) error {
	if err := telegramNoInvalidRows(ctx, tx,
		`SELECT COUNT(*) FROM telegram_support_tickets t LEFT JOIN telegram_commerce_customers c ON c.customer_id=t.customer_id AND c.store_id=t.store_id LEFT JOIN telegram_commerce_customer_clients b ON b.store_id=t.store_id AND b.customer_id=t.customer_id AND b.client_id=t.client_id WHERE c.customer_id IS NULL OR (t.client_id IS NOT NULL AND b.client_id IS NULL) OR t.created_unix<=0 OR t.updated_unix<t.created_unix`,
		`SELECT COUNT(*) FROM telegram_support_messages m LEFT JOIN telegram_support_tickets t ON t.id=m.ticket_id LEFT JOIN telegram_commerce_customers c ON c.store_id=t.store_id AND c.telegram_user_id=m.user_id WHERE t.id IS NULL OR c.customer_id IS NULL OR (m.from_support=0 AND c.customer_id!=t.customer_id) OR (m.from_support=1 AND c.customer_id=t.customer_id) OR m.created_unix<t.created_unix`,
		`SELECT COUNT(*) FROM telegram_support_events e LEFT JOIN telegram_support_tickets t ON t.id=e.ticket_id AND t.store_id=e.store_id LEFT JOIN telegram_commerce_customers c ON c.store_id=e.store_id AND c.telegram_user_id=e.actor_user_id WHERE t.id IS NULL OR c.customer_id IS NULL OR e.kind NOT IN('create','reply','state') OR e.created_unix<t.created_unix`,
	); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,subject FROM telegram_support_tickets`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, subject string
		if err = rows.Scan(&id, &subject); err != nil {
			rows.Close()
			return err
		}
		if !idRE.MatchString(id) || !validName(subject, 120) {
			rows.Close()
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,body,attachment_json FROM telegram_support_messages`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, body, raw string
		var attachment *TelegramAttachmentV1
		if err = rows.Scan(&id, &body, &raw); err != nil {
			return err
		}
		if !idRE.MatchString(id) || len(raw) > 2048 || json.Unmarshal([]byte(raw), &attachment) != nil || !telegramValidAttachment(attachment) || !telegramValidText(body, 4096, attachment != nil) {
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return validateTelegramEngagementAuditV1(ctx, tx, "telegram_support_")
}

func validateTelegramCampaignRowsV1(ctx context.Context, tx *sql.Tx) error {
	if err := telegramNoInvalidRows(ctx, tx,
		`SELECT COUNT(*) FROM telegram_campaign_consent m LEFT JOIN telegram_commerce_customers c ON c.customer_id=m.customer_id AND c.store_id=m.store_id WHERE c.customer_id IS NULL OR m.updated_unix<=0`,
		`SELECT COUNT(*) FROM telegram_campaign_drafts d LEFT JOIN telegram_commerce_customers creator ON creator.store_id=d.store_id AND creator.telegram_user_id=d.creator_user_id LEFT JOIN telegram_commerce_customers approver ON approver.store_id=d.store_id AND approver.telegram_user_id=d.approver_user_id WHERE creator.customer_id IS NULL OR (d.approver_user_id>0 AND approver.customer_id IS NULL) OR d.created_unix<=0 OR d.updated_unix<d.created_unix`,
		`SELECT COUNT(*) FROM telegram_campaign_deliveries d LEFT JOIN telegram_campaign_drafts c ON c.id=d.campaign_id LEFT JOIN telegram_commerce_customers u ON u.customer_id=d.customer_id AND u.store_id=c.store_id LEFT JOIN telegram_commerce_stores s ON s.store_id=c.store_id AND s.bot_id=d.bot_id LEFT JOIN telegram_campaign_consent m ON m.store_id=c.store_id AND m.customer_id=d.customer_id WHERE c.id IS NULL OR u.customer_id IS NULL OR s.store_id IS NULL OR u.telegram_user_id!=d.chat_id OR m.customer_id IS NULL OR d.consent_revision>m.revision OR d.attempt_count!=(SELECT COUNT(*) FROM telegram_campaign_attempts a WHERE a.delivery_id=d.id)`,
		`SELECT COUNT(*) FROM telegram_campaign_previews p LEFT JOIN telegram_campaign_drafts c ON c.id=p.campaign_id WHERE c.id IS NULL OR p.revision>c.revision OR p.selection_unix<c.created_unix OR p.expires_unix!=p.selection_unix+600`,
		`SELECT COUNT(*) FROM telegram_campaign_attempts a LEFT JOIN telegram_campaign_deliveries d ON d.id=a.delivery_id WHERE d.id IS NULL OR a.started_unix<=0 OR a.lease_until_unix!=a.started_unix+120`,
		`SELECT COUNT(*) FROM telegram_campaign_outcomes o LEFT JOIN telegram_campaign_attempts a ON a.attempt_id=o.attempt_id WHERE a.attempt_id IS NULL OR o.finished_unix<a.started_unix OR (o.outcome='sent' AND (o.message_id<=0 OR o.error_code!=0 OR o.retry_after_seconds!=0)) OR (o.outcome='rate_limited' AND (o.message_id!=0 OR o.error_code!=429 OR o.retry_after_seconds NOT BETWEEN 1 AND 3600)) OR (o.outcome='permanent_failure' AND (o.message_id!=0 OR o.error_code NOT BETWEEN 400 AND 499 OR o.error_code=429 OR o.retry_after_seconds!=0)) OR (o.outcome='unknown' AND (o.message_id!=0 OR o.retry_after_seconds!=0 OR o.error_code NOT BETWEEN 0 AND 599))`,
		`SELECT COUNT(*) FROM telegram_campaign_deliveries d LEFT JOIN telegram_campaign_attempts a ON a.attempt_id=d.attempt_id AND a.delivery_id=d.id LEFT JOIN telegram_campaign_outcomes o ON o.attempt_id=d.attempt_id WHERE (d.attempt_count=0 AND (d.attempt_id!='' OR d.state NOT IN('pending','skipped','cancelled'))) OR (d.attempt_count>0 AND a.attempt_id IS NULL) OR (d.attempt_count>0 AND d.attempt_id!=(SELECT newest.attempt_id FROM telegram_campaign_attempts newest WHERE newest.delivery_id=d.id ORDER BY newest.rowid DESC LIMIT 1)) OR (d.state='dispatching' AND (o.attempt_id IS NOT NULL OR d.lease_until_unix!=a.lease_until_unix)) OR (d.state='sent' AND (o.outcome!='sent' OR o.message_id!=d.message_id OR o.attempt_id IS NULL)) OR (d.state='unknown' AND (o.outcome!='unknown' OR o.attempt_id IS NULL)) OR (d.state='permanent_failure' AND (o.attempt_id IS NULL OR NOT(o.outcome='permanent_failure' OR (o.outcome='rate_limited' AND d.attempt_count=8)))) OR (d.state IN('pending','skipped','cancelled') AND d.attempt_count>0 AND (o.attempt_id IS NULL OR o.outcome!='rate_limited'))`,
		`SELECT COUNT(*) FROM telegram_campaign_pacing p LEFT JOIN telegram_commerce_stores s ON s.bot_id=p.bot_id WHERE s.store_id IS NULL OR p.chat_id<0 OR p.next_unix_nano<=0`,
		`SELECT COUNT(*) FROM telegram_campaign_events e LEFT JOIN telegram_commerce_customers u ON u.store_id=e.store_id AND u.telegram_user_id=e.actor_user_id LEFT JOIN telegram_campaign_drafts c ON c.id=e.object_id AND c.store_id=e.store_id WHERE u.customer_id IS NULL OR e.kind NOT IN('consent','save','approve','control') OR (e.kind='consent' AND e.object_id!=u.customer_id) OR (e.kind!='consent' AND c.id IS NULL) OR e.created_unix<=0`,
		`SELECT COUNT(*) FROM telegram_campaign_drafts c WHERE c.state='completed' AND EXISTS(SELECT 1 FROM telegram_campaign_deliveries d WHERE d.campaign_id=c.id AND d.state IN('pending','dispatching'))`,
	); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+telegramCampaignColumnsV1+` FROM telegram_campaign_drafts`)
	if err != nil {
		return err
	}
	campaigns := []TelegramCampaignV1{}
	for rows.Next() {
		c, e := scanTelegramCampaignV1(rows)
		if e != nil {
			rows.Close()
			return e
		}
		campaigns = append(campaigns, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range campaigns {
		if !idRE.MatchString(c.ID) || !validName(c.Title, 120) {
			return ErrUnavailable
		}
		if c.ApproverUserID == 0 {
			var count int
			if c.State != "draft" && c.State != "cancelled" || c.ApprovalDigest != "" || c.RecipientCount != 0 {
				return ErrUnavailable
			}
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_campaign_deliveries WHERE campaign_id=?`, c.ID).Scan(&count); err != nil || count != 0 {
				return ErrUnavailable
			}
			continue
		}
		if c.State == "draft" {
			return ErrUnavailable
		}
		if err = telegramCampaignApprovalValidTx(ctx, tx, c); err != nil {
			return err
		}
		var expected string
		if err = tx.QueryRowContext(ctx, `SELECT audience_digest FROM telegram_campaign_previews WHERE campaign_id=? AND digest=? LIMIT 1`, c.ID, c.ApprovalDigest).Scan(&expected); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT customer_id,chat_id,consent_revision FROM telegram_campaign_deliveries WHERE campaign_id=? ORDER BY customer_id`, c.ID)
		if err != nil {
			return err
		}
		h := sha256.New()
		var count int64
		for rows.Next() {
			var customer string
			var chat, revision int64
			if err = rows.Scan(&customer, &chat, &revision); err != nil {
				rows.Close()
				return err
			}
			fmt.Fprintf(h, "%s:%d:%d:%d\n", customer, chat, chat, revision)
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count != c.RecipientCount || hex.EncodeToString(h.Sum(nil)) != expected {
			return ErrUnavailable
		}
	}
	return validateTelegramEngagementAuditV1(ctx, tx, "telegram_campaign_")
}

func validateTelegramRewardsRowsV1(ctx context.Context, tx *sql.Tx) error {
	if err := telegramNoInvalidRows(ctx, tx,
		`SELECT COUNT(*) FROM telegram_rewards_codes r LEFT JOIN telegram_commerce_customers c ON c.store_id=r.store_id AND c.customer_id=r.customer_id WHERE c.customer_id IS NULL OR r.created_unix<=0`,
		`SELECT COUNT(*) FROM telegram_rewards_attributions a LEFT JOIN telegram_rewards_codes r ON r.code=a.code AND r.store_id=a.store_id AND r.customer_id=a.referrer_customer_id LEFT JOIN telegram_commerce_customers c ON c.store_id=a.store_id AND c.customer_id=a.referred_customer_id WHERE r.code IS NULL OR c.customer_id IS NULL OR a.created_unix<r.created_unix OR a.expires_unix<=a.created_unix`,
		`SELECT COUNT(*) FROM telegram_rewards_grants g LEFT JOIN telegram_rewards_attributions a ON a.store_id=g.store_id AND a.referred_customer_id=g.referred_customer_id AND a.referrer_customer_id=g.referrer_customer_id LEFT JOIN telegram_commerce_orders o ON o.order_id=g.order_id AND o.store_id=g.store_id AND o.customer_id=g.referred_customer_id LEFT JOIN telegram_commerce_payments p ON p.order_id=g.order_id AND p.bot_id=g.payment_bot_id AND p.charge_id=g.charge_id WHERE a.referred_customer_id IS NULL OR o.order_id IS NULL OR p.order_id IS NULL OR g.policy_json!=a.policy_json OR g.created_unix<p.created_unix`,
		`SELECT COUNT(*) FROM telegram_rewards_releases l LEFT JOIN telegram_rewards_grants g ON g.order_id=l.order_id LEFT JOIN telegram_commerce_wallet_ledger w ON w.event_id=l.wallet_event_id AND w.store_id=g.store_id AND w.customer_id=g.referrer_customer_id AND w.bucket='bonus' AND w.currency='XTR' AND w.delta=g.amount_stars WHERE g.order_id IS NULL OR w.event_id IS NULL OR g.amount_stars<=0 OR l.wallet_event_id!='tg-referral-grant:'||g.order_id OR w.reason!='verified referral purchase' OR l.created_unix<g.release_after_unix OR l.created_unix<g.created_unix`,
		`SELECT COUNT(*) FROM telegram_rewards_reversals v LEFT JOIN telegram_rewards_grants g ON g.order_id=v.order_id LEFT JOIN telegram_commerce_payment_refunds f ON f.order_id=v.refund_order_id AND f.order_id=g.order_id AND f.bot_id=g.payment_bot_id AND f.charge_id=g.charge_id LEFT JOIN telegram_rewards_releases l ON l.order_id=g.order_id LEFT JOIN telegram_commerce_wallet_ledger w ON w.event_id=v.wallet_event_id AND w.store_id=g.store_id AND w.customer_id=g.referrer_customer_id AND w.bucket='bonus' AND w.currency='XTR' AND w.delta=-g.amount_stars WHERE g.order_id IS NULL OR f.order_id IS NULL OR v.amount_stars!=g.amount_stars OR v.created_unix<f.created_unix OR (v.released=1 AND (l.order_id IS NULL OR w.event_id IS NULL OR v.wallet_event_id!='tg-referral-reverse:'||g.order_id OR w.reason!='verified referral purchase refunded')) OR (v.released=0 AND (l.order_id IS NOT NULL OR v.wallet_event_id IS NOT NULL))`,
		`SELECT COUNT(*) FROM telegram_rewards_grants g JOIN telegram_commerce_payment_refunds f ON f.order_id=g.order_id LEFT JOIN telegram_rewards_reversals v ON v.order_id=g.order_id WHERE v.order_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger w LEFT JOIN telegram_rewards_releases l ON l.wallet_event_id=w.event_id LEFT JOIN telegram_rewards_reversals v ON v.wallet_event_id=w.event_id WHERE (w.event_id GLOB 'tg-referral-grant:*' AND l.order_id IS NULL) OR (w.event_id GLOB 'tg-referral-reverse:*' AND v.order_id IS NULL)`,
		`SELECT COUNT(*) FROM telegram_rewards_events e LEFT JOIN telegram_commerce_customers c ON c.store_id=e.store_id AND c.telegram_user_id=e.actor_user_id WHERE c.customer_id IS NULL OR e.kind!='policy' OR e.created_unix<=0`,
		`SELECT COUNT(*) FROM telegram_rewards_policies p WHERE NOT EXISTS(SELECT 1 FROM telegram_rewards_events e WHERE e.kind='policy' AND e.store_id=p.store_id AND e.receipt_json=p.policy_json)`,
		`SELECT COUNT(*) FROM telegram_rewards_attributions a WHERE NOT EXISTS(SELECT 1 FROM telegram_rewards_events e WHERE e.kind='policy' AND e.store_id=a.store_id AND e.receipt_json=a.policy_json)`,
	); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT policy_json,revision FROM telegram_rewards_policies`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var raw []byte
		var revision int64
		var p TelegramReferralPolicyV1
		if err = rows.Scan(&raw, &revision); err != nil {
			rows.Close()
			return err
		}
		if len(raw) > 8192 || json.Unmarshal(raw, &p) != nil || !telegramValidReferralPolicy(p) || p.Revision != revision {
			rows.Close()
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT code FROM telegram_rewards_codes`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			rows.Close()
			return err
		}
		raw, e := hex.DecodeString(code)
		if e != nil || len(raw) != 16 || code != strings.ToLower(code) {
			rows.Close()
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	type edge struct{ store, customer, parent string }
	edges := []edge{}
	parents := map[string]string{}
	rows, err = tx.QueryContext(ctx, `SELECT store_id,referred_customer_id,referrer_customer_id FROM telegram_rewards_attributions`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var e edge
		if err = rows.Scan(&e.store, &e.customer, &e.parent); err != nil {
			rows.Close()
			return err
		}
		edges = append(edges, e)
		parents[e.customer] = e.parent
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, edge := range edges {
		a, e := telegramReadAttributionTx(ctx, tx, edge.store, edge.customer)
		if e != nil {
			return e
		}
		expires := a.CreatedUnix + a.Policy.AttributionDays*86400
		if a.Policy.ExpiresUnix != 0 && a.Policy.ExpiresUnix < expires {
			expires = a.Policy.ExpiresUnix
		}
		if a.ExpiresUnix != expires {
			return ErrUnavailable
		}
		visited := map[string]bool{}
		current := edge.customer
		for depth := 0; current != ""; depth++ {
			if depth > 64 || visited[current] {
				return ErrUnavailable
			}
			visited[current] = true
			current = parents[current]
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT order_id FROM telegram_rewards_grants`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	type balance struct{ net, cap int64 }
	balances := map[string]balance{}
	for _, id := range ids {
		o, e := telegramRewardOrderTx(ctx, tx, id)
		if e != nil {
			return e
		}
		a, e := telegramReadAttributionTx(ctx, tx, o.StoreID, o.CustomerID)
		if e != nil {
			return e
		}
		g, e := telegramReadRewardTx(ctx, tx, id)
		if e != nil {
			return e
		}
		var receiptSHA string
		if e = tx.QueryRowContext(ctx, `SELECT receipt_sha256 FROM telegram_rewards_grants WHERE order_id=?`, id).Scan(&receiptSHA); e != nil {
			return e
		}
		if receiptSHA != o.ReceiptSHA || g.ReleaseAfterUnix != o.PaidUnix+a.Policy.HoldHours*3600 {
			return ErrUnavailable
		}
		ceiling := int64(0)
		if o.PaidUnix >= a.CreatedUnix && o.PaidUnix < a.ExpiresUnix {
			var earlier int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments p JOIN telegram_commerce_orders o ON o.order_id=p.order_id WHERE o.store_id=? AND o.customer_id=? AND p.rowid<?`, o.StoreID, o.CustomerID, o.PaymentSequence).Scan(&earlier); e != nil {
				return e
			}
			rate := a.Policy.FirstPurchase
			if earlier > 0 {
				rate = a.Policy.RepeatPurchase
			}
			ceiling = rate
			if a.Policy.Mode == "percent_bps" {
				ceiling = o.AmountStars * rate / 10000
			}
			if ceiling > a.Policy.MaxPerOrderStars {
				ceiling = a.Policy.MaxPerOrderStars
			}
		}
		if g.AmountStars > ceiling {
			return ErrUnavailable
		}
		key := g.StoreID + ":" + g.ReferrerCustomerID
		b := balances[key]
		if a.Policy.PerReferrerCapStars > b.cap {
			b.cap = a.Policy.PerReferrerCapStars
		}
		if g.ReversedUnix == 0 {
			b.net += g.AmountStars
		}
		balances[key] = b
		if g.ReleasedUnix > 0 {
			// A later verified refund changes order status but cannot invalidate
			// the historical native delivery that preceded the original grant.
			var terms, store, customer, client string
			var generation, validUntil, delivered, message int64
			e = tx.QueryRowContext(ctx, `SELECT store_id,customer_id,client_id,client_terms_sha256,generation,valid_until_unix_nano,delivered_unix,telegram_message_id FROM telegram_commerce_order_deliveries WHERE order_id=?`, id).Scan(&store, &customer, &client, &terms, &generation, &validUntil, &delivered, &message)
			if e != nil || store != o.StoreID || customer != o.CustomerID || client != o.ClientID || terms != telegramClientTerms(o.Receipt.Client) || generation <= 0 || message <= 0 || delivered < o.Receipt.CreatedUnix || validUntil <= delivered*int64(time.Second) || delivered > g.ReleasedUnix {
				return ErrUnavailable
			}
		}
	}
	for _, b := range balances {
		if b.net > b.cap {
			return ErrUnavailable
		}
	}
	return validateTelegramEngagementAuditV1(ctx, tx, "telegram_rewards_")
}

func validateTelegramEngagementAuditV1(ctx context.Context, tx *sql.Tx, prefix string) error {
	if prefix != "telegram_support_" && prefix != "telegram_campaign_" && prefix != "telegram_rewards_" {
		return ErrUnavailable
	}
	type event struct{ id, hash, kind string }
	cursor := ""
	for {
		rows, err := tx.QueryContext(ctx, `SELECT event_id,fingerprint,kind FROM `+prefix+`events WHERE event_id>? ORDER BY event_id LIMIT 200`, cursor)
		if err != nil {
			return err
		}
		list := []event{}
		for rows.Next() {
			var e event
			if err = rows.Scan(&e.id, &e.hash, &e.kind); err != nil {
				rows.Close()
				return err
			}
			if !eventRE.MatchString(e.id) {
				rows.Close()
				return ErrUnavailable
			}
			list = append(list, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}
		for _, e := range list {
			switch prefix {
			case "telegram_support_":
				if _, found, err := telegramSupportReplay(ctx, tx, e.id, e.hash, e.kind); err != nil || !found {
					return ErrUnavailable
				}
			case "telegram_campaign_":
				var out any = &TelegramCampaignReceiptV1{}
				if e.kind == "consent" {
					out = &TelegramMarketingConsentV1{}
				}
				if found, err := telegramCampaignEventReplay(ctx, tx, e.id, e.hash, e.kind, out); err != nil || !found {
					return ErrUnavailable
				}
			case "telegram_rewards_":
				var store string
				var user, bot int64
				var raw []byte
				var p TelegramReferralPolicyV1
				if e.kind != "policy" {
					return ErrUnavailable
				}
				if err = tx.QueryRowContext(ctx, `SELECT e.store_id,e.actor_user_id,s.bot_id,e.receipt_json FROM telegram_rewards_events e JOIN telegram_commerce_stores s ON s.store_id=e.store_id WHERE event_id=?`, e.id).Scan(&store, &user, &bot, &raw); err != nil {
					return err
				}
				if len(raw) > 8192 || json.Unmarshal(raw, &p) != nil || !telegramValidReferralPolicy(p) || p.Revision < 1 {
					return ErrUnavailable
				}
				current, err := telegramReferralPolicyTx(ctx, tx, store)
				if err != nil || current.Revision < p.Revision {
					return ErrUnavailable
				}
				req := TelegramReferralPolicyChangeV1{EventID: e.id, ExpectedRevision: p.Revision - 1, Policy: p}
				req.Policy.Revision = 0
				want := fingerprint("telegram-referral-policy", struct {
					Actor   TelegramActor
					Request TelegramReferralPolicyChangeV1
				}{TelegramActor{BotID: bot, UserID: user, ChatID: user}, req})
				if want != e.hash {
					return ErrUnavailable
				}
			}
		}
		cursor = list[len(list)-1].id
	}
}
