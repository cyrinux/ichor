package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
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
			name:       "no targets resolves unverified, never mismatch",
			record:     dnsCheckRequest{DNSName: "app.example.com", RecordType: "A", OwnerID: "homelab"},
			status:     dnsStatusUnverified,
			answers:    []string{"203.0.113.10", "203.0.113.11"},
			owner:      "homelab",
			ownerMatch: &yes,
		},
		{
			name:    "no targets and nothing resolves stays missing",
			record:  dnsCheckRequest{DNSName: "gone.example.com", RecordType: "A"},
			status:  dnsStatusMissing,
			answers: []string{},
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

func TestDNSCheckCNAMEByAddresses(t *testing.T) {
	resolver := stubDNS{
		addresses: map[string][]string{
			"infra.example.com":   {"203.0.113.50", "2001:db8::50"},
			"flat.example.com":    {"203.0.113.50"},
			"chain.example.com":   {"2001:db8::50"},
			"edge.example.net":    {"203.0.113.50"},
			"moved.example.com":   {"198.51.100.9"},
			"partial.example.com": {"203.0.113.50"},
		},
		cnames: map[string]string{
			// getaddrinfo answers the name itself when it has addresses but no CNAME.
			"flat.example.com":  "flat.example.com.",
			"chain.example.com": "edge.example.net.",
			"moved.example.com": "lb.example.net.",
		},
		failures: map[string]error{
			"broken.example.com": &net.DNSError{Err: "server misbehaving", Name: "broken.example.com"},
		},
	}

	tests := []struct {
		name    string
		record  dnsCheckRequest
		status  string
		answers []string
	}{
		{
			name:    "a flattened CNAME with the target's addresses matches",
			record:  dnsCheckRequest{DNSName: "flat.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
			status:  dnsStatusMatch,
			answers: []string{"203.0.113.50"},
		},
		{
			name:    "a chain ending at another name with the same addresses matches",
			record:  dnsCheckRequest{DNSName: "chain.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
			status:  dnsStatusMatch,
			answers: []string{"edge.example.net", "2001:db8::50"},
		},
		{
			name:    "a CNAME to a name with other addresses mismatches and shows them",
			record:  dnsCheckRequest{DNSName: "moved.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
			status:  dnsStatusMismatch,
			answers: []string{"lb.example.net", "198.51.100.9"},
		},
		{
			name:    "a CNAME lookup that fails still counts the addresses",
			record:  dnsCheckRequest{DNSName: "partial.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
			status:  dnsStatusMatch,
			answers: []string{"203.0.113.50"},
		},
		{
			name:    "nothing resolves is missing",
			record:  dnsCheckRequest{DNSName: "gone.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
			status:  dnsStatusMissing,
			answers: []string{},
		},
		{
			name:    "a resolver that answers neither lookup is an error",
			record:  dnsCheckRequest{DNSName: "broken.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
			status:  dnsStatusError,
			answers: []string{},
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
		})
	}
}

// count is how many times the resolver was asked about host.
func (r *recordingDNS) count(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := 0

	for _, q := range r.queried {
		if q == host {
			n++
		}
	}

	return n
}

func TestDNSCheckResolvesSharedTargetOnce(t *testing.T) {
	resolver := &recordingDNS{stubDNS: stubDNS{
		addresses: map[string][]string{
			"infra.example.com": {"203.0.113.50"},
			"a.example.com":     {"203.0.113.50"},
			"b.example.com":     {"203.0.113.50"},
			"c.example.com":     {"203.0.113.50"},
		},
	}}

	records := []dnsCheckRequest{
		{DNSName: "a.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
		{DNSName: "b.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
		{DNSName: "c.example.com", RecordType: "CNAME", Targets: []string{"infra.example.com"}},
	}

	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}

	out, err := dnsCheckWith(resolver, string(raw), 0)
	if err != nil {
		t.Fatal(err)
	}

	var result dnsCheckResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}

	for _, entry := range result.Results {
		if entry.Status != dnsStatusMatch {
			t.Errorf("%s = %q, want match", entry.DNSName, entry.Status)
		}
	}

	if n := resolver.count("infra.example.com"); n != 1 {
		t.Errorf("the shared target was resolved %d times, want once", n)
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

// recordingDNS is a stubDNS that remembers every name it was asked about.
type recordingDNS struct {
	stubDNS
	mu      sync.Mutex
	queried []string
}

func (r *recordingDNS) note(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.queried = append(r.queried, host)
}

func (r *recordingDNS) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	r.note(host)

	return r.stubDNS.LookupNetIP(ctx, network, host)
}

func (r *recordingDNS) LookupCNAME(ctx context.Context, host string) (string, error) {
	r.note(host)

	return r.stubDNS.LookupCNAME(ctx, host)
}

func (r *recordingDNS) LookupTXT(ctx context.Context, host string) ([]string, error) {
	r.note(host)

	return r.stubDNS.LookupTXT(ctx, host)
}

func TestDNSProbeName(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{name: "app.example.com", want: "app.example.com"},
		{name: "*.cdn.example.com", want: "ichor-dns-check.cdn.example.com"},
		{name: "*.example.com", want: "ichor-dns-check.example.com"},
		{name: "*.*.example.com", wantErr: true},
		{name: "app.*.example.com", wantErr: true},
		{name: "*", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dnsProbeName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("probe = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDNSCheckWildcard(t *testing.T) {
	tests := []struct {
		name      string
		record    dnsCheckRequest
		addresses map[string][]string
		cnames    map[string]string
		status    string
		probed    string
		wantQuery string
		message   string
	}{
		{
			name:      "wildcard A resolves through the sample label",
			record:    dnsCheckRequest{DNSName: "*.cdn.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}, OwnerID: "homelab"},
			addresses: map[string][]string{"ichor-dns-check.cdn.example.com": {"203.0.113.10"}},
			status:    dnsStatusMatch,
			probed:    "ichor-dns-check.cdn.example.com",
			wantQuery: "ichor-dns-check.cdn.example.com",
		},
		{
			name:      "wildcard CNAME resolves through the sample label",
			record:    dnsCheckRequest{DNSName: "*.tun.example.com", RecordType: "CNAME", Targets: []string{"edge.example.net"}},
			cnames:    map[string]string{"ichor-dns-check.tun.example.com": "edge.example.net."},
			status:    dnsStatusMatch,
			probed:    "ichor-dns-check.tun.example.com",
			wantQuery: "ichor-dns-check.tun.example.com",
		},
		{
			name:      "wildcard with no record is missing",
			record:    dnsCheckRequest{DNSName: "*.objects.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}},
			status:    dnsStatusMissing,
			probed:    "ichor-dns-check.objects.example.com",
			wantQuery: "ichor-dns-check.objects.example.com",
		},
		{
			name:    "double wildcard is an error and never resolved",
			record:  dnsCheckRequest{DNSName: "*.*.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}},
			status:  dnsStatusError,
			message: "a wildcard can only be the first label",
		},
		{
			name:      "plain name has no probed name",
			record:    dnsCheckRequest{DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}},
			addresses: map[string][]string{"app.example.com": {"203.0.113.10"}},
			status:    dnsStatusMatch,
			wantQuery: "app.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &recordingDNS{stubDNS: stubDNS{
				addresses: tt.addresses,
				cnames:    tt.cnames,
				// A TXT record at every name a wildcard could be looked up under: the check
				// must still not read it.
				txts: map[string][]string{
					"a-ichor-dns-check.cdn.example.com": {"heritage=external-dns,external-dns/owner=homelab"},
					"a-*.cdn.example.com":               {"heritage=external-dns,external-dns/owner=homelab"},
				},
			}}

			got := checkDNSRecord(context.Background(), resolver, tt.record)

			if got.Status != tt.status {
				t.Errorf("status = %q, want %q", got.Status, tt.status)
			}

			if got.Probed != tt.probed {
				t.Errorf("probed = %q, want %q", got.Probed, tt.probed)
			}

			if got.DNSName != tt.record.DNSName {
				t.Errorf("dnsName = %q, want %q", got.DNSName, tt.record.DNSName)
			}

			if tt.message != "" && got.Message != tt.message {
				t.Errorf("message = %q, want %q", got.Message, tt.message)
			}

			for _, q := range resolver.queried {
				if strings.Contains(q, "*") {
					t.Errorf("resolver was asked about %q", q)
				}
			}

			if tt.wantQuery != "" && (len(resolver.queried) == 0 || resolver.queried[0] != tt.wantQuery) {
				t.Errorf("queried = %v, want first %q", resolver.queried, tt.wantQuery)
			}

			if tt.probed != "" {
				if got.Owner != "" || got.OwnerMatch != nil {
					t.Errorf("a wildcard's owner must stay unknown, got %q/%v", got.Owner, got.OwnerMatch)
				}

				for _, q := range resolver.queried {
					if strings.HasPrefix(q, "a-") || strings.HasPrefix(q, "cname-") {
						t.Errorf("a wildcard's TXT owner was looked up at %q", q)
					}
				}
			}
		})
	}
}

func TestDemoDNSCheckWildcardResolves(t *testing.T) {
	entries := demoDNSCheckEntries([]dnsCheckRequest{
		{DNSName: "app.example.com", RecordType: "A", Targets: []string{"203.0.113.10"}},
		{DNSName: "*.cdn.example.com", RecordType: "A", Targets: []string{"203.0.113.20"}, OwnerID: "homelab"},
	})

	got := entries[1]
	if got.Status != dnsStatusMatch || got.Probed != "ichor-dns-check.cdn.example.com" {
		t.Errorf("demo wildcard = %q probed %q, want match through the sample label", got.Status, got.Probed)
	}

	if len(got.Answers) != 1 || got.Answers[0] != "203.0.113.20" {
		t.Errorf("demo wildcard answers = %v, want the first target", got.Answers)
	}

	if entries[0].Probed != "" {
		t.Errorf("plain name probed = %q, want empty", entries[0].Probed)
	}
}

func TestDNSCheckInvalidJSON(t *testing.T) {
	if _, err := dnsCheckWith(stubDNS{}, "{not json", 0); err == nil {
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
		{DNSName: "old.example.com", RecordType: "A", Targets: []string{"203.0.113.50"}, OwnerID: "homelab"},
		{DNSName: "status.example.com", RecordType: "A", OwnerID: "homelab"},
	}

	seen := map[string]bool{}
	for _, entry := range demoDNSCheckEntries(records) {
		seen[entry.Status] = true
	}

	for _, status := range []string{dnsStatusMatch, dnsStatusMismatch, dnsStatusMissing, dnsStatusUnverified, dnsStatusError} {
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

// The TXT names the ownership check reads follow the registry: the lower-case type token
// (a-, aaaa-, cname-), a prefix or suffix with %{record_type} replaced (and no type token
// then), the suffix on the first label, and the type-less names older releases wrote.
func TestDNSOwnerNames(t *testing.T) {
	tests := []struct {
		name           string
		recordType     string
		prefix, suffix string
		want           []string
	}{
		{"plain A", "A", "", "", []string{"a-app.example.com"}},
		{"plain AAAA", "AAAA", "", "", []string{"aaaa-app.example.com"}},
		{"plain CNAME", "CNAME", "", "", []string{"cname-app.example.com"}},
		{"plain prefix", "A", "_dns.", "", []string{"_dns.a-app.example.com", "_dns.app.example.com", "a-_dns.app.example.com"}},
		{"templated prefix", "AAAA", "%{record_type}-abc-", "", []string{"aaaa-abc-app.example.com", "-abc-app.example.com"}},
		{"templated prefix CNAME", "CNAME", "%{record_type}-", "", []string{"cname-app.example.com", "-app.example.com"}},
		{"suffix", "A", "", "-owner", []string{"a-app-owner.example.com", "app-owner.example.com"}},
		{"templated suffix", "CNAME", "", "-%{record_type}", []string{"app-cname.example.com", "app-.example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dnsOwnerNames(dnsCheckRequest{DNSName: "app.example.com", RecordType: tt.recordType, TxtPrefix: tt.prefix, TxtSuffix: tt.suffix})
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// An AAAA record's owner is read at aaaa-<name>, and the suffix travels in the request JSON.
func TestDNSCheckOwnerAAAAWithSuffix(t *testing.T) {
	resolver := stubDNS{
		addresses: map[string][]string{"app.example.com": {"2001:db8::10"}},
		txts:      map[string][]string{"aaaa-app-owner.example.com": {"heritage=external-dns,external-dns/owner=homelab"}},
	}

	var record dnsCheckRequest
	if err := json.Unmarshal([]byte(`{"dnsName":"app.example.com","recordType":"AAAA","targets":["2001:db8::10"],"ownerId":"homelab","txtSuffix":"-owner"}`), &record); err != nil {
		t.Fatal(err)
	}

	got := checkDNSRecord(context.Background(), resolver, record)
	if got.Status != dnsStatusMatch || got.Owner != "homelab" || got.OwnerMatch == nil || !*got.OwnerMatch {
		t.Errorf("got %+v", got)
	}
}
