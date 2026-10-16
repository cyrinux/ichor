package ichorgo

import (
	"errors"

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
}

type pluginCatalogResource struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
}

type pluginCatalogAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Verb  string `json:"verb"`
}

// PluginCatalog lists the built-in plugins:
// {"items":[{id,name,icon,website,category,screen,version,resources:[…],actions:[…]}]}.
func PluginCatalog() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(pluginCatalogOf(plugin.Builtins()))
}

func pluginCatalogOf(compiled []*plugin.Compiled) pluginCatalog {
	out := pluginCatalog{Items: make([]pluginCatalogItem, 0, len(compiled))}

	for _, c := range compiled {
		out.Items = append(out.Items, pluginCatalogItemOf(c))
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
		item.Resources = append(item.Resources, pluginCatalogResource{Group: r.Group, Version: r.Version, Resource: r.Resource, Kind: r.Kind})
	}

	for _, a := range c.Actions {
		item.Actions = append(item.Actions, pluginCatalogAction{ID: a.Name, Label: a.Label, Verb: a.Verb})
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
