package ichorgo

import (
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// The Omni sign-in fields a scanned file prefills: the instance, then the account email or,
// for a service account identity, the key (never in the file: it only picks that mode).
const (
	discoverOmni               = "omni"
	omniFieldURL               = "omniUrl"
	omniFieldEmail             = "omniEmail"
	omniFieldServiceAccountKey = "serviceAccountKey"
)

// omniFileContext is a context of an omnictl config (url) or of an Omni talosconfig (endpoints).
type omniFileContext struct {
	URL       string   `yaml:"url"`
	Endpoints []string `yaml:"endpoints"`
	Cluster   string   `yaml:"cluster"`
	Auth      struct {
		SideroV1 *struct {
			Identity string `yaml:"identity"`
		} `yaml:"siderov1"`
	} `yaml:"auth"`
}

// omniSignInFile reads an omnictl config, or a talosconfig of a whole Omni account (siderov1
// contexts without a cluster), as the Omni sign-in it names. A talosconfig of one Omni cluster
// is not one: it imports as is.
func omniSignInFile(text string) (field string, values map[string]string, ok bool) {
	var file struct {
		Context  string                     `yaml:"context"`
		Contexts map[string]omniFileContext `yaml:"contexts"`
	}
	if yaml.Unmarshal([]byte(text), &file) != nil || len(file.Contexts) == 0 {
		return "", nil, false
	}

	ctx, found := file.Contexts[file.Context]
	if !found {
		ctx = file.Contexts[slices.Sorted(maps.Keys(file.Contexts))[0]]
	}

	raw := ctx.URL
	if raw == "" {
		if ctx.Auth.SideroV1 == nil || ctx.Cluster != "" || len(ctx.Endpoints) == 0 {
			return "", nil, false
		}

		raw = ctx.Endpoints[0]
	}

	instance, err := omniInstanceURL(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	if err != nil {
		return "", nil, false
	}

	values = map[string]string{omniFieldURL: instance}
	field = omniFieldEmail

	if ctx.Auth.SideroV1 != nil {
		switch identity := strings.TrimSpace(ctx.Auth.SideroV1.Identity); {
		case strings.HasSuffix(identity, serviceAccountDomain):
			field = omniFieldServiceAccountKey
		case identity != "":
			values[omniFieldEmail] = identity
		}
	}

	return field, values, true
}
