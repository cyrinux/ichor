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
	// ClearKeys are the values the file's own rules say are encrypted but are not.
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
	// What names the secret: sopsWhatSecret for a Kubernetes Secret, else the Talos key (cluster.secret).
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

	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		info.Format = sopsFormatJSON
	}

	docs, err := yamlDocuments([]byte(content))
	if err != nil || !anySopsDoc(docs) {
		dotenvSops(content, &info)

		return info
	}

	for _, d := range docs {
		m, _ := d.(map[string]any)

		meta, ok := m["sops"].(map[string]any)
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

		rules := newSopsKeyRules(meta)
		walkSopsValues(m, nil, rules, &info)
	}

	info.Recipients = uniqueRecipients(info.Recipients)

	return info
}

func anySopsDoc(docs []any) bool {
	for _, d := range docs {
		if m, ok := d.(map[string]any); ok {
			if _, ok := m["sops"].(map[string]any); ok {
				return true
			}
		}
	}

	return false
}

// sopsKeyRules decides, from a file's metadata, whether the value at a path should be
// encrypted, as SOPS does when it encrypts: encrypted_regex / unencrypted_regex on the keys,
// else the encrypted / unencrypted suffixes.
type sopsKeyRules struct {
	encrypted, unencrypted *regexp.Regexp
	encSuffix, clearSuffix string
}

func newSopsKeyRules(meta map[string]any) sopsKeyRules {
	var r sopsKeyRules

	if s, _ := meta["encrypted_regex"].(string); s != "" {
		r.encrypted, _ = regexp.Compile(s)
	}

	if s, _ := meta["unencrypted_regex"].(string); s != "" {
		r.unencrypted, _ = regexp.Compile(s)
	}

	r.encSuffix, _ = meta["encrypted_suffix"].(string)
	r.clearSuffix, _ = meta["unencrypted_suffix"].(string)

	if r.clearSuffix == "" && r.encSuffix == "" && r.encrypted == nil && r.unencrypted == nil {
		r.clearSuffix = sopsUnencryptedSuffix
	}

	return r
}

// shouldEncrypt tells the value under the keys of path is one SOPS encrypts.
func (r sopsKeyRules) shouldEncrypt(path []string) bool {
	for _, k := range path {
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
// that are not.
func walkSopsValues(v any, path []string, rules sopsKeyRules, info *sopsInfo) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range keysSorted(x) {
			if len(path) == 0 && k == "sops" {
				continue
			}

			walkSopsValues(x[k], append(slices.Clone(path), k), rules, info)
		}
	case []any:
		for i, item := range x {
			walkSopsValues(item, append(slices.Clone(path), fmt.Sprint(i)), rules, info)
		}
	case string:
		if strings.HasPrefix(x, sopsEncPrefix) {
			info.EncryptedKeys++

			return
		}

		addClearKey(path, rules, info)
	case nil:
	default:
		addClearKey(path, rules, info)
	}
}

func addClearKey(path []string, rules sopsKeyRules, info *sopsInfo) {
	if len(path) == 0 || !rules.shouldEncrypt(path) || len(info.ClearKeys) >= sopsMaxClearKeys {
		return
	}

	info.ClearKeys = append(info.ClearKeys, strings.Join(path, "."))
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

// dotenvSops reads a dotenv file's SOPS lines (sops_* keys) and counts its ENC[ values.
func dotenvSops(content string, info *sopsInfo) bool {
	found := false

	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}

		switch {
		case strings.HasPrefix(key, "sops_"):
			found = true

			switch {
			case strings.HasSuffix(key, "__map_recipient"):
				info.Recipients = append(info.Recipients, sopsRecipient{Type: "age", ID: value})
			case strings.HasSuffix(key, "__map_fp"):
				info.Recipients = append(info.Recipients, sopsRecipient{Type: "pgp", ID: value})
			case strings.HasSuffix(key, "__map_arn"):
				info.Recipients = append(info.Recipients, sopsRecipient{Type: "kms", ID: value})
			case key == "sops_lastmodified":
				info.LastModified = value
			}
		case strings.HasPrefix(value, sopsEncPrefix):
			info.EncryptedKeys++
		case !strings.HasSuffix(key, sopsUnencryptedSuffix) && len(info.ClearKeys) < sopsMaxClearKeys:
			info.ClearKeys = append(info.ClearKeys, key)
		}
	}

	if !found {
		info.ClearKeys = []string{}
		info.EncryptedKeys = 0

		return false
	}

	info.Encrypted, info.Format = true, sopsFormatDotenv
	info.Recipients = uniqueRecipients(info.Recipients)

	return true
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

func sopsRuleFor(sopsYAML, path string) (sopsRule, error) {
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
	p := strings.TrimPrefix(path, "/")

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

// sopsEditWarning tells whether content, proposed at path, would commit a clear-text secret
// where sopsYAML's rules encrypt: a Kubernetes Secret with values, or a Talos secret field,
// in a file with no sops block.
func sopsEditWarning(sopsYAML, path, content string) sopsWarning {
	rule, err := sopsRuleFor(sopsYAML, path)
	if err != nil || !rule.Matched {
		return sopsWarning{Rule: rule}
	}

	docs, err := yamlDocuments([]byte(content))
	if err != nil || anySopsDoc(docs) {
		return sopsWarning{Rule: rule}
	}

	what := ""

	for _, d := range docs {
		m, _ := d.(map[string]any)
		if m["kind"] == "Secret" && (m["data"] != nil || m["stringData"] != nil) {
			what = sopsWhatSecret

			break
		}
	}

	if key, ok := setsTalosSecret(docs); ok && what == "" {
		what = key
	}

	return sopsWarning{Warn: what != "", What: what, Rule: rule}
}

// markSOPSEntries flags the files of a listing SOPS encrypts by their name.
func markSOPSEntries(entries []gitForgeEntry) {
	for i := range entries {
		entries[i].SOPS = entries[i].Type != "dir" && isSOPSPath(entries[i].Path)
	}
}
