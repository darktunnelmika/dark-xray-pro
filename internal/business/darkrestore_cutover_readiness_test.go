package business

import "testing"

func TestDarkRestoreReadinessRequiresSubscriptionDeliveryAfterSettlement(t *testing.T) {
	attachment := &DarkRestoreLocalAttachmentReceiptV1{CreatedUnix: 1, CreatedUnixNano: 1_000_000_100}
	cases := []struct {
		name       string
		attachment *DarkRestoreLocalAttachmentReceiptV1
		delivery   subscriptionDeliveryHead
		want       bool
	}{
		{name: "no settlement", delivery: subscriptionDeliveryHead{Enabled: true, Revision: 1, Created: 1_000_000_200}},
		{name: "delivery schema disabled", attachment: attachment, delivery: subscriptionDeliveryHead{Revision: 1, Created: 1_000_000_200}},
		{name: "no authoritative revision", attachment: attachment, delivery: subscriptionDeliveryHead{Enabled: true, Created: 1_000_000_200}},
		{name: "delivery before settlement", attachment: attachment, delivery: subscriptionDeliveryHead{Enabled: true, Revision: 1, Created: 1_000_000_099}},
		{name: "delivery simultaneous with settlement", attachment: attachment, delivery: subscriptionDeliveryHead{Enabled: true, Revision: 1, Created: 1_000_000_100}},
		{name: "delivery after settlement", attachment: attachment, delivery: subscriptionDeliveryHead{Enabled: true, Revision: 1, Created: 1_000_000_101}, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := darkRestoreSubscriptionDeliveredAfterSettlement(test.attachment, test.delivery); got != test.want {
				t.Fatalf("delivered=%v want=%v", got, test.want)
			}
		})
	}
}
