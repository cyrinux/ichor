package main

import "testing"

func TestParseConfig(t *testing.T) {
	t.Setenv("RELAY_LISTEN", "")
	t.Setenv("RELAY_SECRET", "")
	t.Setenv("RELAY_KUBE_API", "")
	t.Setenv("RELAY_NAMESPACE", "monitoring")

	c, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c != (config{listen: ":8080", namespace: "monitoring", secret: "ichor-relay"}) {
		t.Errorf("defaults: %+v", c)
	}

	c, err = parseConfig([]string{"-listen", ":9000", "-secret", "push", "-kube-api", "http://127.0.0.1:8001"})
	if err != nil {
		t.Fatal(err)
	}
	if c != (config{listen: ":9000", namespace: "monitoring", secret: "push", kubeAPI: "http://127.0.0.1:8001"}) {
		t.Errorf("flags: %+v", c)
	}

	if _, err := parseConfig([]string{"-nope"}); err == nil {
		t.Error("an unknown flag parsed")
	}
}
