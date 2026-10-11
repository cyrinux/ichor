package main

import (
	"slices"
	"testing"
)

func TestScannerArg(t *testing.T) {
	for _, c := range []struct {
		args     []string
		fallback string
		refs     []string
		scanner  string
	}{
		{[]string{"alpine:3.18.0"}, "", []string{"alpine:3.18.0"}, ""},
		{[]string{"alpine:3.18.0", "-scanner", "grype"}, "", []string{"alpine:3.18.0"}, "grype"},
		{[]string{"--scanner", "trivy", "a:1", "b:2"}, "grype", []string{"a:1", "b:2"}, "trivy"},
		{[]string{"a:1"}, "grype", []string{"a:1"}, "grype"},
		{[]string{"a:1", "-scanner"}, "", []string{"a:1", "-scanner"}, ""},
	} {
		refs, scanner := scannerArg(c.args, c.fallback)
		if !slices.Equal(refs, c.refs) || scanner != c.scanner {
			t.Errorf("scannerArg(%v, %q) = %v, %q", c.args, c.fallback, refs, scanner)
		}
	}
}
