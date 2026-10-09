package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// The Talos machine config JSON schema describes every field (type, allowed values,
// documentation). It is published per release next to the Talos sources and is what editors
// load from the "yaml-language-server: $schema=" comment of a config file. The machine config
// screen uses it to show and edit the config field by field.

const (
	configSchemaURL      = "https://raw.githubusercontent.com/siderolabs/talos/%s/pkg/machinery/config/schemas/config.schema.json"
	configSchemaDir      = "schemas"
	configSchemaTimeout  = 15 * time.Second
	configSchemaMaxBytes = 8 << 20
	configSchemaRetry    = 5 * time.Minute
	// configSchemaKept is how many versions stay on disk: a cluster runs a few at most.
	configSchemaKept = 6

	schemaSourceMemory  = "memory"
	schemaSourceDisk    = "disk"
	schemaSourceNetwork = "network"
)

// schemaVersionPattern is a Talos release tag. The version comes from the node and ends up
// in a URL and a file name, so nothing else is accepted.
var schemaVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// schemaNode is the part of a JSON schema node the config tree needs.
type schemaNode struct {
	Ref                  string                 `json:"$ref"`
	Type                 string                 `json:"type"`
	Title                string                 `json:"title"`
	Description          string                 `json:"description"`
	Enum                 []any                  `json:"enum"`
	Properties           map[string]*schemaNode `json:"properties"`
	PatternProperties    map[string]*schemaNode `json:"patternProperties"`
	AdditionalProperties json.RawMessage        `json:"additionalProperties"`
	Items                *schemaNode            `json:"items"`
	OneOf                []*schemaNode          `json:"oneOf"`
}

// configSchema is a parsed config.schema.json: one definition per config document, listed
// in OneOf.
type configSchema struct {
	Defs  map[string]*schemaNode `json:"$defs"`
	OneOf []*schemaNode          `json:"oneOf"`
}

type schemaFailure struct {
	at  time.Time
	err error
}

// schemaStore keeps one schema per Talos version: in memory, then in the data directory,
// and only then from the network, so a version is downloaded once.
type schemaStore struct {
	// fetching is held for a whole prepare, download included: two screens asking at once
	// still fetch only once. mu only guards the maps and the files, so describing a config
	// never waits for a download.
	fetching sync.Mutex
	mu       sync.Mutex
	loaded   map[string]*configSchema
	// failed remembers a failed download a while: offline, every opening of the screen
	// would wait for the timeout.
	failed map[string]schemaFailure

	dir    func() string
	url    func(version string) string
	client *http.Client
}

func newSchemaStore(dir func() string, url func(string) string, client *http.Client) *schemaStore {
	return &schemaStore{loaded: map[string]*configSchema{}, failed: map[string]schemaFailure{}, dir: dir, url: url, client: client}
}

var configSchemas = newSchemaStore(dataDir, func(version string) string { return fmt.Sprintf(configSchemaURL, version) }, newHTTPClient(httpClientOpts{followRedirects: true}))

type schemaStatus struct {
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Source    string `json:"source,omitempty"` // memory, disk, network
	Reason    string `json:"reason,omitempty"` // why it is not available
}

// MachineConfigSchemaPrepare makes the machine config schema of node's Talos version ready
// for MachineConfigDescribe: {"version","available","source","reason"}. The schema is
// downloaded from the Talos repository on GitHub the first time a version is seen (no
// authentication, 15 s timeout), then kept in the data directory (see SetDataDir) and in
// memory. A schema that cannot be had is not an error: available is false and the config is
// described without it.
func MachineConfigSchemaPrepare(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	version, err := configTalosVersion(configYAML, contextName, node)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), configSchemaTimeout)
	defer cancel()

	status := schemaStatus{Version: version}

	if _, source, err := configSchemas.prepare(ctx, version); err != nil {
		status.Reason = err.Error()
	} else {
		status.Available, status.Source = true, source
	}

	return toJSON(status)
}

// configTalosVersion is the Talos version tag node runs.
func configTalosVersion(configYAML, contextName, node string) (string, error) {
	if isTalosDemoContext(configYAML, contextName) {
		return demoTalosVersion, nil
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if v := s.nodeVersion(ctx, node); v != "" {
			return v, nil
		}

		return "", fmt.Errorf("node %s did not report its Talos version", node)
	})
}

// cached returns the schema of version without using the network, nil when it is not known.
func (st *schemaStore) cached(version string) *configSchema {
	if !schemaVersionPattern.MatchString(version) {
		return nil
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	schema, _ := st.local(version)

	return schema
}

// prepare returns the schema of version and where it came from, downloading it when it is
// neither in memory nor on disk.
func (st *schemaStore) prepare(ctx context.Context, version string) (*configSchema, string, error) {
	if !schemaVersionPattern.MatchString(version) {
		return nil, "", fmt.Errorf("no config schema for Talos version %q", version)
	}

	st.fetching.Lock()
	defer st.fetching.Unlock()

	if schema, source, err := st.known(version); schema != nil || err != nil {
		return schema, source, err
	}

	body, err := st.fetch(ctx, version)

	var schema *configSchema
	if err == nil {
		schema, err = parseConfigSchema(body)
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	if err != nil {
		st.failed[version] = schemaFailure{at: time.Now(), err: err}

		return nil, "", err
	}

	delete(st.failed, version)
	st.loaded[version] = schema
	st.save(version, body) // best effort: without it the next launch downloads again

	return schema, schemaSourceNetwork, nil
}

// known is what is already there for version: the schema, or the recent failure to get it.
func (st *schemaStore) known(version string) (*configSchema, string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if schema, source := st.local(version); schema != nil {
		return schema, source, nil
	}

	if f, ok := st.failed[version]; ok && time.Since(f.at) < configSchemaRetry {
		return nil, "", f.err
	}

	return nil, "", nil
}

// local looks in memory, then on disk. The caller holds mu.
func (st *schemaStore) local(version string) (*configSchema, string) {
	if schema, ok := st.loaded[version]; ok {
		return schema, schemaSourceMemory
	}

	path := st.path(version)
	if path == "" {
		return nil, ""
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	schema, err := parseConfigSchema(body)
	if err != nil {
		_ = os.Remove(path) //nolint:errcheck // unreadable: download it again

		return nil, ""
	}

	st.loaded[version] = schema

	return schema, schemaSourceDisk
}

func (st *schemaStore) path(version string) string {
	dir := st.dir()
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, configSchemaDir, "config.schema."+version+".json")
}

func (st *schemaStore) fetch(ctx context.Context, version string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, st.url(version), nil)
	if err != nil {
		return nil, err
	}

	resp, err := st.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch config schema: %s", friendlyError(err))
	}

	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch config schema %s: GitHub answered %s", version, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, configSchemaMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch config schema: %w", err)
	}

	if len(body) > configSchemaMaxBytes {
		return nil, errors.New("fetch config schema: the file is too large")
	}

	return body, nil
}

// save writes the downloaded schema atomically and drops the oldest ones beyond
// configSchemaKept.
func (st *schemaStore) save(version string, body []byte) {
	path := st.path(version)
	if path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) //nolint:errcheck

		return
	}

	pruneSchemas(filepath.Dir(path), configSchemaKept)
}

// pruneSchemas keeps the keep most recently written schema files of dir.
func pruneSchemas(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type file struct {
		name string
		at   time.Time
	}

	var files []file

	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		files = append(files, file{e.Name(), info.ModTime()})
	}

	slices.SortFunc(files, func(a, b file) int { return b.at.Compare(a.at) })

	for i := keep; i < len(files); i++ {
		_ = os.Remove(filepath.Join(dir, files[i].name)) //nolint:errcheck
	}
}

func parseConfigSchema(body []byte) (*configSchema, error) {
	var schema configSchema
	if err := json.Unmarshal(body, &schema); err != nil {
		return nil, fmt.Errorf("parse config schema: %w", err)
	}

	if len(schema.Defs) == 0 || len(schema.OneOf) == 0 {
		return nil, errors.New("parse config schema: not a Talos config schema")
	}

	return &schema, nil
}

// resolve follows n's $ref to its definition (nil when it leads nowhere). What the
// referring node says of the field itself (its title and documentation) wins over what the
// definition says of the type.
func (s *configSchema) resolve(n *schemaNode) *schemaNode {
	title, description := "", ""

	for depth := 0; n != nil && n.Ref != ""; depth++ {
		name, ok := strings.CutPrefix(n.Ref, "#/$defs/")
		if !ok || depth > 16 {
			return nil
		}

		if title == "" && description == "" {
			title, description = n.Title, n.Description
		}

		n = s.Defs[name]
	}

	if n == nil || title == "" && description == "" {
		return n
	}

	described := *n
	if title != "" {
		described.Title = title
	}

	if description != "" {
		described.Description = description
	}

	return &described
}

// property is the schema of key in the object obj describes: a declared property, else what
// the object allows for free keys (maps such as sysctls or nodeLabels).
func (s *configSchema) property(obj *schemaNode, key string) *schemaNode {
	if obj == nil {
		return nil
	}

	if p, ok := obj.Properties[key]; ok {
		return s.resolve(p)
	}

	return s.freeKey(obj)
}

// freeKey is the schema of the values an object takes under keys it does not declare, nil
// when it takes none (or anything).
func (s *configSchema) freeKey(obj *schemaNode) *schemaNode {
	if obj == nil {
		return nil
	}

	for _, p := range obj.PatternProperties {
		return s.resolve(p)
	}

	if len(obj.AdditionalProperties) > 0 && obj.AdditionalProperties[0] == '{' {
		var n schemaNode
		if json.Unmarshal(obj.AdditionalProperties, &n) == nil {
			return s.resolve(&n)
		}
	}

	return nil
}

// allowsFreeKeys tells whether obj takes keys it does not declare.
func (s *configSchema) allowsFreeKeys(obj *schemaNode) bool {
	if obj == nil {
		return true
	}

	return len(obj.PatternProperties) > 0 || len(obj.AdditionalProperties) > 0 && string(obj.AdditionalProperties) != "false"
}
