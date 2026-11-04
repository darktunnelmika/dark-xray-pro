package business

import (
	"context"
	"errors"
	"testing"
)

func TestDarkRestoreIssuedCredentialRequiresCurrentActivation(t *testing.T) {
	for _, kind := range []string{"managed", "lab"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newPreparedNativeActivationFixture(t)
			defer f.s.Close()
			id := f.evidence.ClientID
			in := DarkRestoreNativeActivationInputV1{EventID: "revalidation-activation-0001", ClientID: id, Pins: f.pins}
			if _, err := f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, in, f.approval); err != nil {
				t.Fatal(err)
			}
			mutate := func(m SubscriptionTokenMutation) (SubscriptionTokenReceipt, error) {
				if kind == "managed" {
					return f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", id, m)
				}
				return f.s.MutateLabSubscription(ctx, f.key, "owner", id, m)
			}
			reveal := func() (SubscriptionSecret, error) {
				if kind == "managed" {
					return f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", id, 1)
				}
				return f.s.RevealLabSubscription(ctx, f.key, "owner", id, 1)
			}
			if _, err := mutate(SubscriptionTokenMutation{EventID: "revalidation-issue-0001", Action: "issue"}); err != nil {
				t.Fatal(err)
			}
			if secret, err := reveal(); err != nil || secret.Token == "" {
				t.Fatal("valid activation did not reveal credential", err)
			}
			c, err := f.s.Client(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: "revalidation-renewal-0001", ClientID: id, AdditionalVolumeBytes: 17, Reason: "Synthetic renewal invalidates pinned entitlement"}, c.RepresentativeID); err != nil {
				t.Fatal(err)
			}
			if err = requireDarkRestoreFinalCutoverTx(ctx, f.s.db, id); !errors.Is(err, ErrDarkRestoreCutover) {
				t.Fatal("renewal failed to invalidate approval", err)
			}
			if secret, err := reveal(); !errors.Is(err, ErrDarkRestoreCutover) || secret.Token != "" {
				t.Fatal("stale activation disclosed issued credential")
			}
			if _, err := mutate(SubscriptionTokenMutation{EventID: "revalidation-rotate-0001", Action: "rotate", ExpectedGeneration: 1}); !errors.Is(err, ErrDarkRestoreCutover) {
				t.Fatal("stale activation rotated credential", err)
			}
			revoked, err := mutate(SubscriptionTokenMutation{EventID: "revalidation-revoke-0001", Action: "revoke", ExpectedGeneration: 1})
			if err != nil || revoked.State != "revoked" || revoked.Generation != 2 {
				t.Fatal("stale approval prevented revocation", err)
			}
		})
	}
}
