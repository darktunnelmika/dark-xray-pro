package main

import "testing"

func TestLabDoesNotContactProductionOrRemoteNodes(t *testing.T) {
    accepted := []string{"http://127.0.0.1:2087"}
    for _, raw := range accepted {
        if _, err := checkLabEndpoint(raw); err != nil { t.Fatalf("%s: %v", raw, err) }
    }
    denied := []string{
        "http://old-hub.example:2087", "http://127.0.0.1:2088",
        "https://example.com", "http://localhost:2087",
        "http://127.0.0.1:2087/other", "http://127.0.0.1:2087?node=x",
    }
    for _, raw := range denied {
        if _, err := checkLabEndpoint(raw); err == nil {
            t.Fatalf("unsafe lab endpoint permitted: %s", raw)
        }
    }
}
