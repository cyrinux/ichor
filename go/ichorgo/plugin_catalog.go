package ichorgo

import (
	"errors"
	"slices"

	"github.com/cyrinux/ichor/go/plugin"
)

// The CRD plugin catalog for the apps: what each built-in plugin reads and can do, and the
// validation of a plugin file a user brings (the import screen).

type pluginCatalog struct {
	Items []pluginCatalogItem `json:"items"`
}

type pluginCatalogItem struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Icon      string                  `json:"icon"`
	Website   string                  `json:"website"`
	Category  string                  `json:"category"`
	Screen    string                  `json:"screen"`
	Version   string                  `json:"version"`
	Resources []pluginCatalogResource `json:"resources"`
	Actions   []pluginCatalogAction   `json:"actions"`
	// User is an imported plugin (PluginUserImport), not a built-in.
	User bool `json:"user"`
}

type pluginCatalogResource struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
	// Reveal shows a Secret's data instead of masking it: the consent sheet flags it.
	Reveal bool `json:"reveal"`
}

type pluginCatalogAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Verb  string `json:"verb"`
	// RBAC are the Kubernetes verbs the action needs on RBACResource; Confirm is how the app
	// confirms it (none, plain, typedName).
	RBAC         []string `json:"rbac"`
	RBACResource string   `json:"rbacResource"`
	Confirm      string   `json:"confirm"`
}

// PluginCatalog lists the installed plugins, the built-ins then the enabled user plugins:
// {"items":[{id,name,icon,website,category,screen,version,resources:[…],actions:[…],user}]}.
func PluginCatalog() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(pluginCatalogOf(plugin.Builtins(), enabledUserPlugins()))
}

func pluginCatalogOf(builtins, user []*plugin.Compiled) pluginCatalog {
	out := pluginCatalog{Items: make([]pluginCatalogItem, 0, len(builtins)+len(user))}

	for _, c := range builtins {
		out.Items = append(out.Items, pluginCatalogItemOf(c))
	}

	for _, c := range user {
		item := pluginCatalogItemOf(c)
		item.User = true
		out.Items = append(out.Items, item)
	}

	return out
}

func pluginCatalogItemOf(c *plugin.Compiled) pluginCatalogItem {
	p := c.Plugin
	item := pluginCatalogItem{
		ID: p.ID, Name: p.Name, Icon: p.Icon, Website: p.Website,
		Category: orDefault(p.Category, "data-services"), Screen: p.Screen, Version: p.SchemaVersion,
		Resources: []pluginCatalogResource{}, Actions: []pluginCatalogAction{},
	}

	for _, key := range c.Resources {
		r := p.Resources[key]
		item.Resources = append(item.Resources, pluginCatalogResource{Group: r.Group, Version: r.Version, Resource: r.Resource, Kind: r.Kind, Reveal: r.Reveal})
	}

	for _, a := range c.Actions {
		action := pluginCatalogAction{ID: a.Name, Label: a.Label, Verb: a.Verb, RBAC: []string{}, Confirm: orDefault(a.Confirm, "plain")}
		if a.RBAC != nil {
			action.RBAC = slices.Clone(a.RBAC.Verbs)
			action.RBACResource = a.RBAC.Resource
		}

		item.Actions = append(item.Actions, action)
	}

	return item
}

type pluginValidation struct {
	OK       bool             `json:"ok"`
	ID       string           `json:"id"`
	Problems []plugin.Problem `json:"problems"`
}

// PluginValidate checks a plugin file: {"ok":bool,"id":"…","problems":[{path,line,msg,severity}]}.
// ok is false when any problem is an error; warnings do not stop a plugin. Hostile input
// (an alias bomb, deep nesting, more than 1 MiB) is a problem, never a crash: maskResult
// turns a panic into an error.
func PluginValidate(yaml string) (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(validatePlugin(yaml))
}

func validatePlugin(yaml string) pluginValidation {
	p, err := plugin.Load("plugin.yaml", []byte(yaml))

	var le *plugin.LoadError
	if errors.As(err, &le) {
		return pluginValidation{Problems: le.Problems}
	}

	if err != nil {
		return pluginValidation{Problems: []plugin.Problem{{Msg: err.Error(), Severity: plugin.SeverityError}}}
	}

	problems := plugin.Validate(p)
	if problems == nil {
		problems = []plugin.Problem{}
	}

	return pluginValidation{OK: !plugin.HasErrors(problems), ID: p.ID, Problems: problems}
}
