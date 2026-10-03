package ichorgo

import (
	"strings"
	"testing"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

const mergeStored = `context: prod
contexts:
  prod:
    endpoints: [10.0.0.1, 10.0.0.2]
    nodes: [10.0.0.1]
    ca: b2xkLWNh
    crt: b2xkLWNydA==
    key: b2xkLWtleQ==
  lab:
    endpoints: [10.1.0.1]
    ca: bGFiLWNh
    crt: bGFiLWNydA==
    key: bGFiLWtleQ==
`

const mergeGenerated = `context: prod
contexts:
  prod:
    endpoints: [10.9.9.9]
    ca: bmV3LWNh
    crt: bmV3LWNydA==
    key: bmV3LWtleQ==
`

func TestReplaceContextCredentials(t *testing.T) {
	out, err := ReplaceContextCredentials(mergeStored, mergeGenerated, "prod")
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := clientconfig.FromString(out)
	if err != nil {
		t.Fatal(err)
	}

	prod := cfg.Contexts["prod"]
	if prod.CA != "bmV3LWNh" || prod.Crt != "bmV3LWNydA==" || prod.Key != "bmV3LWtleQ==" {
		t.Fatalf("credentials not replaced: %+v", prod)
	}

	if strings.Join(prod.Endpoints, ",") != "10.0.0.1,10.0.0.2" || strings.Join(prod.Nodes, ",") != "10.0.0.1" {
		t.Fatalf("endpoints/nodes changed: %+v", prod)
	}

	if lab := cfg.Contexts["lab"]; lab == nil || lab.Crt != "bGFiLWNydA==" {
		t.Fatalf("other context changed: %+v", lab)
	}

	if cfg.Context != "prod" {
		t.Fatalf("current context changed: %q", cfg.Context)
	}

	// The input must not be mutated through shared pointers.
	if !strings.Contains(mergeStored, "b2xkLWNydA==") {
		t.Fatal("input changed")
	}
}

func TestReplaceContextCredentialsErrors(t *testing.T) {
	if _, err := ReplaceContextCredentials(mergeStored, mergeGenerated, "missing"); err == nil {
		t.Fatal("missing context accepted")
	}

	if _, err := ReplaceContextCredentials(mergeStored, mergeStored, "prod"); err == nil {
		t.Fatal("multi-context generated config accepted")
	}

	if _, err := ReplaceContextCredentials("not: [yaml", mergeGenerated, "prod"); err == nil {
		t.Fatal("bad stored config accepted")
	}

	noCreds := "context: prod\ncontexts:\n  prod:\n    endpoints: [10.9.9.9]\n"
	if _, err := ReplaceContextCredentials(mergeStored, noCreds, "prod"); err == nil {
		t.Fatal("generated config without credentials accepted")
	}
}
