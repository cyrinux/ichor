package gitforge

import (
	"net/url"
	"regexp"
	"strings"
)

// RepoWebURL is the https page of a repository, "" when it cannot be browsed: an OCI
// registry, a local path, or something that is not a URL. SSH forms become https on the same
// host; credentials and ports are dropped.
func RepoWebURL(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" || strings.HasPrefix(repo, "oci://") {
		return ""
	}

	var host, path string

	switch {
	case strings.Contains(repo, "://"):
		u, err := url.Parse(repo)
		if err != nil || u.Hostname() == "" {
			return ""
		}

		switch u.Scheme {
		case "https", "http", "ssh", "git", "git+ssh", "ssh+git":
		default:
			return ""
		}

		host, path = u.Hostname(), u.Path
	default:
		m := scpLikeURL.FindStringSubmatch(repo)
		if m == nil {
			return ""
		}

		host, path = m[1], "/"+m[2]
	}

	path = strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
	if path == "" || path == "/" {
		return ""
	}

	return "https://" + host + path
}

// scpLikeURL is "git@host:org/repo.git": SSH without a scheme.
var scpLikeURL = regexp.MustCompile(`^(?:[\w.-]+@)?([\w.-]+):([^/\s].*)$`)
