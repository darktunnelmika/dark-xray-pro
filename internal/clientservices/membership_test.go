package clientservices

import (
	"errors"
	"reflect"
	"testing"
)

func fixture() (Snapshot, Change) {
	return Snapshot{Grant: Grant{RepresentativeID: "rep-a", InboundID: 7, Revision: 3, Enabled: true}, RepresentativeEnabled: true, CanManageClients: true, ClientID: "mine", ClientRepresentativeID: "rep-a", InboundID: 7, ExecutionRevision: 9, Ready: true, Members: []string{"other-b", "other-a"}}, Change{ClientID: "mine", InboundID: 7, ExpectedGrantRevision: 3, ExpectedExecutionRevision: 9, Add: true}
}
func TestMembershipPreservesOtherCustomersAndInput(t *testing.T) {
	s, c := fixture()
	original := append([]string(nil), s.Members...)
	got, e := Membership("rep-a", s, c)
	if e != nil || !reflect.DeepEqual(got, []string{"mine", "other-a", "other-b"}) || !reflect.DeepEqual(s.Members, original) {
		t.Fatalf("addition or input preservation: %v %v", got, e)
	}
	s.Members = got
	c.Add = false
	got, e = Membership("rep-a", s, c)
	if e != nil || !reflect.DeepEqual(got, []string{"other-a", "other-b"}) {
		t.Fatalf("removal: %v %v", got, e)
	}
}
func TestMembershipRejectsAuthorityAndConcurrentChanges(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Snapshot, *Change)
		want error
	}{
		{"foreign client", func(s *Snapshot, c *Change) { s.ClientRepresentativeID = "rep-b" }, ErrDenied},
		{"foreign grant", func(s *Snapshot, c *Change) { s.Grant.RepresentativeID = "rep-b" }, ErrDenied},
		{"revoked grant", func(s *Snapshot, c *Change) { s.Grant.Enabled = false }, ErrDenied},
		{"wrong granted inbound", func(s *Snapshot, c *Change) { s.Grant.InboundID = 8 }, ErrDenied},
		{"disabled representative", func(s *Snapshot, c *Change) { s.RepresentativeEnabled = false }, ErrDenied},
		{"suspended login", func(s *Snapshot, c *Change) { s.LoginSuspended = true }, ErrDenied},
		{"no management permission", func(s *Snapshot, c *Change) { s.CanManageClients = false }, ErrDenied},
		{"archived client", func(s *Snapshot, c *Change) { s.ClientArchived = true }, ErrDenied},
		{"grant changed after preview", func(s *Snapshot, c *Change) { s.Grant.Revision++ }, ErrConflict},
		{"execution changed after preview", func(s *Snapshot, c *Change) { s.ExecutionRevision++ }, ErrConflict},
		{"runtime not ready", func(s *Snapshot, c *Change) { s.Ready = false }, ErrConflict},
		{"forged target", func(s *Snapshot, c *Change) { c.ClientID = "foreign" }, ErrInvalid},
		{"forged inbound", func(s *Snapshot, c *Change) { c.InboundID = 8 }, ErrInvalid},
		{"duplicate stored members", func(s *Snapshot, c *Change) { s.Members = []string{"other", "other"} }, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, c := fixture()
			tc.edit(&s, &c)
			got, e := Membership("rep-a", s, c)
			if !errors.Is(e, tc.want) || got != nil {
				t.Fatalf("got %v %v", got, e)
			}
		})
	}
	s, c := fixture()
	if _, e := Membership("", s, c); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
