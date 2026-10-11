package ichorgo

import (
	"net/url"
	"strings"
)

// An ~/.aws/config written by `aws configure sso` holds no secret: the access portal's
// start URL and region, and per profile the account, role and default region. Pasted or
// scanned, it routes to the EKS Identity Center sign-in with those fields filled.

// awsConfigImport is the Identity Center sign-in an ~/.aws/config describes.
type awsConfigImport struct {
	StartURL, SSORegion, AccountID, RoleName, Region, Profile string
}

// awsINISection is one `[kind name]` section of an AWS config or credentials file.
type awsINISection struct {
	kind, name string
	keys       map[string]string
}

// readAWSINI reads the sections of an AWS config file, or false when the text is not one:
// every line is a section header, a `key = value` (or `key: value`) pair, a comment, blank,
// or an indented sub-setting (ignored). A paste indented as a whole (from a chat or a
// terminal) and a leading byte-order mark are accepted.
func readAWSINI(text string) ([]awsINISection, bool) {
	var sections []awsINISection

	lines := strings.Split(strings.TrimPrefix(text, "\uFEFF"), "\n")
	indent := awsINIIndent(lines)

	for _, raw := range lines {
		line := strings.TrimPrefix(strings.TrimRight(raw, " \t\r"), indent)
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';':
		case trimmed[0] == '[':
			header, _, ok := strings.Cut(trimmed[1:], "]")
			if !ok {
				return nil, false
			}

			kind, name, _ := strings.Cut(strings.TrimSpace(header), " ")
			sections = append(sections, awsINISection{kind: kind, name: strings.TrimSpace(name), keys: map[string]string{}})
		case len(sections) == 0:
			return nil, false
		case line[0] == ' ' || line[0] == '\t':
		default:
			sep := strings.IndexAny(trimmed, "=:")
			if sep < 0 {
				return nil, false
			}

			key, value := trimmed[:sep], trimmed[sep+1:]
			for _, comment := range []string{" ;", "\t;", " #", "\t#"} {
				value, _, _ = strings.Cut(value, comment)
			}

			sections[len(sections)-1].keys[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	return sections, len(sections) > 0
}

// awsINIIndent is the leading whitespace of the first non-blank line: a whole file pasted
// indented is read as if it were not.
func awsINIIndent(lines []string) string {
	for _, l := range lines {
		if t := strings.TrimLeft(l, " \t"); strings.TrimSpace(t) != "" {
			return l[:len(l)-len(t)]
		}
	}

	return ""
}

// parseAWSConfig resolves the first profile that signs in with Identity Center (through an
// sso-session or the legacy keys in the profile), else the first sso-session alone.
func parseAWSConfig(text string) (awsConfigImport, bool) {
	sections, ok := readAWSINI(text)
	if !ok {
		return awsConfigImport{}, false
	}

	sessions := map[string]map[string]string{}

	for _, s := range sections {
		if s.kind == "sso-session" {
			sessions[s.name] = s.keys
		}
	}

	for _, s := range sections {
		name, isProfile := awsProfileName(s)
		if !isProfile {
			continue
		}

		portal := s.keys
		if session := s.keys["sso_session"]; session != "" {
			portal = sessions[session]
		}

		out := awsConfigImport{
			StartURL: portal["sso_start_url"], SSORegion: portal["sso_region"],
			AccountID: s.keys["sso_account_id"], RoleName: s.keys["sso_role_name"],
			Region: s.keys["region"], Profile: name,
		}
		if out.valid() {
			return out, true
		}
	}

	for _, s := range sections {
		if s.kind != "sso-session" {
			continue
		}

		out := awsConfigImport{StartURL: s.keys["sso_start_url"], SSORegion: s.keys["sso_region"]}
		if out.valid() {
			return out, true
		}
	}

	return awsConfigImport{}, false
}

// awsProfileName is the profile a config section names: `[default]` or `[profile NAME]`.
func awsProfileName(s awsINISection) (string, bool) {
	switch {
	case s.kind == "default" && s.name == "":
		return "default", true
	case s.kind == "profile" && s.name != "":
		return s.name, true
	}

	return "", false
}

// valid is an https start URL and an Identity Center region; the device flow checks the rest.
func (c awsConfigImport) valid() bool {
	u, err := url.Parse(c.StartURL)

	return err == nil && u.Scheme == "https" && u.Host != "" && c.SSORegion != ""
}

// values is the sign-in and discovery fields the apps prefill, without the empty ones. The
// profile's region is left out: as awsRegions it would narrow discovery to that one region
// (empty reads the Identity Center region and the common ones), and awsRegion belongs to
// the access-keys set, while the apps pick the option whose fields the values name.
func (c awsConfigImport) values() map[string]string {
	out := map[string]string{}

	for k, v := range map[string]string{
		awsFieldSSOStartURL: c.StartURL, awsFieldSSORegion: c.SSORegion,
		awsFieldAccountID: c.AccountID, awsFieldRoleName: c.RoleName,
	} {
		if v != "" {
			out[k] = v
		}
	}

	return out
}

// hasAWSAccessKeys tells an ~/.aws/credentials file (or a config holding keys): the apps
// refuse it rather than route secrets through the classifier.
func hasAWSAccessKeys(text string) bool {
	sections, ok := readAWSINI(text)
	if !ok {
		return false
	}

	for _, s := range sections {
		if s.keys["aws_access_key_id"] != "" {
			return true
		}
	}

	return false
}
