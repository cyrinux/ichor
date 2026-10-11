package ichorgo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/meta"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// The META partition: a few boot-time values Talos reads before the machine config, such as
// the network config of a metal node (0xa). Writing one is the last-resort fix of a node
// that cannot boot with its config, like `talosctl meta write`.

// metaValueLimit is the largest value written: the META partition is a few hundred KiB.
const metaValueLimit = 256 << 10

// metaKeyNames are the keys Talos defines (pkg/machinery/meta), by tag.
var metaKeyNames = map[int]string{
	meta.Upgrade:                     "upgrade",
	meta.StagedUpgradeImageRef:       "staged upgrade image",
	meta.StagedUpgradeInstallOptions: "staged upgrade options",
	meta.StateEncryptionConfig:       "STATE encryption",
	meta.MetalNetworkPlatformConfig:  "metal network config",
	meta.DownloadURLCode:             "config download code",
	meta.UserReserved1:               "user reserved 1",
	meta.UserReserved2:               "user reserved 2",
	meta.UserReserved3:               "user reserved 3",
	meta.UUIDOverride:                "UUID override",
	meta.UniqueMachineToken:          "unique machine token",
	meta.DiskImageBootloader:         "disk image bootloader",
}

// metaSecretKeys hold secrets: their values are never shown.
var metaSecretKeys = []int{meta.StateEncryptionConfig, meta.UniqueMachineToken}

type metaKey struct {
	Key  int    `json:"key"`
	Hex  string `json:"hex"`
	Name string `json:"name"` // "" for a key Talos does not define
	// Value is the text, the bytes in hex when Binary, "" when Secret.
	Value  string `json:"value"`
	Size   int    `json:"size"`
	Binary bool   `json:"binary"`
	Secret bool   `json:"secret"`
	// Writable: the API accepts a write or delete of this key (meta.IsAPIWriteable).
	Writable bool `json:"writable"`
}

type metaKeyList struct {
	Keys []metaKey `json:"keys"`
	// Loaded: the node has read its META partition (false early in boot, or without one).
	Loaded bool `json:"loaded"`
	// Known are the keys Talos defines, for the "add a key" picker.
	Known []metaKey `json:"known"`
}

// NodeMetaKeys lists the keys of node's META partition (os:reader; the secret ones without
// their value): {"keys":[{key,hex,name,value,size,binary,secret,writable}],"loaded","known"}.
func NodeMetaKeys(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeMetaKeys", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		keys, err := safe.StateListAll[*runtime.MetaKey](ctx, s.client.COSI)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		list := metaKeyList{Keys: []metaKey{}, Known: knownMetaKeys()}

		for k := range keys.All() {
			tag, err := strconv.ParseUint(strings.TrimPrefix(k.Metadata().ID(), "0x"), 16, 8)
			if err != nil {
				continue
			}

			list.Keys = append(list.Keys, newMetaKey(int(tag), k.TypedSpec().Value))
		}

		loaded, err := safe.StateGetByID[*runtime.MetaLoaded](ctx, s.client.COSI, runtime.MetaLoadedID)
		list.Loaded = err == nil && loaded.TypedSpec().Done

		slices.SortFunc(list.Keys, func(a, b metaKey) int { return a.Key - b.Key })

		return toJSON(list)
	})
}

// NodeMetaWrite sets key of node's META partition to value, like `talosctl meta write`
// (os:admin). Talos reads it at the next boot. The activity log has the key and the size,
// never the value.
func NodeMetaWrite(configYAML, contextName, node string, key int, value string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	defer recordAction(&err, configYAML, contextName,
		auditAction{Action: "meta-write", Node: node, Object: "META/" + metaHex(key), Params: fmt.Sprintf("size=%d", len(value))})

	if err := checkMetaKey(key); err != nil {
		return err
	}

	switch {
	case value == "":
		return errors.New("no value given: delete the key instead")
	case len(value) > metaValueLimit:
		return fmt.Errorf("the value is %d bytes: at most %d fit", len(value), metaValueLimit)
	}

	return nodeAction(configYAML, contextName, node, callTimeout, func(ctx context.Context, c *client.Client) error {
		return c.MetaWrite(ctx, uint8(key), []byte(value))
	})
}

// NodeMetaDelete removes key from node's META partition, like `talosctl meta delete`
// (os:admin).
func NodeMetaDelete(configYAML, contextName, node string, key int) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "meta-delete", Node: node, Object: "META/" + metaHex(key)})

	if err := checkMetaKey(key); err != nil {
		return err
	}

	return nodeAction(configYAML, contextName, node, callTimeout, func(ctx context.Context, c *client.Client) error {
		return c.MetaDelete(ctx, uint8(key))
	})
}

// checkMetaKey refuses what Talos would: a tag out of a byte, or a key only Talos writes.
func checkMetaKey(key int) error {
	if key < 0 || key > 255 {
		return fmt.Errorf("META keys go from 0x00 to 0xff, not %d", key)
	}

	if !meta.IsAPIWriteable(uint8(key)) {
		return fmt.Errorf("%s (%s) is written by Talos itself, not through the API", metaHex(key), metaName(key))
	}

	return nil
}

func newMetaKey(key int, value string) metaKey {
	k := metaKey{Key: key, Hex: metaHex(key), Name: metaKeyNames[key], Size: len(value), Writable: checkMetaKey(key) == nil}

	switch {
	case slices.Contains(metaSecretKeys, key):
		k.Secret = true
	case !printableText(value):
		k.Binary, k.Value = true, hex.EncodeToString([]byte(value))
	default:
		k.Value = value
	}

	return k
}

func knownMetaKeys() []metaKey {
	known := []metaKey{}

	for key, name := range metaKeyNames {
		known = append(known, metaKey{Key: key, Hex: metaHex(key), Name: name, Writable: checkMetaKey(key) == nil})
	}

	slices.SortFunc(known, func(a, b metaKey) int { return a.Key - b.Key })

	return known
}

func metaHex(key int) string { return fmt.Sprintf("0x%02x", key) }

func metaName(key int) string {
	if name, ok := metaKeyNames[key]; ok {
		return name
	}

	return "an unknown key"
}

// printableText: valid UTF-8 without control characters other than newlines and tabs.
func printableText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}

	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}

	return true
}

// demoMetaKeys are a demo node's META keys: a metal network config and a user value.
func demoMetaKeys() metaKeyList {
	return metaKeyList{
		Loaded: true,
		Known:  knownMetaKeys(),
		Keys: []metaKey{
			newMetaKey(meta.MetalNetworkPlatformConfig, "addresses:\n  - address: 192.0.2.21/24\n    linkName: eth0\n"),
			newMetaKey(meta.UserReserved1, "rack-3"),
			newMetaKey(meta.UniqueMachineToken, "demo-token"),
		},
	}
}
