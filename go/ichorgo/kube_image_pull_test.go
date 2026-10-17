package ichorgo

import (
	"encoding/json"
	"testing"
)

func TestExplainPull(t *testing.T) {
	cases := []struct {
		name, reason, message, image, kind string
	}{
		{"manifest unknown", "ErrImagePull", `failed to pull and unpack image "ghcr.io/example-org/app:9": failed to resolve reference "ghcr.io/example-org/app:9": ghcr.io/example-org/app:9: not found`, "ghcr.io/example-org/app:9", pullNotFound},
		{"name unknown", "ImagePullBackOff", `rpc error: code = NotFound desc = failed to pull and unpack image: MANIFEST_UNKNOWN: manifest unknown`, "registry.example.test/team/api:1", pullNotFound},
		{"unauthorized", "ErrImagePull", `failed to authorize: failed to fetch anonymous token: unexpected status from GET request to https://ghcr.io/token: 401 Unauthorized`, "ghcr.io/example-org/private:1", pullUnauthorized},
		{"pull access denied", "ImagePullBackOff", `pull access denied, repository does not exist or may require authorization: server message: insufficient_scope: authorization failed`, "example/private:2", pullNotFound},
		{"rate limited", "ImagePullBackOff", demoPullMessage, demoPullImage, pullRateLimited},
		{"tls", "ErrImagePull", `failed to do request: Head "https://registry.example.test/v2/app/manifests/1": tls: failed to verify certificate: x509: certificate signed by unknown authority`, "registry.example.test/app:1", pullTLS},
		{"dns", "ErrImagePull", `failed to do request: Head "https://registry.example.test/v2/app/manifests/1": dial tcp: lookup registry.example.test on 10.96.0.10:53: no such host`, "registry.example.test/app:1", pullNetwork},
		{"timeout", "ImagePullBackOff", `failed to do request: Head "https://quay.io/v2/x/manifests/1": dial tcp 192.0.2.4:443: i/o timeout`, "quay.io/x:1", pullNetwork},
		{"platform", "ErrImagePull", `failed to pull and unpack image "docker.io/library/app:1": no match for platform in manifest: not found`, "app:1", pullPlatform},
		{"platform alone", "ErrImagePull", `no matching manifest for linux/arm64 in the manifest list entries`, "app:1", pullPlatform},
		{"invalid name", "InvalidImageName", `Failed to apply default image tag "Bad:Name": couldn't parse image reference`, "Bad:Name", pullInvalidName},
		// A digest holding "401" stays a rate limit.
		{"rate limited, digest", "ErrImagePull", `failed to copy: httpReadSeeker: failed open: unexpected status code https://registry-1.docker.io/v2/library/app/manifests/sha256:9f40140a4011: 429 Too Many Requests - Server message: toomanyrequests: You have reached your pull rate limit.`, "app:1", pullRateLimited},
		{"forbidden", "ErrImagePull", `failed to resolve reference "registry.example.test/team/app:1": unexpected status from HEAD request: 403 Forbidden`, "registry.example.test/team/app:1", pullUnauthorized},
		{"unknown", "ErrImagePull", `something new happened`, "app:1", pullUnknown},
	}

	for _, c := range cases {
		p := explainPull(c.reason, c.message, c.image, []string{"regcred"})
		if p == nil || p.Kind != c.kind || p.Image != c.image || p.Secrets[0] != "regcred" {
			t.Errorf("%s: %+v, want %s", c.name, p, c.kind)
		}
	}

	if p := explainPull("CrashLoopBackOff", "back-off restarting", "app:1", nil); p != nil {
		t.Errorf("not a pull: %+v", p)
	}

	if p := explainPull("ErrImagePull", "x", "app:1", nil); p == nil || p.Secrets == nil {
		t.Errorf("secrets must be a list: %+v", p)
	}
}

func TestImageRegistry(t *testing.T) {
	for image, want := range map[string]string{
		"nginx:1.25":                      "docker.io",
		"library/nginx":                   "docker.io",
		"example/app@sha256:abc":          "docker.io",
		"ghcr.io/example-org/app:1":       "ghcr.io",
		"localhost:5000/app":              "localhost:5000",
		"localhost/app":                   "localhost",
		"registry.example.test:8443/a/b":  "registry.example.test:8443",
		"registry.k8s.io/coredns/coredns": "registry.k8s.io",
	} {
		if got := imageRegistry(image); got != want {
			t.Errorf("%s: %q, want %q", image, got, want)
		}
	}
}

func TestMapPodWaitingAndPull(t *testing.T) {
	var obj podObject
	raw := `{"metadata":{"name":"api-1","namespace":"web"},
		"spec":{"initContainers":[{"name":"migrate","image":"ghcr.io/example-org/migrate:2"}],"containers":[{"name":"api","image":"ghcr.io/example-org/api:3"}],
			"imagePullSecrets":[{"name":"regcred"}]},
		"status":{"phase":"Pending",
			"initContainerStatuses":[{"name":"migrate","state":{"waiting":{"reason":"ImagePullBackOff","message":"Back-off pulling image \"ghcr.io/example-org/migrate:2\": 401 Unauthorized"}}}],
			"containerStatuses":[{"name":"api","state":{"waiting":{"reason":"PodInitializing"}}}]}}`

	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatal(err)
	}

	p := mapPod(obj)
	if p.Status != "Init:ImagePullBackOff" || p.Waiting == "" || p.Pull == nil {
		t.Fatalf("pod %+v", p)
	}

	if p.Pull.Kind != pullUnauthorized || p.Pull.Image != "ghcr.io/example-org/migrate:2" || p.Pull.Registry != "ghcr.io" || len(p.Pull.Secrets) != 1 {
		t.Errorf("pull %+v", p.Pull)
	}

	// A running pod has neither.
	var running podObject
	if err := json.Unmarshal([]byte(`{"spec":{"containers":[{"name":"a","image":"a:1"}]},"status":{"phase":"Running","containerStatuses":[{"name":"a","ready":true,"state":{"running":{}}}]}}`), &running); err != nil {
		t.Fatal(err)
	}

	if q := mapPod(running); q.Waiting != "" || q.Pull != nil {
		t.Errorf("running %+v", q)
	}
}
