package main

import (
	"errors"
	"flag"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// CRD plugins (go/plugin): the render tree a plugin draws on the cluster.
var pluginCommands = []command{
	{name: "plugin-render", args: "ID [-scope NS] [-locale xx]", run: func(e env) (string, error) {
		// plugin-render ID [-scope NS] [-locale xx]: KubePluginData, read-only.
		return pluginRender(e, flag.Args()[1:])
	}},
}

func pluginRender(e env, args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("usage: plugin-render ID [-scope NS] [-locale xx]")
	}

	fs := flag.NewFlagSet("plugin-render", flag.ContinueOnError)
	scope := fs.String("scope", "", "namespace to read (all when empty)")
	locale := fs.String("locale", "", "the plugin's translations to apply (fr, de…)")

	if err := fs.Parse(args[1:]); err != nil {
		return "", err
	}

	return ichorgo.KubePluginData(e.cfg, e.context, e.kubeServer, args[0], *scope, *locale)
}
