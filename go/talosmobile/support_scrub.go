package talosmobile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Talos's RedactSecrets only knows the secrets of the machine config schema (CA keys,
// tokens, encryption secrets). A support bundle is meant to be shared, so its copy of the
// machine config also loses what users put secrets in: file contents, inline manifests,
// environment variables, registry credentials, manifest headers and any value whose key
// looks like a credential.

// secretKey matches keys whose value is replaced whatever it is.
var secretKey = regexp.MustCompile(`(?i)(password|passphrase|secret|token|private_?key|credential|^auth$|^key$)`)

// secretContainers are keys whose whole content is user-provided and may embed secrets:
// machine.files[].content, cluster.inlineManifests[].contents, machine.env,
// cluster.extraManifestHeaders, extension service environments, WireGuard private keys.
var secretContainers = map[string]bool{
	"content": true, "contents": true, "env": true, "environment": true, "extraManifestHeaders": true, "privateKey": true,
}

// scrubMachineConfig redacts user-provided secrets from an already RedactSecrets'ed machine
// config (possibly multi-document). A config that cannot be parsed is not returned at all.
func scrubMachineConfig(text string) (string, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))

	var out bytes.Buffer

	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)

	docs := 0

	for {
		var doc yaml.Node

		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", fmt.Errorf("machine config left out: it could not be parsed to redact it: %w", err)
		}

		scrubNode(&doc)

		if err := enc.Encode(&doc); err != nil {
			return "", fmt.Errorf("machine config left out: %w", err)
		}

		docs++
	}

	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("machine config left out: %w", err)
	}

	if docs == 0 {
		return "", errors.New("machine config is empty")
	}

	return out.String(), nil
}

func scrubNode(n *yaml.Node) {
	switch n.Kind { //nolint:exhaustive
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			scrubNode(c)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]

			if secretContainers[key.Value] || secretKey.MatchString(key.Value) {
				redactNode(value)

				continue
			}

			scrubNode(value)
		}
	}
}

// redactNode replaces a value by the redaction marker; empty values stay, so the bundle
// still shows that nothing was set.
func redactNode(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && (n.Value == "" || n.Tag == "!!null" || n.Tag == "!!bool") {
		return
	}

	if (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) && len(n.Content) == 0 {
		return
	}

	*n = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: redacted}
}
