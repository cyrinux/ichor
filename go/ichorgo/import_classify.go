package ichorgo

import "go.yaml.in/yaml/v4"

// ClassifyImportText returns routing metadata only, never the imported credentials.
// The caller decodes compressed import text first. Classification is not validation:
// discovery or the config preview still checks the supplied credentials.
func ClassifyImportText(text string) (out string, err error) {
	defer maskResult(&out, &err)

	route := struct {
		Kind     string `json:"kind"`
		Provider string `json:"provider,omitempty"`
		Field    string `json:"field,omitempty"`
	}{Kind: "unknown"}
	switch gcpCredentialType(text) {
	case gcpTypeAuthorizedUser, gcpTypeWorkforceUser:
		route.Kind, route.Provider, route.Field = "credentials", discoverGKE, gcpFieldUserCredentials
	case "service_account":
		route.Kind, route.Provider, route.Field = "credentials", discoverGKE, gcpFieldServiceAccount
	default:
		if IsKubeconfig(text) {
			route.Kind = "kubeconfig"
		} else {
			var shape struct {
				Contexts yaml.Node `yaml:"contexts"`
			}
			if yaml.Unmarshal([]byte(text), &shape) == nil && shape.Contexts.Kind == yaml.MappingNode {
				route.Kind = "talosconfig"
			}
		}
	}
	return toJSON(route)
}
