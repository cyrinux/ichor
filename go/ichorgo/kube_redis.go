package ichorgo

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A Redis client just big enough to read Argo CD's cache through a port-forward: RESP2
// commands (AUTH, SCAN, GET) and their replies. Nothing is written to Redis.

// respClient speaks RESP over one stream.
type respClient struct {
	rw       io.ReadWriter
	r        *bufio.Reader
	maxValue int
}

func newRespClient(rw io.ReadWriter, maxValue int) *respClient {
	return &respClient{rw: rw, r: bufio.NewReaderSize(rw, 64<<10), maxValue: maxValue}
}

// errRespNil is a nil bulk reply (a key that does not exist).
var errRespNil = errors.New("nil")

// call sends one command and returns its reply: string for simple strings and bulks,
// int64 for integers, []any for arrays, errRespNil for a nil bulk, a Redis error as an error.
func (c *respClient) call(args ...string) (any, error) {
	var buf strings.Builder

	fmt.Fprintf(&buf, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&buf, "$%d\r\n%s\r\n", len(a), a)
	}

	if _, err := io.WriteString(c.rw, buf.String()); err != nil {
		return nil, err
	}

	return c.reply()
}

func (c *respClient) reply() (any, error) {
	line, err := c.line()
	if err != nil {
		return nil, err
	}

	if line == "" {
		return nil, errors.New("empty reply")
	}

	payload := line[1:]

	switch line[0] {
	case '+':
		return payload, nil
	case '-':
		return nil, errors.New(payload)
	case ':':
		n, err := strconv.ParseInt(payload, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad integer reply %q", payload)
		}

		return n, nil
	case '$':
		n, err := strconv.Atoi(payload)
		if err != nil {
			return nil, fmt.Errorf("bad bulk length %q", payload)
		}

		if n < 0 {
			return nil, errRespNil
		}

		if n > c.maxValue {
			return nil, fmt.Errorf("value of %d bytes is larger than the app reads (%d)", n, c.maxValue)
		}

		data := make([]byte, n+2)
		if _, err := io.ReadFull(c.r, data); err != nil {
			return nil, err
		}

		return string(data[:n]), nil
	case '*':
		n, err := strconv.Atoi(payload)
		if err != nil {
			return nil, fmt.Errorf("bad array length %q", payload)
		}

		if n < 0 {
			return nil, errRespNil
		}

		out := make([]any, 0, n)

		for range n {
			item, err := c.reply()
			if err != nil && !errors.Is(err, errRespNil) {
				return nil, err
			}

			out = append(out, item)
		}

		return out, nil
	default:
		return nil, fmt.Errorf("unexpected reply %q: not a Redis server (TLS on Redis is not supported)", clipUTF8(line, 40))
	}
}

func (c *respClient) line() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

// auth sends the password when there is one; a Redis without a password accepts the
// connection as it is.
func (c *respClient) auth(password string) error {
	if password == "" {
		return nil
	}

	_, err := c.call("AUTH", password)
	if err != nil && strings.Contains(err.Error(), "without any password") {
		return nil
	}

	return err
}

// findValue is the value of the one key starting with prefix (SCAN, then GET), nil when
// there is none.
func (c *respClient) findValue(prefix string) ([]byte, error) {
	cursor := "0"

	for {
		reply, err := c.call("SCAN", cursor, "MATCH", respGlobEscape(prefix)+"*", "COUNT", strconv.Itoa(argoRedisScanCount))
		if err != nil {
			return nil, err
		}

		page, ok := reply.([]any)
		if !ok || len(page) != 2 {
			return nil, errors.New("unexpected SCAN reply")
		}

		next, _ := page[0].(string)
		keys, _ := page[1].([]any)

		for _, k := range keys {
			key, _ := k.(string)
			if !strings.HasPrefix(key, prefix) {
				continue
			}

			value, err := c.call("GET", key)
			if errors.Is(err, errRespNil) {
				continue // expired between the two commands
			}

			if err != nil {
				return nil, err
			}

			s, _ := value.(string)

			return []byte(s), nil
		}

		if next == "0" || next == "" {
			return nil, nil
		}

		cursor = next
	}
}

// respGlobEscape escapes the glob characters of a MATCH pattern so a name is matched literally.
func respGlobEscape(s string) string {
	var out strings.Builder

	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			out.WriteByte('\\')
		}

		out.WriteRune(r)
	}

	return out.String()
}
