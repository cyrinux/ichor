package ichorgo

import (
	"bufio"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// SOPS files in a repository, read and never decrypted: who a file is encrypted for (its
// metadata block), which values are still in clear, which .sops.yaml creation rule covers a
// path, and whether an edit would commit a clear-text secret where the rules encrypt.

const (
	sopsFormatYAML   = "yaml"
	sopsFormatJSON   = "json"
	sopsFormatDotenv = "dotenv"
	sopsFormatBinary = "binary"

	sopsEncPrefix = "ENC["
	// sopsUnencryptedSuffix is SOPS's default: a key ending with it stays in clear.
	sopsUnencryptedSuffix = "_unencrypted"
	// sopsMaxClearKeys caps the clear keys named.
	sopsMaxClearKeys = 20
	// sopsWhatSecret is a Kubernetes Secret in a sopsWarning: the apps word it.
	sopsWhatSecret = "Secret"
	// sopsWhatTalos is a Talos secrets bundle (talsecret.yaml, secrets.yaml): the apps word it.
	sopsWhatTalos = "TalosSecrets"
)

type sopsRecipient struct {
	Type string `json:"type"` // age, pgp, kms, gcp_kms, azure_kv, hc_vault
	ID   string `json:"id"`
}

type sopsInfo struct {
	Encrypted      bool            `json:"encrypted"`
	Format         string          `json:"format"`
	Recipients     []sopsRecipient `json:"recipients"`
	LastModified   string          `json:"lastModified,omitempty"`
	EncryptedRegex string          `json:"encryptedRegex,omitempty"`
	EncryptedKeys  int             `json:"encryptedKeys"`
	// ClearKeys are the values the file's own rules say are encrypted but are not; none are
	// named when the rules are comments the app does not read.
	ClearKeys []string `json:"clearKeys"`
}

type sopsRule struct {
	Matched          bool            `json:"matched"`
	PathRegex        string          `json:"pathRegex,omitempty"`
	Recipients       []sopsRecipient `json:"recipients"`
	EncryptedRegex   string          `json:"encryptedRegex,omitempty"`
	UnencryptedRegex string          `json:"unencryptedRegex,omitempty"`
}

// sopsWarning is an edit that would commit a clear-text secret where a rule encrypts.
type sopsWarning struct {
	Warn bool `json:"warn"`
	// What names the secret: sopsWhatSecret, sopsWhatTalos, or the Talos key (cluster.secret).
	What string   `json:"what,omitempty"`
	Rule sopsRule `json:"rule"`
}

// SopsInspect reads a file's SOPS metadata, never its values: {"encrypted","format":
// "yaml|json|dotenv|binary","recipients":[{"type","id"}],"lastModified","encryptedRegex",
// "encryptedKeys","clearKeys"}. A file with a sops block whose values meant to be encrypted are
// not all ENC[…] names them in clearKeys (a half-encrypted file). Local.
func SopsInspect(content string) (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(inspectSops(content))
}

func inspectSops(content string) sopsInfo {
	info := sopsInfo{Format: sopsFormatYAML, Recipients: []sopsRecipient{}, ClearKeys: []string{}}

	if strings.HasPrefix(strings.TrimSpace(content), "{") {
		info.Format = sopsFormatJSON
	}

	docs, err := yamlDocuments([]byte(content))
	if err != nil || !anySopsDoc(docs) {
		dotenvSops(content, &info)

		return info
	}

	for _, d := range docs {
		m, _ := d.(map[string]any)

		meta, ok := sopsMeta(m)
		if !ok {
			continue
		}

		info.Encrypted = true
		info.Recipients = append(info.Recipients, sopsRecipients(meta)...)
		info.LastModified, _ = meta["lastmodified"].(string)
		info.EncryptedRegex, _ = meta["encrypted_regex"].(string)

		if _, binary := m["data"].(string); binary && len(m) == 2 {
			info.Format = sopsFormatBinary
		}

		walkSopsValues(m, nil, nil, newSopsKeyRules(meta), &info)
	}

	info.Recipients = uniqueRecipients(info.Recipients)

	return info
}

// sopsMeta is a document's SOPS metadata block: a sops map with the MAC or the version SOPS
// always writes (a values file's own "sops:" section is not one).
func sopsMeta(m map[string]any) (map[string]any, bool) {
	meta, ok := m["sops"].(map[string]any)
	if !ok || (meta["mac"] == nil && meta["version"] == nil) {
		return nil, false
	}

	return meta, true
}

func anySopsDoc(docs []any) bool {
	for _, d := range docs {
		if m, ok := d.(map[string]any); ok {
			if _, ok := sopsMeta(m); ok {
				return true
			}
		}
	}

	return false
}

// sopsKeyRules decides, from a file's metadata, whether the value at a path should be
// encrypted, as SOPS does when it encrypts: encrypted_regex / unencrypted_regex on the keys,
// else the encrypted / unencrypted suffixes. Comment regexes are not read: unknown is then
// set and no clear key is named.
type sopsKeyRules struct {
	encrypted, unencrypted *regexp.Regexp
	encSuffix, clearSuffix string
	unknown                bool
}

func newSopsKeyRules(meta map[string]any) sopsKeyRules {
	var r sopsKeyRules

	text := func(k string) string { s, _ := meta[k].(string); return s }

	if s := text("encrypted_regex"); s != "" {
		r.encrypted, _ = regexp.Compile(s)
	}

	if s := text("unencrypted_regex"); s != "" {
		r.unencrypted, _ = regexp.Compile(s)
	}

	r.encSuffix, r.clearSuffix = text("encrypted_suffix"), text("unencrypted_suffix")
	r.unknown = text("encrypted_comment_regex") != "" || text("unencrypted_comment_regex") != ""

	if r.clearSuffix == "" && r.encSuffix == "" && r.encrypted == nil && r.unencrypted == nil {
		r.clearSuffix = sopsUnencryptedSuffix
	}

	return r
}

// shouldEncrypt tells the value under keys (the map keys on its path: list items keep their
// parent's, as SOPS walks them) is one SOPS encrypts.
func (r sopsKeyRules) shouldEncrypt(keys []string) bool {
	for _, k := range keys {
		switch {
		case r.encrypted != nil && r.encrypted.MatchString(k):
			return true
		case r.unencrypted != nil && r.unencrypted.MatchString(k):
			return false
		case r.encSuffix != "" && strings.HasSuffix(k, r.encSuffix):
			return true
		case r.clearSuffix != "" && strings.HasSuffix(k, r.clearSuffix):
			return false
		}
	}

	// A regex or a suffix to opt in: nothing matched, so it stays clear.
	return r.encrypted == nil && r.encSuffix == ""
}

// walkSopsValues counts the encrypted values of v and names the ones meant to be encrypted
// that are not. name is the displayed path (list indices in it), keys the map keys only.
func walkSopsValues(v any, name, keys []string, rules sopsKeyRules, info *sopsInfo) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range keysSorted(x) {
			if len(name) == 0 && k == "sops" {
				continue
			}

			walkSopsValues(x[k], append(slices.Clone(name), k), append(slices.Clone(keys), k), rules, info)
		}
	case map[any]any:
		// A mapping with number keys (ports: {80: …}).
		for k, item := range x {
			key := fmt.Sprint(k)
			walkSopsValues(item, append(slices.Clone(name), key), append(slices.Clone(keys), key), rules, info)
		}
	case []any:
		for i, item := range x {
			walkSopsValues(item, append(slices.Clone(name), fmt.Sprint(i)), keys, rules, info)
		}
	case string:
		if strings.HasPrefix(x, sopsEncPrefix) {
			info.EncryptedKeys++

			return
		}

		addClearKey(name, keys, rules, info)
	case nil:
	default:
		addClearKey(name, keys, rules, info)
	}
}

func addClearKey(name, keys []string, rules sopsKeyRules, info *sopsInfo) {
	if len(name) == 0 || rules.unknown || !rules.shouldEncrypt(keys) || len(info.ClearKeys) >= sopsMaxClearKeys {
		return
	}

	info.ClearKeys = append(info.ClearKeys, strings.Join(name, "."))
}

// sopsRecipients are the keys a metadata block (or a creation rule) names, key groups included.
func sopsRecipients(meta map[string]any) []sopsRecipient {
	var out []sopsRecipient

	for _, f := range []struct{ key, typ, id string }{
		{"age", "age", "recipient"}, {"pgp", "pgp", "fp"}, {"kms", "kms", "arn"},
		{"gcp_kms", "gcp_kms", "resource_id"}, {"azure_kv", "azure_kv", "vault_url"},
		{"hc_vault", "hc_vault", "vault_address"},
	} {
		out = append(out, recipientsOf(meta[f.key], f.typ, f.id)...)
	}

	if groups, ok := meta["key_groups"].([]any); ok {
		for _, g := range groups {
			if gm, ok := g.(map[string]any); ok {
				out = append(out, sopsRecipients(gm)...)
			}
		}
	}

	return out
}

// recipientsOf reads one list of keys: a string (comma separated, as .sops.yaml writes them),
// a list of strings, or a list of objects holding the id under idKey.
func recipientsOf(v any, typ, idKey string) []sopsRecipient {
	var out []sopsRecipient

	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, sopsRecipient{Type: typ, ID: id})
		}
	}

	switch x := v.(type) {
	case string:
		for _, id := range strings.Split(x, ",") {
			add(id)
		}
	case []any:
		for _, item := range x {
			switch it := item.(type) {
			case string:
				add(it)
			case int, int64, uint64, float64:
				// An unquoted all-digit fingerprint reads as a number.
				add(fmt.Sprint(it))
			case map[string]any:
				id, _ := it[idKey].(string)
				if typ == "azure_kv" {
					name, _ := it["name"].(string)
					id = strings.TrimSuffix(id, "/") + "/" + name
				}

				add(id)
			}
		}
	}

	return out
}

func uniqueRecipients(rs []sopsRecipient) []sopsRecipient {
	out := []sopsRecipient{}

	for _, r := range rs {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}

	return out
}

// dotenvSops reads a dotenv file's SOPS lines (sops_* keys: its metadata and rules, read
// first) and its values, counted as encrypted or named as clear by those rules.
func dotenvSops(content string, info *sopsInfo) {
	meta := map[string]any{}

	var values [][2]string

	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}

		if name, isMeta := strings.CutPrefix(key, "sops_"); isMeta {
			meta[name] = value

			switch {
			case strings.HasSuffix(key, "__map_recipient"):
				info.Recipients = append(info.Recipients, sopsRecipient{Type: "age", ID: value})
			case strings.HasSuffix(key, "__map_fp"):
				info.Recipients = append(info.Recipients, sopsRecipient{Type: "pgp", ID: value})
			case strings.HasSuffix(key, "__map_arn"):
				info.Recipients = append(info.Recipients, sopsRecipient{Type: "kms", ID: value})
			}

			continue
		}

		values = append(values, [2]string{key, value})
	}

	// SOPS always writes these: a file with only some other sops_ key is a plain one.
	if meta["mac"] == nil && meta["version"] == nil {
		info.Recipients = []sopsRecipient{}

		return
	}

	info.Encrypted, info.Format = true, sopsFormatDotenv
	info.Recipients = uniqueRecipients(info.Recipients)
	info.LastModified, _ = meta["lastmodified"].(string)
	info.EncryptedRegex, _ = meta["encrypted_regex"].(string)
	rules := newSopsKeyRules(meta)

	for _, kv := range values {
		if strings.HasPrefix(kv[1], sopsEncPrefix) {
			info.EncryptedKeys++
		} else {
			addClearKey([]string{kv[0]}, []string{kv[0]}, rules, info)
		}
	}
}

// SopsRuleFor is the .sops.yaml creation rule that covers path (from the repository root, as
// SOPS run there matches it): {"matched","pathRegex","recipients","encryptedRegex",
// "unencryptedRegex"}. The first rule whose path_regex matches; one without path_regex matches
// every path. No rule, or no .sops.yaml (""), is {"matched":false}. Local.
func SopsRuleFor(sopsYAML, path string) (out string, err error) {
	defer maskResult(&out, &err)

	rule, err := sopsRuleFor(sopsYAML, path)
	if err != nil {
		return "", err
	}

	return toJSON(rule)
}

func sopsRuleFor(sopsYAML, p string) (sopsRule, error) {
	none := sopsRule{Recipients: []sopsRecipient{}}
	if strings.TrimSpace(sopsYAML) == "" {
		return none, nil
	}

	docs, err := yamlDocuments([]byte(sopsYAML))
	if err != nil {
		return none, fmt.Errorf(".sops.yaml is not YAML: %w", err)
	}

	if len(docs) == 0 {
		return none, nil
	}

	root, _ := docs[0].(map[string]any)
	rules, _ := root["creation_rules"].([]any)
	p = strings.TrimPrefix(p, "/")

	for _, raw := range rules {
		r, _ := raw.(map[string]any)

		pathRegex, _ := r["path_regex"].(string)
		if pathRegex != "" {
			re, err := regexp.Compile(pathRegex)
			if err != nil || !re.MatchString(p) {
				continue
			}
		}

		rule := sopsRule{Matched: true, PathRegex: pathRegex, Recipients: uniqueRecipients(ruleRecipients(r))}
		rule.EncryptedRegex, _ = r["encrypted_regex"].(string)
		rule.UnencryptedRegex, _ = r["unencrypted_regex"].(string)

		return rule, nil
	}

	return none, nil
}

// ruleRecipients are a creation rule's keys: age, pgp, kms… as strings, and key groups.
func ruleRecipients(r map[string]any) []sopsRecipient {
	out := sopsRecipients(r)

	for _, f := range []struct{ key, typ string }{{"azure_keyvault", "azure_kv"}, {"hc_vault_transit_uri", "hc_vault"}} {
		out = append(out, recipientsOf(r[f.key], f.typ, "")...)
	}

	return out
}

// SopsEditWarning tells whether content, proposed at path (from the repository root), would
// commit a clear-text secret where the repository's .sops.yaml (sopsYAML, "" for none)
// encrypts: {"warn","what":"Secret|TalosSecrets|<Talos key>","rule":<SopsRuleFor>}. A
// Kubernetes Secret with values (in a List too) or a Talos secret, in a document with no sops
// block. Local: the apps read .sops.yaml at the branch the edit goes to.
func SopsEditWarning(sopsYAML, path, content string) (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(sopsEditWarning(sopsYAML, path, content))
}

func sopsEditWarning(sopsYAML, p, content string) sopsWarning {
	rule, err := sopsRuleFor(sopsYAML, p)
	if err != nil || !rule.Matched {
		return sopsWarning{Rule: rule}
	}

	docs, err := yamlDocuments([]byte(content))
	if err != nil {
		return sopsWarning{Rule: rule}
	}

	// Each document on its own: an encrypted one does not cover a clear one beside it.
	for _, d := range docs {
		m, _ := d.(map[string]any)
		if _, encrypted := sopsMeta(m); encrypted || m == nil {
			continue
		}

		if what := clearSecret(m); what != "" {
			return sopsWarning{Warn: true, What: what, Rule: rule}
		}
	}

	return sopsWarning{Rule: rule}
}

// clearSecret names the secret a clear document holds: a Kubernetes Secret with values (also
// among a List's items), a Talos secret field, or a Talos secrets bundle; "" when none.
func clearSecret(m map[string]any) string {
	if m["kind"] == "Secret" && (holdsValue(m["data"]) || holdsValue(m["stringData"])) {
		return sopsWhatSecret
	}

	if items, ok := m["items"].([]any); ok && strings.HasSuffix(fmt.Sprint(m["kind"]), "List") {
		for _, it := range items {
			if im, ok := it.(map[string]any); ok {
				if what := clearSecret(im); what != "" {
					return what
				}
			}
		}
	}

	if key, ok := setsTalosSecret([]any{m}); ok {
		return key
	}

	// talosctl gen secrets / talhelper gensecret: secrets, certs and trustdinfo at the top.
	if holdsValue(m["secrets"]) && (holdsValue(m["certs"]) || holdsValue(m["trustdinfo"])) {
		return sopsWhatTalos
	}

	return ""
}

// holdsValue tells a value holds something: a non-empty map, list or string.
func holdsValue(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case string:
		return x != ""
	case nil:
		return false
	default:
		return true
	}
}

// markSOPSEntries flags the files of a listing SOPS encrypts by their name.
func markSOPSEntries(entries []gitForgeEntry) {
	for i := range entries {
		entries[i].SOPS = entries[i].Type != "dir" && isSOPSPath(entries[i].Path)
	}
}
