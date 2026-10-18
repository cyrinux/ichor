package main

import (
	"errors"
	"flag"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// CRD plugins (go/plugin): the render tree a plugin draws on the cluster.
var pluginCommands = []command{
	// The config is read here, not by main: -demo needs none.
	{name: "plugin-render", args: "ID [-scope NS] [-locale xx] [-demo]", noConfig: true, run: func(e env) (string, error) {
		// plugin-render ID [-scope NS] [-locale xx] [-demo]: KubePluginData, read-only.
		return pluginRender(e, flag.Args()[1:])
	}},
}

func pluginRender(e env, args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("usage: plugin-render ID [-scope NS] [-locale xx] [-demo]")
	}

	fs := flag.NewFlagSet("plugin-render", flag.ContinueOnError)
	scope := fs.String("scope", "", "namespace to read (all when empty)")
	locale := fs.String("locale", "", "the plugin's translations to apply (fr, de…)")
	demo := fs.Bool("demo", false, "render the plugin's samples (the demo cluster), no config needed")

	if err := fs.Parse(args[1:]); err != nil {
		return "", err
	}

	cfg, contextName := "", e.context

	if *demo {
		var err error
		if cfg, err = ichorgo.DemoKubeconfig(); err != nil {
			return "", err
		}

		contextName = ""
	} else {
		cfg = loadProbeConfig(e.configPath, e.context, false, e.mask)
	}

	return ichorgo.KubePluginData(cfg, contextName, e.kubeServer, args[0], *scope, *locale)
}
