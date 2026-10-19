package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"
)

// dnsCheckRecordCap bounds one call: the app sends the records of one controller, and a
// controller with more than this is cut rather than refused.
const dnsCheckRecordCap = 200

// dnsCheckConcurrency is how many names are resolved at once.
const dnsCheckConcurrency = 8

// dnsCheckDefaultTimeout bounds the whole call when the app sends 0.
const dnsCheckDefaultTimeout = 10 * time.Second

// dnsProbeLabel is the label a wildcard name is resolved through: "*" is not a valid label,
// and any name under a wildcard with no record of its own answers what the wildcard answers
// (RFC 4592). Plain letters and a hyphen, so getaddrinfo on Android and iOS accepts it.
const dnsProbeLabel = "ichor-dns-check"

// errDNSWildcardDepth is a "*" anywhere but the first label, which the DNS does not treat as
// a wildcard and no resolver can be asked about.
var errDNSWildcardDepth = errors.New("a wildcard can only be the first label")

// dnsCheckOwnerTimeout bounds the ownership check on its own: a resolver that cannot serve
// TXT (the Android Go resolver often cannot) must not delay the verdict.
const dnsCheckOwnerTimeout = 2 * time.Second

// The verdicts a name can get. missing is a name that does not resolve, mismatch one that
// resolves elsewhere (split-horizon DNS, propagation, or another controller owning it), and
// error a resolver that could not answer at all.
const (
	dnsStatusMatch    = "match"
	dnsStatusMismatch = "mismatch"
	dnsStatusMissing  = "missing"
	dnsStatusError    = "error"
)

// dnsCheckRequest is one hostname to check, as the apps send it: the record ExternalDNS is
// asked to publish, plus the controller's owner id and TXT prefix for the ownership check.
type dnsCheckRequest struct {
	DNSName    string   `json:"dnsName"`
	RecordType string   `json:"recordType"`
	Targets    []string `json:"targets"`
	OwnerID    string   `json:"ownerId"`
	TxtPrefix  string   `json:"txtPrefix"`
}

// dnsCheckEntry is one hostname's verdict. OwnerMatch is null when no TXT record was
// readable, so the apps can say "owner unknown" rather than "not ours". Probed is the name
// actually resolved when it differs from DNSName (a wildcard's sample label), "" otherwise.
type dnsCheckEntry struct {
	DNSName    string   `json:"dnsName"`
	Probed     string   `json:"probed"`
	Status     string   `json:"status"`
	Answers    []string `json:"answers"`
	Owner      string   `json:"owner"`
	OwnerMatch *bool    `json:"ownerMatch"`
	MS         int64    `json:"ms"`
	Message    string   `json:"message"`
}

// dnsCheckResult is the whole call's answer.
type dnsCheckResult struct {
	Results  []dnsCheckEntry `json:"results"`
	Resolver string          `json:"resolver"`
}

// dnsChecker is the resolver the check needs: *net.Resolver in production, a stub in tests.
type dnsChecker interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupTXT(ctx context.Context, host string) ([]string, error)
}

// DNSCheck resolves the hostnames ExternalDNS is asked to publish from the phone's own
// resolver and says whether each one points at the cluster. It needs no kubeconfig: the
// records come from the app, the answers from the device. A name that does not resolve is
// missing, one that resolves elsewhere is a mismatch, and a resolver failure is an error,
// never fatal. Only invalid JSON fails the whole call.
func DNSCheck(recordsJSON string, timeoutMs int) (out string, err error) {
	defer maskResult(&out, &err)

	return dnsCheckWith(net.DefaultResolver, recordsJSON, timeoutMs)
}

// DemoDNSCheck is DNSCheck for the built-in demo: canned verdicts, no resolver, so the
// button works offline.
func DemoDNSCheck(recordsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var records []dnsCheckRequest
	if err := json.Unmarshal([]byte(recordsJSON), &records); err != nil {
		return "", err
	}

	return toJSON(dnsCheckResult{Results: demoDNSCheckEntries(records), Resolver: "demo"})
}

// dnsCheckWith is DNSCheck against an injected resolver, so the verdicts are testable
// without a network.
func dnsCheckWith(resolver dnsChecker, recordsJSON string, timeoutMs int) (string, error) {
	var records []dnsCheckRequest
	if err := json.Unmarshal([]byte(recordsJSON), &records); err != nil {
		return "", err
	}

	if len(records) > dnsCheckRecordCap {
		records = records[:dnsCheckRecordCap]
	}

	timeout := dnsCheckDefaultTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	results := make([]dnsCheckEntry, len(records))
	sem := make(chan struct{}, dnsCheckConcurrency)

	var wg sync.WaitGroup

	for i, record := range records {
		wg.Add(1)

		sem <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			results[i] = checkDNSRecord(ctx, resolver, record)
		}()
	}

	wg.Wait()

	return toJSON(dnsCheckResult{Results: results, Resolver: "system"})
}

// checkDNSRecord resolves one name and reads its ownership, timing the whole thing.
func checkDNSRecord(ctx context.Context, resolver dnsChecker, record dnsCheckRequest) dnsCheckEntry {
	start := time.Now()

	entry := dnsCheckEntry{DNSName: record.DNSName, Answers: []string{}}

	probe, err := dnsProbeName(record.DNSName)
	if err != nil {
		entry.Status = dnsStatusError
		entry.Message = err.Error()
		entry.MS = time.Since(start).Milliseconds()

		return entry
	}

	wildcard := probe != record.DNSName
	if wildcard {
		entry.Probed = probe
	}

	probed := record
	probed.DNSName = probe

	answers, err := lookupDNSAnswers(ctx, resolver, probed)
	if answers != nil {
		entry.Answers = answers
	}

	switch {
	case err != nil:
		entry.Status = dnsStatusError
		entry.Message = dnsLookupMessage(err)
	case len(answers) == 0:
		entry.Status = dnsStatusMissing
	case dnsAnswersMatch(ctx, resolver, answers, record.Targets):
		entry.Status = dnsStatusMatch
	default:
		entry.Status = dnsStatusMismatch
	}

	// Ownership only says something about a name that resolves: a missing one has no
	// registry record to read, and a failed lookup leaves no budget to read it with. A
	// wildcard's registry record only exists under --txt-wildcard-replacement, which the
	// check cannot see, so its owner stays unknown.
	if !wildcard && (entry.Status == dnsStatusMatch || entry.Status == dnsStatusMismatch) {
		entry.Owner, entry.OwnerMatch = dnsCheckOwner(ctx, resolver, record)
	}

	entry.MS = time.Since(start).Milliseconds()

	return entry
}

// dnsProbeName is the name the resolver is asked about: a leading "*." becomes the sample
// label, any other name is unchanged. A "*" deeper than the first label is refused.
func dnsProbeName(dnsName string) (string, error) {
	name := strings.TrimSpace(dnsName)

	rest, wildcard := strings.CutPrefix(name, "*.")
	if strings.Contains(rest, "*") || (!wildcard && name == "*") {
		return "", errDNSWildcardDepth
	}

	if !wildcard {
		return dnsName, nil
	}

	return dnsProbeLabel + "." + rest, nil
}

// lookupDNSAnswers resolves one record the way its type asks: A and AAAA through the address
// lookup, CNAME through the canonical name. A name that does not exist comes back as no
// answers, not an error.
func lookupDNSAnswers(ctx context.Context, resolver dnsChecker, record dnsCheckRequest) ([]string, error) {
	if strings.EqualFold(strings.TrimSpace(record.RecordType), "CNAME") {
		name, err := resolver.LookupCNAME(ctx, record.DNSName)
		if err != nil {
			if dnsNotFound(err) {
				return nil, nil
			}

			return nil, err
		}

		return []string{normalizeDNSName(name)}, nil
	}

	network := "ip"
	switch strings.ToUpper(strings.TrimSpace(record.RecordType)) {
	case "A":
		network = "ip4"
	case "AAAA":
		network = "ip6"
	}

	addrs, err := resolver.LookupNetIP(ctx, network, record.DNSName)
	if err != nil {
		if dnsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.Unmap().String())
	}

	return out, nil
}

// dnsAnswersMatch reports whether any answer is one of the targets. Addresses compare as
// addresses, so round-robin order does not matter; a target that is itself a hostname (an
// ELB name, a CNAME target) is resolved once and its addresses count too.
func dnsAnswersMatch(ctx context.Context, resolver dnsChecker, answers, targets []string) bool {
	if len(targets) == 0 {
		return false
	}

	normalized := make(map[string]bool, len(answers))
	for _, answer := range answers {
		normalized[normalizeDNSName(answer)] = true
	}

	for _, target := range targets {
		if normalized[normalizeDNSName(target)] {
			return true
		}
	}

	for _, target := range targets {
		if net.ParseIP(target) != nil {
			continue
		}

		addrs, err := resolver.LookupNetIP(ctx, "ip", target)
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if normalized[normalizeDNSName(addr.Unmap().String())] {
				return true
			}
		}
	}

	return false
}

// dnsCheckOwner reads the ExternalDNS TXT registry record for a name, best effort: the
// phone's resolver may not serve TXT at all, in which case the owner is unknown (a nil
// match) rather than an error.
func dnsCheckOwner(ctx context.Context, resolver dnsChecker, record dnsCheckRequest) (string, *bool) {
	ctx, cancel := context.WithTimeout(ctx, dnsCheckOwnerTimeout)
	defer cancel()

	for _, name := range dnsOwnerNames(record) {
		values, err := resolver.LookupTXT(ctx, name)
		if err != nil {
			continue
		}

		for _, value := range values {
			owner, ok := parseExternalDNSOwner(value)
			if !ok {
				continue
			}

			match := owner == record.OwnerID

			return owner, &match
		}
	}

	return "", nil
}

// dnsOwnerNames is where the TXT registry record may live: the controller's prefix plus the
// name, and the type-prefixed default (a- for A, cname- for CNAME).
func dnsOwnerNames(record dnsCheckRequest) []string {
	var names []string

	if prefix := strings.TrimSpace(record.TxtPrefix); prefix != "" {
		names = append(names, prefix+record.DNSName)
	}

	prefix := "a-"
	if strings.EqualFold(strings.TrimSpace(record.RecordType), "CNAME") {
		prefix = "cname-"
	}

	if name := prefix + record.DNSName; !slices.Contains(names, name) {
		names = append(names, name)
	}

	return names
}

// parseExternalDNSOwner reads the owner id out of an ExternalDNS TXT registry value, e.g.
// "heritage=external-dns,external-dns/owner=homelab,external-dns/resource=service/…".
func parseExternalDNSOwner(value string) (string, bool) {
	heritage, owner := false, ""

	for _, field := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			continue
		}

		switch strings.TrimSpace(key) {
		case "heritage":
			heritage = strings.TrimSpace(val) == "external-dns"
		case "external-dns/owner":
			owner = strings.TrimSpace(val)
		}
	}

	if !heritage || owner == "" {
		return "", false
	}

	return owner, true
}

// dnsNotFound reports whether err is a name that does not exist (NXDOMAIN): a missing
// record, not a resolver failure.
func dnsNotFound(err error) bool {
	var dnsErr *net.DNSError

	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// dnsLookupMessage is the short reason shown next to an error verdict.
func dnsLookupMessage(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsTimeout {
		return "the resolver timed out"
	}

	return "the resolver could not answer"
}

// normalizeDNSName lowercases a name and drops its trailing dot, so "App.Example.com." and
// "app.example.com" compare equal.
func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// demoDNSCheckEntries is the demo's canned verdicts: most names resolve to their targets,
// one points elsewhere, one is gone and one cannot be checked, so every chip is visible
// offline. A wildcard resolves through its sample label, as the real check does.
func demoDNSCheckEntries(records []dnsCheckRequest) []dnsCheckEntry {
	out := make([]dnsCheckEntry, 0, len(records))

	for i, record := range records {
		entry := dnsCheckEntry{DNSName: record.DNSName, Answers: []string{}, MS: int64(3 + i)}

		if probe, err := dnsProbeName(record.DNSName); err == nil && probe != record.DNSName {
			entry.Probed = probe
		}

		switch {
		case len(record.Targets) == 0:
			entry.Status = dnsStatusMissing
		case entry.Probed != "":
			entry.Status = dnsStatusMatch
			entry.Answers = []string{record.Targets[0]}
		case i%4 == 3:
			entry.Status = dnsStatusMismatch
			entry.Answers = []string{"198.51.100.7"}
		case i%4 == 2:
			entry.Status = dnsStatusError
			entry.Message = "the resolver could not answer"
		default:
			entry.Status = dnsStatusMatch
			entry.Answers = []string{record.Targets[0]}
		}

		if record.OwnerID != "" && entry.Probed == "" {
			owner, match := record.OwnerID, true
			if entry.Status == dnsStatusMismatch {
				owner, match = "other-cluster", false
			}

			entry.Owner, entry.OwnerMatch = owner, &match
		}

		out = append(out, entry)
	}

	return out
}
