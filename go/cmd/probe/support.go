package main

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cyrinux/ichor/go/ichorgo"
)

type supportDone struct {
	path string
	size int64
	err  string
}

type supportPrinter struct {
	steps int
	done  chan supportDone
}

func (p *supportPrinter) OnProgress(string) { p.steps++ }

func (p *supportPrinter) OnDone(path string, size int64, errMessage string) {
	p.done <- supportDone{path, size, errMessage}
}

// supportProbe collects a support bundle (read-only on the cluster), prints only what kind
// of files it holds and deletes it: the bundle is not masked, so neither its content nor
// its node directory names are shown.
func supportProbe(cfg, contextName, nodes string) string {
	dir, err := os.MkdirTemp("", "support-probe")
	if err != nil {
		return err.Error()
	}

	defer os.RemoveAll(dir) //nolint:errcheck

	dest := filepath.Join(dir, "support.zip")
	p := &supportPrinter{done: make(chan supportDone, 1)}

	ichorgo.StartSupportBundle(cfg, contextName, "", nodes, dest, p)

	res := <-p.done
	if res.err != "" {
		return "failed: " + res.err
	}

	zr, err := zip.OpenReader(res.path)
	if err != nil {
		return err.Error()
	}

	defer zr.Close() //nolint:errcheck

	// Per kind of file (path without the node directory; one line per sub-directory).
	type kind struct {
		files int
		bytes uint64
	}

	kinds := map[string]*kind{}
	dirs := map[string]bool{}

	var (
		withKeys   []string
		redactions int
	)

	for _, f := range zr.File {
		top, rest, ok := strings.Cut(f.Name, "/")
		if !ok {
			top, rest = "", f.Name
		}

		if top != "" && top != "cluster" {
			dirs[top] = true
			top = "NODE"
		}

		if sub, _, nested := strings.Cut(rest, "/"); nested {
			rest = sub + "/*"
		}

		name := strings.TrimPrefix(top+"/"+rest, "/")
		if kinds[name] == nil {
			kinds[name] = &kind{}
		}

		kinds[name].files++
		kinds[name].bytes += f.UncompressedSize64

		// Secrets check: how many files hold private key material (PEM, or base64 PEM as in a
		// machine config), and whether the machine config was redacted.
		content := readZipFile(f)

		if hasPrivateKey(content) {
			withKeys = append(withKeys, name)
		}

		if strings.HasSuffix(rest, "machine-config.redacted.yaml") {
			redactions += strings.Count(content, "******")
		}
	}

	names := make([]string, 0, len(kinds))
	for name := range kinds {
		names = append(names, name)
	}

	sort.Strings(names)

	var b strings.Builder

	fmt.Fprintf(&b, "bundle: %d bytes, %d files, %d node directories, %d progress callbacks\n", res.size, len(zr.File), len(dirs), p.steps)

	for _, name := range names {
		fmt.Fprintf(&b, "  %-40s %4d file(s) %10d bytes\n", name, kinds[name].files, kinds[name].bytes)
	}

	fmt.Fprintf(&b, "files with private key material: %d %v; redacted values in machine configs: %d\n", len(withKeys), withKeys, redactions)

	if _, err := os.Stat(dest + ".part"); err == nil {
		b.WriteString("partial file left!\n")
	}

	return b.String()
}

func readZipFile(f *zip.File) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}

	defer rc.Close() //nolint:errcheck

	b, _ := io.ReadAll(rc) //nolint:errcheck

	return string(b)
}

// base64PEM matches base64-encoded PEM blocks ("-----BEGIN"), as in a machine config.
var base64PEM = regexp.MustCompile(`LS0tLS1CRUdJTi[A-Za-z0-9+/=]+`)

func hasPrivateKey(content string) bool {
	if strings.Contains(content, "PRIVATE KEY") {
		return true
	}

	for _, blob := range base64PEM.FindAllString(content, -1) {
		if decoded, err := base64.StdEncoding.DecodeString(blob); err == nil && strings.Contains(string(decoded), "PRIVATE KEY") {
			return true
		}
	}

	return false
}
