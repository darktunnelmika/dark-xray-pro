package nodeagent

import (
	"fmt"
	"strings"
	"testing"
)

func TestLogRing(t *testing.T) {
	r := NewLogRing(5)
	fmt.Fprint(r, "a 1\nb [Warning] 2\nc 3\n")
	fmt.Fprint(r, "d [Error] 4\ne 5\nf 6\npartial")
	got := r.Tail(10, false, "")
	if strings.Join(got, ",") != "b [Warning] 2,c 3,d [Error] 4,e 5,f 6" {
		t.Fatal(got)
	}
	if got := r.Tail(10, true, ""); len(got) != 2 || got[1] != "d [Error] 4" {
		t.Fatal(got)
	}
	if got := r.Tail(2, false, ""); strings.Join(got, ",") != "e 5,f 6" {
		t.Fatal(got)
	}
	fmt.Fprint(r, " line\n")
	if got := r.Tail(1, false, "PARTIAL"); len(got) != 1 || got[0] != "partial line" {
		t.Fatal(got)
	}
}

func TestDropTrusted(t *testing.T) {
	online := map[string][]string{"ann": {"10.1.2.3", "5.5.5.5", "2001:db8::7"}, "ben": {"10.9.9.9"}}
	got := DropTrusted(online, []string{"10.0.0.0/8", "2001:db8::7", "bad"})
	if len(got["ann"]) != 1 || got["ann"][0] != "5.5.5.5" || len(got["ben"]) != 0 || len(online["ann"]) != 3 {
		t.Fatal(got, online)
	}
}
