package ichorgo

import (
	"strings"

	"github.com/cyrinux/ichor/go/kube"
)

// Why a pod's image cannot be pulled, from the kubelet's waiting message: the kind of
// problem and the facts its fix needs (the registry, the pod's pull secrets). The apps word
// the kinds; the message itself is shown as is.

// The kinds of pull problem.
const (
	pullNotFound     = "notFound"
	pullUnauthorized = "unauthorized"
	pullRateLimited  = "rateLimited"
	pullTLS          = "tls"
	pullNetwork      = "network"
	pullPlatform     = "platform"
	pullInvalidName  = "invalidName"
	pullUnknown      = "unknown"
)

type pullProblem = kube.PullProblem

// pullPatterns are the stable parts of containerd's and the registries' messages, by kind,
// in the order they are tried (lower case). A wrong platform ends in "not found" and a rate
// limit carries a digest (where a bare "401" can hide): both are tried first.
var pullPatterns = []struct {
	kind     string
	contains []string
}{
	{pullPlatform, []string{"no match for platform", "no matching manifest"}},
	{pullRateLimited, []string{"toomanyrequests", "429 too many requests", "rate limit"}},
	{pullNotFound, []string{"not found", "manifest unknown", "name_unknown", "manifest_unknown", "repository does not exist"}},
	{pullUnauthorized, []string{"unauthorized", "authentication required", "pull access denied", "insufficient_scope", "denied", "403 forbidden"}},
	{pullTLS, []string{"x509", "certificate", "tls:"}},
	{pullNetwork, []string{"dial tcp", "i/o timeout", "no such host", "connection refused", "context deadline exceeded", "network is unreachable"}},
}

// isPullReason tells whether a waiting reason is a failed pull.
func isPullReason(reason string) bool {
	switch reason {
	case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
		return true
	}

	return false
}

// explainPull classifies a failed pull of image; nil when reason is not a pull failure.
func explainPull(reason, message, image string, secrets []string) *pullProblem {
	if !isPullReason(reason) {
		return nil
	}

	p := &pullProblem{Kind: pullUnknown, Image: image, Registry: imageRegistry(image), Secrets: secrets}
	if p.Secrets == nil {
		p.Secrets = []string{}
	}

	if reason == "InvalidImageName" {
		p.Kind = pullInvalidName

		return p
	}

	lower := strings.ToLower(message)

	for _, pat := range pullPatterns {
		for _, s := range pat.contains {
			if strings.Contains(lower, s) {
				p.Kind = pat.kind

				return p
			}
		}
	}

	return p
}

// imageRegistry is the host an image is pulled from, docker.io when the reference names
// none (as containerd reads it): the first path part when it has a dot or a port, or is
// localhost.
func imageRegistry(image string) string {
	first, _, hasPath := strings.Cut(image, "/")
	if hasPath && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}

	return "docker.io"
}
