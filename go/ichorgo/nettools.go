package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
)

// NetToolListener receives a network tool's run (implemented in Kotlin/Swift).
type NetToolListener interface {
	// OnOutput is a progress or output line, as it comes.
	OnOutput(line string)
	// OnDone is called exactly once: the result JSON (netToolResult), or "" and why it failed.
	OnDone(resultJSON string, errMessage string)
}

// NetToolRun is a running network tool. Its methods are safe from any thread.
type NetToolRun struct {
	cancel context.CancelFunc
}

// Cancel stops the run; OnDone is still called.
func (r *NetToolRun) Cancel() { r.cancel() }

// The tools, by the name the apps pass.
const (
	netToolDNS   = "dns"
	netToolPing  = "ping"
	netToolPort  = "port"
	netToolTrace = "trace"
	netToolHTTP  = "http"
)

// netToolImage carries dig, ping, nc, mtr, tracepath, curl and openssl.
const netToolImage = "nicolaka/netshoot:latest"

const (
	netToolDefaultTimeout = 60 * time.Second
	netToolMaxTimeout     = 120 * time.Second
	netToolMinTimeout     = 5 * time.Second
	netToolCurlTimeout    = 10
	netToolDefaultCount   = 3
	netToolMaxCount       = 10
	netToolMaxOutput      = 256 << 10
)

// netToolRecords are the DNS record types asked for; A by default.
var netToolRecords = []string{"A", "AAAA", "CNAME", "MX", "NS", "PTR", "SRV", "TXT"}

// netToolOptions is the options JSON; every field is optional.
type netToolOptions struct {
	Record     string `json:"record"`
	Server     string `json:"server"`
	Count      int    `json:"count"`
	TimeoutSec int    `json:"timeoutSec"`
}

// netToolCommand is one validated run: the argv given to the container, never a shell line.
// The scripts below only ever read the target from "$@", so it is not parsed by the shell.
type netToolCommand struct {
	tool, target string
	argv         []string
	timeout      time.Duration
	// https: the HTTP check also reads the certificate.
	https bool
}

// netToolExec runs its arguments with stderr merged into stdout (the run reads stdout only).
const netToolExec = `exec "$@" 2>&1`

// netToolTraceScript prefers mtr, else tracepath (both in netshoot; mtr gives loss and latency).
const netToolTraceScript = `if command -v mtr >/dev/null 2>&1; then exec mtr -rwzc 10 -n "$1" 2>&1; fi; exec tracepath -n "$1" 2>&1`

// netToolHTTPScript: $1 URL, $2 curl timeout, $3 the TLS host:port and $4 its server name
// ("" for plain HTTP).
const netToolHTTPScript = `curl -sSIkL --max-time "$2" -o /dev/null ` +
	`-w 'ICHOR-HTTP %{http_code}|%{redirect_url}|%{ssl_verify_result}|%{time_total}\n' "$1" 2>&1
if [ -n "$3" ]; then
  echo | openssl s_client -connect "$3" -servername "$4" 2>/dev/null | openssl x509 -noout -subject -issuer -dates 2>&1
fi
exit 0`

// StartNodeNetTool runs a network check from node in a privileged netshoot container, like
// the debug shell (os:admin), and parses its output: tool is dns, ping, port, trace or http;
// target a host name, an IP, host:port (port) or a URL (http); options the netToolOptions
// JSON. A target that is not one of those is refused before anything runs.
func StartNodeNetTool(configYAML, contextName, node, tool, target, options string, listener NetToolListener) *NetToolRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)
	// A node's masked address typed back as the target (screenshot mode) is the real one.
	target = privacy.unmaskNode(strings.TrimSpace(target))

	listener = maskedNetToolListener{listener}

	cmd, err := netToolCommandFor(tool, target, options)

	ctx, cancel := context.WithTimeout(context.Background(), cmd.timeout+netToolPullAllowance)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })

		if err != nil {
			listener.OnDone("", err.Error())

			return
		}

		if isTalosDemoContext(configYAML, contextName) {
			out, err := demoRead("NodeNetTool", configYAML, contextName, node, cmd.tool, cmd.target)
			listener.OnDone(out, errText(err))

			return
		}

		result, err := runNetTool(ctx, configYAML, contextName, node, cmd, listener)
		if err != nil {
			listener.OnDone("", err.Error())

			return
		}

		out, err := toJSON(result)
		listener.OnDone(out, errText(err))
	}()

	return &NetToolRun{cancel: cancel}
}

// netToolPullAllowance is added to the tool's own bound: the first run pulls netshoot.
const netToolPullAllowance = 60 * time.Second

func runNetTool(ctx context.Context, configYAML, contextName, node string, cmd netToolCommand, listener NetToolListener) (netToolResult, error) {
	s, release, err := acquireSession(configYAML, contextName)
	if err != nil {
		return netToolResult{}, err
	}

	defer release()

	// A privileged container: never let a missing node fall through to the endpoint.
	if err := validatePowerTarget(s.context, node); err != nil {
		return netToolResult{}, err
	}

	nodeCtx := client.WithNode(ctx, node)

	listener.OnOutput("pulling image " + netToolImage + "…")

	imageName, err := pullImage(nodeCtx, s.client, netToolImage)
	if err != nil {
		return netToolResult{}, errors.New("pull failed: " + s.friendly(node, err))
	}

	runCtx, cancel := context.WithTimeout(ctx, cmd.timeout)
	defer cancel()

	raw, code, err := runDebugCommand(client.WithNode(runCtx, node), s.client.DebugClient, imageName, cmd.argv, listener.OnOutput)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return netToolResult{}, fmt.Errorf("%s did not finish within %s", cmd.tool, cmd.timeout)
		}

		if ctx.Err() != nil {
			return netToolResult{}, errors.New("cancelled")
		}

		return netToolResult{}, errors.New(s.friendly(node, err))
	}

	return parseNetTool(cmd, raw, code, time.Now())
}

// runDebugCommand runs argv in a privileged container without a TTY and returns its output
// (capped at netToolMaxOutput) and exit code; each complete line also goes to onLine.
func runDebugCommand(ctx context.Context, debug machineapi.DebugServiceClient, imageName string, argv []string, onLine func(string)) (string, int, error) {
	stream, err := debug.ContainerRun(ctx, grpc.MaxCallRecvMsgSize(4*1024*1024))
	if err != nil {
		return "", -1, err
	}

	err = stream.Send(&machineapi.DebugContainerRunRequest{
		Request: &machineapi.DebugContainerRunRequest_Spec{Spec: &machineapi.DebugContainerRunRequestSpec{
			Containerd: debugContainerd,
			ImageName:  imageName,
			Args:       argv,
			Profile:    machineapi.DebugContainerRunRequestSpec_PROFILE_PRIVILEGED,
		}},
	})
	if err != nil {
		return "", -1, err
	}

	var out, pending bytes.Buffer

	flush := func(all bool) {
		for {
			line, err := pending.ReadString('\n')
			if err != nil { // no newline: keep the partial line for later, unless at the end
				if all && line != "" {
					onLine(line)
				} else {
					pending.Reset()
					pending.WriteString(line)
				}

				return
			}

			onLine(strings.TrimRight(line, "\r\n"))
		}
	}

	for {
		msg, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			flush(true)

			return out.String(), -1, errors.New("the container ended without an exit code")
		case err != nil:
			return out.String(), -1, err
		}

		switch resp := msg.GetResp().(type) {
		case *machineapi.DebugContainerRunResponse_StdoutData:
			if out.Len() < netToolMaxOutput {
				out.Write(resp.StdoutData)
			}

			pending.Write(resp.StdoutData)
			flush(false)
		case *machineapi.DebugContainerRunResponse_ExitCode:
			flush(true)
			_ = stream.CloseSend()

			return out.String(), int(resp.ExitCode), nil
		}
	}
}

// netToolCommandFor validates tool, target and options and builds the argv. It never fails
// with a zero timeout, so the caller can bound a refused run too.
func netToolCommandFor(tool, target, options string) (netToolCommand, error) {
	cmd := netToolCommand{tool: tool, target: strings.TrimSpace(target), timeout: netToolDefaultTimeout}

	var opts netToolOptions
	if strings.TrimSpace(options) != "" {
		if err := json.Unmarshal([]byte(options), &opts); err != nil {
			return cmd, fmt.Errorf("invalid options: %w", err)
		}
	}

	if opts.TimeoutSec != 0 {
		cmd.timeout = min(max(time.Duration(opts.TimeoutSec)*time.Second, netToolMinTimeout), netToolMaxTimeout)
	}

	if err := checkNetToolTarget(cmd.target); err != nil {
		return cmd, err
	}

	var err error

	switch tool {
	case netToolDNS:
		cmd.argv, err = dnsArgv(cmd.target, opts)
	case netToolPing:
		cmd.argv, err = pingArgv(cmd.target, opts)
	case netToolPort:
		cmd.argv, err = portArgv(cmd.target)
	case netToolTrace:
		if !validHostOrIP(cmd.target) {
			return cmd, fmt.Errorf("%q is not a host name or an IP address", cmd.target)
		}

		cmd.argv = []string{"/bin/sh", "-c", netToolTraceScript, "sh", cmd.target}
	case netToolHTTP:
		cmd.argv, cmd.https, err = httpArgv(cmd.target, opts)
	default:
		return cmd, fmt.Errorf("unknown network tool %q", tool)
	}

	return cmd, err
}

// checkNetToolTarget refuses what no tool takes: shell metacharacters, spaces, a leading
// dash (an option to the tool), an empty or overlong target.
func checkNetToolTarget(target string) error {
	switch {
	case target == "":
		return errors.New("no target given")
	case len(target) > 2048:
		return errors.New("target too long")
	case strings.HasPrefix(target, "-"):
		return fmt.Errorf("invalid target %q", target)
	case strings.ContainsAny(target, " \t\r\n;|&$`<>(){}'\"\\*!"):
		return fmt.Errorf("invalid target %q: spaces and shell characters are not allowed", target)
	}

	for _, r := range target {
		if r < 0x21 || r > 0x7e {
			return fmt.Errorf("invalid target %q", target)
		}
	}

	return nil
}

// validHostOrIP: an IP address, or a DNS name (underscores allowed, for SRV names).
func validHostOrIP(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}

	name := strings.TrimSuffix(s, ".")
	if name == "" || strings.HasPrefix(name, ".") || strings.Contains(name, "..") {
		return false
	}

	return validHostname(strings.ReplaceAll(name, "_", "a"))
}

func dnsArgv(target string, opts netToolOptions) ([]string, error) {
	record := strings.ToUpper(strings.TrimSpace(opts.Record))
	if record == "" {
		record = "A"
	}

	if !slices.Contains(netToolRecords, record) {
		return nil, fmt.Errorf("unsupported record type %q", opts.Record)
	}

	if !validHostOrIP(target) {
		return nil, fmt.Errorf("%q is not a host name or an IP address", target)
	}

	argv := []string{"/bin/sh", "-c", netToolExec, "sh", "dig", "+noall", "+answer", "+comments", "+stats"}

	if server := strings.TrimSpace(opts.Server); server != "" {
		if net.ParseIP(server) == nil {
			return nil, fmt.Errorf("DNS server %q is not an IP address", server)
		}

		argv = append(argv, "@"+server)
	}

	// An address is looked up in reverse.
	if net.ParseIP(target) != nil {
		return append(argv, "-x", target), nil
	}

	return append(argv, target, record), nil
}

func pingArgv(target string, opts netToolOptions) ([]string, error) {
	if !validHostOrIP(target) {
		return nil, fmt.Errorf("%q is not a host name or an IP address", target)
	}

	count := opts.Count
	if count <= 0 {
		count = netToolDefaultCount
	}

	count = min(count, netToolMaxCount)

	return []string{"/bin/sh", "-c", netToolExec, "sh", "ping", "-c", strconv.Itoa(count), "-W", "2", target}, nil
}

func portArgv(target string) ([]string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("%q is not host:port", target)
	}

	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("invalid port %q", port)
	}

	if !validHostOrIP(host) {
		return nil, fmt.Errorf("%q is not a host name or an IP address", host)
	}

	return []string{"/bin/sh", "-c", netToolExec, "sh", "nc", "-zvw2", host, port}, nil
}

func httpArgv(target string, opts netToolOptions) ([]string, bool, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, false, fmt.Errorf("%q is not an http:// or https:// URL", target)
	}

	if u.User != nil {
		return nil, false, errors.New("URLs with credentials are not allowed")
	}

	host := u.Hostname()
	if !validHostOrIP(host) {
		return nil, false, fmt.Errorf("%q is not a host name or an IP address", host)
	}

	curlTimeout := netToolCurlTimeout
	if opts.TimeoutSec > 0 {
		curlTimeout = min(opts.TimeoutSec, int(netToolMaxTimeout/time.Second))
	}

	connect, serverName := "", ""

	if u.Scheme == "https" {
		port := u.Port()
		if port == "" {
			port = "443"
		}

		connect, serverName = net.JoinHostPort(host, port), host
	}

	return []string{"/bin/sh", "-c", netToolHTTPScript, "sh", u.String(), strconv.Itoa(curlTimeout), connect, serverName}, u.Scheme == "https", nil
}
