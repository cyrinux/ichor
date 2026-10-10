package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// The classic upgrade trap: a node runs a system extension the target version has no build
// of, and comes back without it. UpgradeExtensionCheck compares the node's extensions with
// the Image Factory's official list for the version.

const factoryTimeout = 5 * time.Second

// factoryBaseURL is the Image Factory the official extensions are read from (tests replace it).
var factoryBaseURL = "https://" + factoryHost

// factoryExtensions caches the official extension names per version for the session.
var factoryExtensions = struct {
	sync.Mutex
	byVersion map[string][]string
}{byVersion: map[string][]string{}}

// extensionCheck is what UpgradeExtensionCheck returns.
type extensionCheck struct {
	Schematic     string             `json:"schematic"`
	TargetVersion string             `json:"targetVersion"`
	Installed     []installedExtName `json:"installed"`
	// Missing: installed extensions the version has no official build of.
	Missing []string `json:"missing"`
	// Unknown: the image is not from the public Image Factory, or the factory could not be
	// read (Error says why): nothing can be said.
	Unknown bool   `json:"unknown"`
	Error   string `json:"error"`
}

type installedExtName struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// UpgradeExtensionCheck tells which of node's system extensions have no official build for
// image's version on the Image Factory (os:reader; the factory is asked without credentials,
// 5 s timeout, once per version per session). See extensionCheck for the JSON; an image not
// from factory.talos.dev (a custom registry or a private factory) is "unknown", never a
// false "missing".
func UpgradeExtensionCheck(configYAML, contextName, node, image string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("UpgradeExtensionCheck", configYAML, contextName, node, image)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		installed, err := nodeExtensions(ctx, s, node)
		if err != nil {
			return "", err
		}

		return toJSON(checkExtensions(installed, image, officialExtensions))
	})
}

// nodeExtensions reads node's system extensions; ctx targets node.
func nodeExtensions(ctx context.Context, s *session, node string) ([]extensionInfo, error) {
	exts, err := safe.StateListAll[*runtime.ExtensionStatus](client.WithNode(ctx, node), s.client.COSI)
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	return mapExtensions(safe.ToSlice(exts, identity)), nil
}

// checkExtensions compares installed with official(version of image).
func checkExtensions(installed []extensionInfo, image string, official func(version string) ([]string, error)) extensionCheck {
	repo, tag := splitImageRef(image)
	version, _ := normalizeTalosVersion(tag)

	out := extensionCheck{Schematic: imageSchematic(image), TargetVersion: version, Installed: []installedExtName{}, Missing: []string{}}

	for _, e := range installed {
		// Talos lists the factory schematic it booted from as an extension: not one to check.
		if e.Name != "schematic" {
			out.Installed = append(out.Installed, installedExtName{Name: e.Name, Version: e.Version})
		}
	}

	if version == "" || !strings.HasPrefix(repo, factoryHost+"/") {
		out.Unknown = true

		return out
	}

	if len(out.Installed) == 0 {
		return out
	}

	names, err := official(version)
	if err != nil {
		out.Unknown, out.Error = true, err.Error()

		return out
	}

	known := map[string]bool{}
	for _, n := range names {
		known[path.Base(n)] = true
	}

	for _, e := range out.Installed {
		if !known[path.Base(e.Name)] {
			out.Missing = append(out.Missing, e.Name)
		}
	}

	return out
}

// officialExtensions lists the official extension names of version on the Image Factory,
// cached for the session.
func officialExtensions(version string) ([]string, error) {
	factoryExtensions.Lock()
	cached, ok := factoryExtensions.byVersion[version]
	factoryExtensions.Unlock()

	if ok {
		return cached, nil
	}

	names, err := fetchOfficialExtensions(version)
	if err != nil {
		return nil, err
	}

	factoryExtensions.Lock()
	factoryExtensions.byVersion[version] = names
	factoryExtensions.Unlock()

	return names, nil
}

func fetchOfficialExtensions(version string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), factoryTimeout)
	defer cancel()

	u := factoryBaseURL + "/version/" + url.PathEscape(version) + "/extensions/official"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := newHTTPClient(httpClientOpts{followRedirects: true}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not check extensions: %s", friendlyError(err))
	}

	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not check extensions: the Image Factory answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("could not check extensions: %w", err)
	}

	var list []struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("could not check extensions: %w", err)
	}

	names := make([]string, 0, len(list))
	for _, e := range list {
		names = append(names, e.Name)
	}

	return slices.Compact(slices.Sorted(slices.Values(names))), nil
}

// extensionWarnings are the plan warnings of a check: one per missing extension, or why it
// could not be done.
func extensionWarnings(c extensionCheck) []string {
	var out []string

	for _, name := range c.Missing {
		out = append(out, fmt.Sprintf("extension %s has no build for %s: the node would come back without it (pick a schematic with it on factory.talos.dev)", name, c.TargetVersion))
	}

	if c.Error != "" {
		out = append(out, c.Error)
	}

	return out
}

// demoTag is the tag of image, the demo's own version when it has none.
func demoTag(image string) string {
	if _, tag := splitImageRef(image); tag != "" {
		return tag
	}

	return demoTalosVersion
}
