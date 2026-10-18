package ichorgo

import (
	"cmp"
	"slices"

	"github.com/cyrinux/ichor/go/plugin"
)

// pluginSpecs are the Settings rows of plugins: the groups they detect by, for the list and the
// integration requests, and the plugin itself, whose own detect entries tell whether the
// cluster runs it (pluginProbe.detection), as its tile does.
func pluginSpecs(compiled []*plugin.Compiled) []integrationSpec {
	out := make([]integrationSpec, 0, len(compiled))

	for _, c := range compiled {
		p := c.Plugin
		s := integrationSpec{ID: cmp.Or(p.CatalogID, p.ID), Name: p.Name, Website: p.Website, fromPlugin: c}

		for _, d := range p.Detect {
			if d.Group != "" && !slices.Contains(s.Groups, d.Group) {
				s.Groups = append(s.Groups, d.Group)
			}
		}

		out = append(out, s)
	}

	return out
}
