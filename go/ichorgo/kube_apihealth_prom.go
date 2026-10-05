package ichorgo

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// promSample is one line of a Prometheus text exposition: its labels and value.
type promSample struct {
	labels map[string]string
	value  float64
}

// promSamples maps a metric name to its samples.
type promSamples map[string][]promSample

// promMaxLine bounds one exposition line; a longer one (none in practice) fails the read.
const promMaxLine = 1 << 20

// parsePromText reads the samples of the metrics in keep from a Prometheus text
// exposition (the API server's /metrics), line by line: a large cluster's runs to tens of
// megabytes, mostly histogram buckets that are skipped. Comments, other metrics and lines
// that do not parse are skipped too: an unknown line must not lose the rest.
func parsePromText(r io.Reader, keep map[string]bool) (promSamples, error) {
	out := promSamples{}
	lines := bufio.NewScanner(r)
	lines.Buffer(make([]byte, 0, 64<<10), promMaxLine)

	for lines.Scan() {
		line := lines.Text()
		if line == "" || line[0] == '#' {
			continue
		}

		end := strings.IndexAny(line, "{ ")
		if end <= 0 || !keep[line[:end]] {
			continue
		}

		name, rest := line[:end], line[end:]

		labels, rest, ok := parsePromLabels(rest)
		if !ok {
			continue
		}

		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}

		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}

		out[name] = append(out[name], promSample{labels: labels, value: value})
	}

	return out, lines.Err()
}

// parsePromLabels reads a {key="value",...} block at the start of s, if any, and returns
// what follows it.
func parsePromLabels(s string) (map[string]string, string, bool) {
	labels := map[string]string{}
	if !strings.HasPrefix(s, "{") {
		return labels, s, true
	}

	s = s[1:]

	for {
		s = strings.TrimLeft(s, " ,")
		if strings.HasPrefix(s, "}") {
			return labels, s[1:], true
		}

		key, after, found := strings.Cut(s, "=")
		if !found || !strings.HasPrefix(after, `"`) {
			return nil, "", false
		}

		value, rest, ok := readPromQuoted(after[1:])
		if !ok {
			return nil, "", false
		}

		labels[strings.TrimSpace(key)] = value
		s = rest
	}
}

// readPromQuoted reads a label value up to its closing quote, undoing \\, \" and \n.
func readPromQuoted(s string) (string, string, bool) {
	var b strings.Builder

	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
			if i+1 == len(s) {
				return "", "", false
			}

			i++
			if s[i] == 'n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(c)
		}
	}

	return "", "", false
}

// promKey joins label values into a map key.
func promKey(values ...string) string { return strings.Join(values, "\x00") }

// promSplit undoes promKey.
func promSplit(key string) []string { return strings.Split(key, "\x00") }

// sumBy sums a metric's samples grouped by the values of keys, keeping those match accepts
// (nil for all).
func (s promSamples) sumBy(name string, match func(map[string]string) bool, keys ...string) map[string]float64 {
	out := map[string]float64{}

	for _, sample := range s[name] {
		if match != nil && !match(sample.labels) {
			continue
		}

		values := make([]string, len(keys))
		for i, k := range keys {
			values[i] = sample.labels[k]
		}

		out[promKey(values...)] += sample.value
	}

	return out
}

// total sums every sample of a metric that match accepts (nil for all).
func (s promSamples) total(name string, match func(map[string]string) bool) float64 {
	return s.sumBy(name, match)[""]
}

// has tells whether the exposition has the metric.
func (s promSamples) has(name string) bool { return len(s[name]) > 0 }
