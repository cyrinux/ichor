package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
)

// stubDNS is a dnsChecker whose answers come from maps: a host absent from a map is
// NXDOMAIN, one in failures fails with that error.
type stubDNS struct {
	addresses map[string][]string
	cnames    map[string]string
	txts      map[string][]string
	failures  map[string]error
}

func (s stubDNS) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if err := s.failures[host]; err != nil {
		return nil, err
	}

	raw, ok := s.addresses[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	out := make([]netip.Addr, 0, len(raw))

	for _, a := range raw {
		addr, err := netip.ParseAddr(a)
		if err != nil {
			return nil, err
		}

		out = append(out, addr)
	}

	return out, nil
}

func (s stubDNS) LookupCNAME(_ context.Context, host string) (string, error) {
	if err := s.failures[host]; err != nil {
		return "", err
	}

	name, ok := s.cnames[host]
	if !ok {
		return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	return name, nil
}

func (s stubDNS) LookupTXT(_ context.Context, host string) ([]string, error) {
	if err := s.failures[host]; err != nil {
		return nil, err
	}

	values, ok := s.txts[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	return values, nil
}

func TestDNSCheckStatuses(t *testing.T) {
	resolver := stubDNS{
		addresses: map[string][]string{
			"app.example.com":   {"203.0.113.10", "203.0.113.11"},
			"other.example.com": {"198.51.100.7"},
			"svc.example.com":   {"203.0.113.30"},
			"elb.example.com":   {"203.0.113.30"},
		},
		cnames: map[string]string{
			"alias.example.com": "target.example.com.",
		},
		txts: map[string][]string{
			"a-app.example.com":   {"heritage=external-dns,external-dns/owner=homelab,external-dns/resource=ingress/demo/app"},
			"a-other.example.com": {"heritage=external-dns,external-dns/owner=other-cluster"},
		},
		failures: map[string]error{
			"slow.example.com": &net.DNSError{Err: "timeout", Name: "slow.example.com", IsTimeout: true},
		},
	}

	yes, no := true, false

	tests := []struct {
		name       string
		record     dnsCheckRequest
		status     string
		answers    []string
		owner      string
		ownerMatch *bool
	}{
		{
			name:       "an address among the targets matches",
			record:     dnsCheckRequest{DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}, OwnerID: "homelab"},
			status:     dnsStatusMatch,
			answers:    []string{"203.0.113.10", "203.0.113.11"},
			owner:      "homelab",
			ownerMatch: &yes,
		},
		{
			name:       "round-robin order does not matter",
			record:     dnsCheckRequest{DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.11"}, OwnerID: "homelab"},
			status:     dnsStatusMatch,
			answers:    []string{"203.0.113.10", "203.0.113.11"},
			owner:      "homelab",
			ownerMatch: &yes,
		},
		{
			name:       "answers that are none of the targets mismatch",
			record:     dnsCheckRequest{DNSName: "other.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}, OwnerID: "homelab"},
			status:     dnsStatusMismatch,
			answers:    []string{"198.51.100.7"},
			owner:      "other-cluster",
			ownerMatch: &no,
		},
		{
			name:    "a name that does not resolve is missing",
			record:  dnsCheckRequest{DNSName: "gone.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}},
			status:  dnsStatusMissing,
			answers: []string{},
		},
		{
			name:    "a resolver failure is an error, not fatal",
			record:  dnsCheckRequest{DNSName: "slow.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}},
			status:  dnsStatusError,
			answers: []string{},
		},
		{
			name:    "a CNAME that is a target matches",
			record:  dnsCheckRequest{DNSName: "alias.example.com", RecordType: "CNAME", Targets: []string{"target.example.com"}},
			status:  dnsStatusMatch,
			answers: []string{"target.example.com"},
		},
		{
			name:    "a CNAME elsewhere mismatches",
			record:  dnsCheckRequest{DNSName: "alias.example.com", RecordType: "CNAME", Targets: []string{"elsewhere.example.com"}},
			status:  dnsStatusMismatch,
			answers: []string{"target.example.com"},
		},
		{
			name:    "a hostname target counts through its addresses",
			record:  dnsCheckRequest{DNSName: "svc.example.com", RecordType: "A", Targets: []string{"elb.example.com"}},
			status:  dnsStatusMatch,
			answers: []string{"203.0.113.30"},
		},
		{
			name:       "no targets cannot match",
			record:     dnsCheckRequest{DNSName: "app.example.com", RecordType: "A", OwnerID: "homelab"},
			status:     dnsStatusMismatch,
			answers:    []string{"203.0.113.10", "203.0.113.11"},
			owner:      "homelab",
			ownerMatch: &yes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkDNSRecord(context.Background(), resolver, tt.record)

			if got.Status != tt.status {
				t.Errorf("status = %q, want %q", got.Status, tt.status)
			}

			if strings.Join(got.Answers, ",") != strings.Join(tt.answers, ",") {
				t.Errorf("answers = %v, want %v", got.Answers, tt.answers)
			}

			if got.Owner != tt.owner {
				t.Errorf("owner = %q, want %q", got.Owner, tt.owner)
			}

			switch {
			case tt.ownerMatch == nil && got.OwnerMatch != nil:
				t.Errorf("ownerMatch = %v, want nil", *got.OwnerMatch)
			case tt.ownerMatch != nil && got.OwnerMatch == nil:
				t.Errorf("ownerMatch = nil, want %v", *tt.ownerMatch)
			case tt.ownerMatch != nil && *got.OwnerMatch != *tt.ownerMatch:
				t.Errorf("ownerMatch = %v, want %v", *got.OwnerMatch, *tt.ownerMatch)
			}

			if tt.status == dnsStatusError && got.Message == "" {
				t.Error("an error verdict must carry a message")
			}
		})
	}
}

func TestDNSCheckOwnerUnknownWhenTXTUnreadable(t *testing.T) {
	// A resolver that answers addresses but refuses TXT (the Android Go resolver often
	// cannot serve TXT): the owner is unknown, never an error.
	resolver := stubDNS{
		addresses: map[string][]string{"app.example.com": {"203.0.113.10"}},
		failures:  map[string]error{"a-app.example.com": &net.DNSError{Err: "no such host", Name: "a-app.example.com", IsNotFound: true}},
	}

	got := checkDNSRecord(context.Background(), resolver, dnsCheckRequest{
		DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}, OwnerID: "homelab",
	})

	if got.Status != dnsStatusMatch {
		t.Errorf("status = %q, want %q", got.Status, dnsStatusMatch)
	}

	if got.Owner != "" || got.OwnerMatch != nil {
		t.Errorf("owner = %q, ownerMatch = %v, want unknown", got.Owner, got.OwnerMatch)
	}
}

func TestDNSCheckUsesTxtPrefix(t *testing.T) {
	resolver := stubDNS{
		addresses: map[string][]string{"app.example.com": {"203.0.113.10"}},
		txts:      map[string][]string{"_dns.app.example.com": {"heritage=external-dns,external-dns/owner=homelab"}},
	}

	got := checkDNSRecord(context.Background(), resolver, dnsCheckRequest{
		DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.10"},
		OwnerID: "homelab", TxtPrefix: "_dns.",
	})

	if got.Owner != "homelab" || got.OwnerMatch == nil || !*got.OwnerMatch {
		t.Errorf("owner = %q, ownerMatch = %v, want homelab/true", got.Owner, got.OwnerMatch)
	}
}

func TestDNSCheckInvalidJSON(t *testing.T) {
	if _, err := DNSCheck("{not json", 0); err == nil {
		t.Fatal("invalid JSON must fail the whole call")
	}
}

func TestDNSCheckCapsRecords(t *testing.T) {
	records := make([]dnsCheckRequest, dnsCheckRecordCap+25)
	for i := range records {
		records[i] = dnsCheckRequest{DNSName: fmt.Sprintf("host%d.example.com", i), RecordType: "A", Targets: []string{"203.0.113.10"}}
	}

	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}

	out, err := dnsCheckWith(stubDNS{addresses: map[string][]string{}}, string(raw), 0)
	if err != nil {
		t.Fatal(err)
	}

	var result dnsCheckResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}

	if len(result.Results) != dnsCheckRecordCap {
		t.Errorf("results = %d, want %d", len(result.Results), dnsCheckRecordCap)
	}

	if result.Resolver != "system" {
		t.Errorf("resolver = %q, want system", result.Resolver)
	}
}

func TestDemoDNSCheckShowsEveryChip(t *testing.T) {
	records := []dnsCheckRequest{
		{DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}, OwnerID: "homelab"},
		{DNSName: "shop.example.com", RecordType: "A", Targets: []string{"203.0.113.20"}, OwnerID: "homelab"},
		{DNSName: "api.example.com", RecordType: "A", Targets: []string{"203.0.113.30"}, OwnerID: "homelab"},
		{DNSName: "db.example.com", RecordType: "A", Targets: []string{"203.0.113.40"}, OwnerID: "homelab"},
		{DNSName: "status.example.com", RecordType: "A", OwnerID: "homelab"},
	}

	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}

	out, err := DemoDNSCheck(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	var result dnsCheckResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}

	if result.Resolver != "demo" {
		t.Errorf("resolver = %q, want demo", result.Resolver)
	}

	seen := map[string]bool{}
	for _, entry := range result.Results {
		seen[entry.Status] = true
	}

	for _, status := range []string{dnsStatusMatch, dnsStatusMismatch, dnsStatusMissing, dnsStatusError} {
		if !seen[status] {
			t.Errorf("demo never shows a %q chip", status)
		}
	}
}

func TestParseExternalDNSOwner(t *testing.T) {
	tests := []struct {
		value string
		owner string
		ok    bool
	}{
		{"heritage=external-dns,external-dns/owner=homelab,external-dns/resource=service/demo/shop", "homelab", true},
		{"external-dns/owner=homelab,heritage=external-dns", "homelab", true},
		{"heritage=external-dns", "", false},
		{"external-dns/owner=homelab", "", false},
		{"heritage=other,external-dns/owner=homelab", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		owner, ok := parseExternalDNSOwner(tt.value)
		if owner != tt.owner || ok != tt.ok {
			t.Errorf("parseExternalDNSOwner(%q) = %q, %v; want %q, %v", tt.value, owner, ok, tt.owner, tt.ok)
		}
	}
}
