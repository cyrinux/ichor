package ichorgo

import "go.yaml.in/yaml/v4"

// ClassifyImportText returns routing metadata only, never the imported credentials.
// The caller decodes compressed import text first. Classification is not validation:
// discovery or the config preview still checks the supplied credentials.
func ClassifyImportText(text string) (out string, err error) {
	defer maskResult(&out, &err)

	route := struct {
		Kind     string            `json:"kind"`
		Provider string            `json:"provider,omitempty"`
		Field    string            `json:"field,omitempty"`
		Values   map[string]string `json:"values,omitempty"` // only for secret-free files
	}{Kind: "unknown"}
	switch gcpCredentialType(text) {
	case gcpTypeAuthorizedUser, gcpTypeWorkforceUser:
		route.Kind, route.Provider, route.Field = "credentials", discoverGKE, gcpFieldUserCredentials
	case gcpTypeLoginConfig:
		route.Kind, route.Provider, route.Field = "credentials", discoverGKE, gcpFieldLoginConfig
	case "service_account":
		route.Kind, route.Provider, route.Field = "credentials", discoverGKE, gcpFieldServiceAccount
	default:
		if aws, ok := parseAWSConfig(text); ok {
			route.Kind, route.Provider, route.Field, route.Values = "credentials", discoverEKS, awsFieldSSOStartURL, aws.values()
		} else if hasAWSAccessKeys(text) {
			// Access keys are typed on the EKS discovery screen, never routed from a scan.
			route.Kind, route.Provider, route.Field = "refused", discoverEKS, awsFieldAccessKey
		} else if IsKubeconfig(text) {
			route.Kind = "kubeconfig"
		} else if field, values, ok := omniSignInFile(text); ok {
			// An omnictl config or a whole account's talosconfig: Omni lists its clusters.
			route.Kind, route.Provider, route.Field, route.Values = "credentials", discoverOmni, field, values
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
