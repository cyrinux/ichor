package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
	"github.com/cyrinux/ichor/go/plugin"
)

// CRD plugins (go/plugin, docs/plugins.md "Command line"). plugin-validate and plugin-lint
// are the contract of a CRD-to-plugin converter: their exit codes say whether a draft passes,
// problems go to stderr as FILE:LINE: message, the result to stdout as one JSON line.
var pluginCommands = []command{
	{name: "plugin-validate", args: "FILE [FILE ...]", noConfig: true, run: func(e env) (string, error) {
		// plugin-validate FILE...: load, validate, compile and render the samples; exit 1 on any error.
		return noOutput, validatePluginFiles(flag.Args()[1:], os.Stdout, os.Stderr)
	}},
	{name: "plugin-lint", args: "FILE [-crd MANIFEST] [-strict]", noConfig: true, run: func(e env) (string, error) {
		// plugin-lint FILE [-crd MANIFEST] [-strict]: validate plus warnings; exit 1 on errors, or on warnings with -strict.
		return noOutput, lintPluginFile(flag.Args()[1:], os.Stdout, os.Stderr)
	}},
	// The config is read here, not by main: -demo needs none.
	{name: "plugin-render", args: "FILE|ID [-scope NS] [-locale xx] [-demo]", noConfig: true, run: func(e env) (string, error) {
		// plugin-render FILE|ID [-scope NS] [-locale xx] [-demo]: KubePluginData (or KubePluginPreview for a file), read-only.
		return pluginRender(e, flag.Args()[1:])
	}},
	{name: "plugin-list", noConfig: true, run: func(e env) (string, error) {
		// plugin-list: the built-in plugins, one JSON line each.
		return pluginList()
	}},
	{name: "plugin-action", args: "ID ACTION NAMESPACE NAME [PARAMS_JSON]", run: func(e env) (string, error) {
		// plugin-action ID ACTION NAMESPACE NAME [PARAMS_JSON]: KubePluginAction on a built-in plugin's
		// object; NAMESPACE "" for a cluster-scoped resource. It changes the cluster.
		if flag.NArg() < 5 || flag.NArg() > 6 {
			return "", errors.New("usage: plugin-action ID ACTION NAMESPACE NAME [PARAMS_JSON]")
		}

		if err := ichorgo.KubePluginAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5)); err != nil {
			return "", err
		}

		return "done", nil
	}},
}

// pluginReport is plugin-validate's line for a plugin that passes.
type pluginReport struct {
	OK        bool   `json:"ok"`
	ID        string `json:"id"`
	Cards     int    `json:"cards"`
	Findings  int    `json:"findings"`
	Resources int    `json:"resources"`
	Actions   int    `json:"actions"`
	// Warnings is set by plugin-lint.
	Warnings *int `json:"warnings,omitempty"`
}

// errProblems is returned once the problems are printed: main prints only this summary
// ("plugin-lint: 2 errors, 1 warning"), which has no FILE:LINE: prefix.
func errProblems(problems []plugin.Problem) error {
	var errs, warnings int

	for _, p := range problems {
		if p.Severity == plugin.SeverityError {
			errs++
		} else {
			warnings++
		}
	}

	return fmt.Errorf("plugin-lint: %s, %s", plural(errs, "error"), plural(warnings, "warning"))
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	return fmt.Sprintf("%d %ss", n, word)
}

// validatePluginFiles checks each file and prints its report to stdout, or its problems to
// stderr; the error says how many problems there were in all.
func validatePluginFiles(files []string, stdout, stderr io.Writer) error {
	if len(files) == 0 {
		return errors.New("usage: plugin-validate FILE [FILE ...]")
	}

	failed := 0

	for _, file := range files {
		_, c, problems := checkPluginFile(file)
		printProblems(stderr, file, problems)

		if plugin.HasErrors(problems) {
			failed++

			continue
		}

		if err := printJSON(stdout, reportOf(c)); err != nil {
			return err
		}
	}

	if failed > 0 {
		return fmt.Errorf("plugin-validate: %d of %d plugin files do not validate", failed, len(files))
	}

	return nil
}

// checkPluginFile loads, validates and compiles a plugin file, then renders its samples in
// English and in each language it translates to: a value that fails to evaluate on a sample
// is an error too, at the line of what it evaluates.
func checkPluginFile(file string) (*plugin.Plugin, *plugin.Compiled, []plugin.Problem) {
	src, err := os.ReadFile(argPath(file))
	if err != nil {
		return nil, nil, []plugin.Problem{{Msg: err.Error(), Severity: plugin.SeverityError}}
	}

	p, err := plugin.Load(file, src)

	var le *plugin.LoadError
	if errors.As(err, &le) {
		return nil, nil, le.Problems
	}

	if err != nil {
		return nil, nil, []plugin.Problem{{Msg: err.Error(), Severity: plugin.SeverityError}}
	}

	problems := plugin.Validate(p)
	if plugin.HasErrors(problems) {
		return p, nil, problems
	}

	c, err := plugin.Compile(p)
	if err != nil {
		return p, nil, append(problems, plugin.Problem{Msg: err.Error(), Severity: plugin.SeverityError})
	}

	return p, c, append(problems, renderProblems(c)...)
}

// renderProblems renders the samples once per locale and reports each failing value once.
func renderProblems(c *plugin.Compiled) []plugin.Problem {
	var out []plugin.Problem

	seen := map[string]bool{}
	now := time.Now()

	for _, locale := range append([]string{""}, sortedLocales(c.Plugin)...) {
		tree, _ := plugin.Render(c, plugin.SamplesInput(c, locale, nil, now), now) //nolint:errcheck // the same errors are in the tree

		for _, e := range tree.Errors {
			if seen[e.Where+e.Message] {
				continue
			}

			seen[e.Where+e.Message] = true
			out = append(out, plugin.Problem{
				Path: e.Where, Line: c.Plugin.Line(e.Where), Severity: plugin.SeverityError,
				Msg: "fails on the samples: " + e.Message,
			})
		}
	}

	return out
}

func sortedLocales(p *plugin.Plugin) []string { return slices.Sorted(maps.Keys(p.I18n)) }

// printProblems writes FILE:LINE: message per problem; a warning says so.
func printProblems(w io.Writer, file string, problems []plugin.Problem) {
	for _, p := range problems {
		msg := p.Msg
		if p.Path != "" {
			msg = p.Path + ": " + msg
		}

		if p.Severity == plugin.SeverityWarning {
			msg = "warning: " + msg
		}

		fmt.Fprintf(w, "%s:%d: %s\n", file, p.Line, msg)
	}
}

func reportOf(c *plugin.Compiled) pluginReport {
	p := c.Plugin

	return pluginReport{OK: true, ID: p.ID, Cards: len(p.Cards), Findings: len(p.Findings), Resources: len(p.Resources), Actions: len(p.Actions)}
}

func printJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, string(b))

	return err
}

// lintPluginFile validates the file, then prints Lint's warnings to stderr and the report,
// with the warning count, to stdout.
func lintPluginFile(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	crdPath := fs.String("crd", "", "the CRD manifest the plugin reads (kubectl get crd NAME -o yaml)")
	strict := fs.Bool("strict", false, "exit 1 on any warning")

	file, err := fileAndFlags(fs, args)
	if err != nil {
		return fmt.Errorf("%w\nusage: plugin-lint FILE [-crd MANIFEST] [-strict]", err)
	}

	var crd *plugin.CRDManifest

	if *crdPath != "" {
		src, err := os.ReadFile(argPath(*crdPath))
		if err != nil {
			return err
		}

		if crd, err = plugin.LoadCRD(src); err != nil {
			return fmt.Errorf("%s: %w", *crdPath, err)
		}
	}

	p, c, problems := checkPluginFile(file)
	if plugin.HasErrors(problems) {
		printProblems(stderr, file, problems)

		return errProblems(problems)
	}

	warnings := append(problems, plugin.Lint(p, crd)...)
	printProblems(stderr, file, warnings)

	report := reportOf(c)
	n := len(warnings)
	report.Warnings = &n

	if err := printJSON(stdout, report); err != nil {
		return err
	}

	if *strict && n > 0 {
		return fmt.Errorf("%w (-strict)", errProblems(warnings))
	}

	return nil
}

// fileAndFlags parses FILE and the flags, in either order.
func fileAndFlags(fs *flag.FlagSet, args []string) (string, error) {
	var file string

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		file, args = args[0], args[1:]
	}

	if err := fs.Parse(args); err != nil {
		return "", err
	}

	if file == "" && fs.NArg() > 0 {
		file = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return "", err
		}
	}

	switch {
	case file == "":
		return "", errors.New("a plugin file is needed")
	case fs.NArg() > 0:
		return "", fmt.Errorf("unexpected arguments %q", fs.Args())
	}

	return file, nil
}

// argPath resolves a relative path against ICHOR_PROBE_CWD, the directory `just probe` was
// called from (the recipe runs in go/), so that `just probe plugin-validate
// go/plugin/builtin/kube-green.yaml` works from the repository root. Problems still name
// the path as written.
func argPath(path string) string {
	if dir := os.Getenv("ICHOR_PROBE_CWD"); dir != "" && !filepath.IsAbs(path) {
		return filepath.Join(dir, path)
	}

	return path
}

// isPluginFile tells a plugin file from a built-in plugin id: an id has no dot or slash.
func isPluginFile(arg string) bool { return strings.ContainsAny(arg, "./"+string(os.PathSeparator)) }

func pluginRender(e env, args []string) (string, error) {
	fs := flag.NewFlagSet("plugin-render", flag.ContinueOnError)
	scope := fs.String("scope", "", "namespace to read (all when empty)")
	locale := fs.String("locale", "", "the plugin's translations to apply (fr, de…)")
	demo := fs.Bool("demo", false, "render the plugin's samples (the demo cluster), no config needed")

	target, err := fileAndFlags(fs, args)
	if err != nil {
		return "", fmt.Errorf("%w\nusage: plugin-render FILE|ID [-scope NS] [-locale xx] [-demo]", err)
	}

	// A file is validated before any config is read: its problems are FILE:LINE lines.
	var src []byte

	if isPluginFile(target) {
		if src, err = os.ReadFile(argPath(target)); err != nil {
			return "", err
		}

		if _, err := plugin.CompileSource(target, src); err != nil {
			return "", err
		}
	}

	cfg, contextName := "", e.context

	if *demo {
		if cfg, err = ichorgo.DemoKubeconfig(); err != nil {
			return "", err
		}

		contextName = ""
	} else {
		cfg = loadProbeConfig(e.configPath, e.context, false, e.mask)
	}

	if !isPluginFile(target) {
		return ichorgo.KubePluginData(cfg, contextName, e.kubeServer, target, *scope, *locale)
	}

	return ichorgo.KubePluginPreview(cfg, contextName, e.kubeServer, string(src), *scope, *locale)
}

// pluginListItem is one built-in plugin.
type pluginListItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Category  string `json:"category"`
	Resources int    `json:"resources"`
	Cards     int    `json:"cards"`
	Actions   int    `json:"actions"`
	Findings  int    `json:"findings"`
	File      string `json:"file"`
}

// pluginList prints the built-in plugins, one JSON line each, sorted by file.
func pluginList() (string, error) {
	var b strings.Builder

	for _, c := range plugin.Builtins() {
		p := c.Plugin
		category := p.Category
		if category == "" {
			category = "data-services"
		}

		item := pluginListItem{
			ID: p.ID, Name: p.Name, Version: p.SchemaVersion, Category: category,
			Resources: len(p.Resources), Cards: len(p.Cards), Actions: len(p.Actions), Findings: len(p.Findings),
			File: "go/plugin/" + p.FileName(),
		}

		if err := printJSON(&b, item); err != nil {
			return "", err
		}
	}

	return strings.TrimSuffix(b.String(), "\n"), nil
}
