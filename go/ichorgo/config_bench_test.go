package ichorgo

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// benchConfig is a talosconfig of n generated contexts (each with its own PEMs, ~1.3 KB).
func benchConfig(tb testing.TB, n int) string {
	var b strings.Builder

	b.WriteString("context: lab-0\ncontexts:\n")

	for i := range n {
		ca, crt, key := testIdentity(tb, time.Now().Add(time.Hour))
		fmt.Fprintf(&b, "  lab-%d:\n    endpoints:\n      - 10.0.%d.1\n    nodes:\n      - 10.0.%d.2\n    ca: %s\n    crt: %s\n    key: %s\n", i, i%250, i%250, ca, crt, key)
	}

	return b.String()
}

// benchCopies are copies of text: gomobile hands each call a new string with the same content.
func benchCopies(text string) []string {
	out := make([]string, 8)
	for i := range out {
		out[i] = strings.Clone(text)
	}

	return out
}

// perCallConfigWork is what an exported data call (ClusterOverview) does with the config
// before any network: the privacy hook, the demo gate, the session lookup and open, the
// fingerprints.
func perCallConfigWork(configYAML, contextName string) {
	_ = unmaskContext(configYAML, contextName)
	_ = isDemoContext(configYAML, contextName)
	_, _, _ = resolveContext(configYAML, contextName)
	_ = cacheKey(configYAML, contextName)
	_, _, _ = resolveContext(configYAML, contextName)
	_ = contextFingerprints(configYAML)
}

func BenchmarkPerCallOverhead(b *testing.B) {
	for _, n := range []int{1, 25, 100} {
		b.Run(fmt.Sprintf("contexts=%d", n), func(b *testing.B) {
			copies := benchCopies(benchConfig(b, n))

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				perCallConfigWork(copies[i%len(copies)], "lab-0")
			}
		})
	}
}

func BenchmarkParseConfig(b *testing.B) {
	for _, n := range []int{1, 25, 100} {
		b.Run(fmt.Sprintf("contexts=%d", n), func(b *testing.B) {
			copies := benchCopies(benchConfig(b, n))

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if _, err := ParseConfig(copies[i%len(copies)]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
