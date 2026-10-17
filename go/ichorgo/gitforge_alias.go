package ichorgo

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
)

// Git host aliases: Argo CD and Flux often reach a repository by an in-cluster or SSH name
// (ssh.git.example.org, gitea-http.gitea.svc:3000) other than the forge's API host. A
// repository on an alias is read through the API host: requests, and the token, only ever
// go to the host's baseURL, never to an alias.

// gitForgeMaxAliases bounds a host's other names.
const gitForgeMaxAliases = 16

// normalizeGitHostAlias is host[:port] in lower case, without scheme, user or path and with
// the host's trailing dot dropped; "" when nothing is left. A non-numeric "port" (the path of
// an scp-like git@host:owner/repo) is dropped.
func normalizeGitHostAlias(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}

	s, _, _ = strings.Cut(s, "/")
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}

	host, port := s, ""
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
		if !isPortNumber(port) {
			port = ""
		}
	}

	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if host == "" || strings.ContainsAny(host, " \t/@") {
		return ""
	}

	if port != "" {
		return net.JoinHostPort(host, port)
	}

	return host
}

func isPortNumber(s string) bool {
	return s != "" && len(s) <= 5 && strings.Trim(s, "0123456789") == ""
}

// normalizeGitHostAliases cleans, deduplicates and caps a host's other names.
func normalizeGitHostAliases(raw []string) []string {
	out := []string{}

	for _, a := range raw {
		if a = normalizeGitHostAlias(a); a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}

		if len(out) == gitForgeMaxAliases {
			break
		}
	}

	return out
}

// gitRepoAddr is the host[:port] a repository URL names, normalized as an alias is, and
// whether it is scp-like (git@host:owner/repo, which has no port).
func gitRepoAddr(repoURL string) (addr string, scp bool) {
	repoURL = strings.TrimSpace(repoURL)
	if strings.Contains(repoURL, "://") {
		u, err := url.Parse(repoURL)
		if err != nil {
			return "", false
		}

		return normalizeGitHostAlias(u.Host), false
	}

	m := scpLikeURL.FindStringSubmatch(repoURL)
	if m == nil {
		return "", false
	}

	return normalizeGitHostAlias(m[1]), true
}

// checkGitRepoHost refuses a repository that is not on h: its host must be h's (or a
// well-known name for it) or one of h's aliases, host[:port] exactly, or host only for an
// scp-like address.
func (h gitForgeHost) checkGitRepoHost(repo gitRepoRef, repoURL string) error {
	if repo.host == h.host {
		return nil
	}

	addr, scp := gitRepoAddr(repoURL)
	for _, a := range h.aliases {
		if a == addr || scp && aliasHostOnly(a) == addr {
			return nil
		}
	}

	if addr == "" {
		addr = repo.host
	}

	return fmt.Errorf("%s is on %s, not on the Git host %s: if it is the same server, add %s to the host's other names in Settings → Git hosts",
		repo.full(), addr, h.host, addr)
}

// aliasHostOnly is an alias without its port.
func aliasHostOnly(alias string) string {
	if h, _, err := net.SplitHostPort(alias); err == nil {
		return h
	}

	return alias
}
