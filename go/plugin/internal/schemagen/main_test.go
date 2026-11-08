package main

import (
	"bytes"
	"os"
	"testing"
)

// The committed schema is what schema.go generates: run go generate ./plugin after
// changing the format.
func TestSchemaIsGenerated(t *testing.T) {
	want, err := generate("../..")
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile("../../schema/plugin.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatal("go/plugin/schema/plugin.schema.json is stale: run `cd go && go generate ./plugin`")
	}
}
