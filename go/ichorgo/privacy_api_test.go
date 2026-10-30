package ichorgo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// Exported functions whose result is a credential or file the user saves (a new node's config,
// its cloud user data): only their errors are masked. The shells' terminal stream (debug, pod and VM console) is not masked at all, nor its snippets
// (fixed commands whose well-known addresses masking would break), nor the GitHub links of
// integration requests (opened, not shown; built from already masked results), nor share
// links (built from the masked names on screen), nor the alert rules' decisions (the app's own
// fingerprints and keys, matched back), nor a config's digest (a hash, which masking
// a word could corrupt), nor the name of a backup file format (a fixed word).
var unmaskedResults = []string{"Kubeconfig", "GenerateTalosconfig", "JoinNodeConfigSealed", "NodeCloudUserData", "BuildTalosconfig", "ReplaceContextCredentials", "MergeConfig", "RemoveContext", "MergeKubeconfig", "RemoveKubeContext", "ExportKubeContext", "MergeTalosconfig", "DecodeImportText", "PluginFetchURL", "PluginUserExport", "PluginQRPayload", "RunbookStoreGet", "QRCodeText", "QRPart", "QRPartsJoin", "KubeAuthForBackup", "DiscoverClusters", "AddContextNodes", "SetContextEndpoints", "AddContextEndpoint", "DemoConfig", "DemoKubeconfig", "DecryptBackup", "BackupFormatOf", "NormalizeKubeServer", "NormalizePromSource", "DebugSnippets", "IntegrationIssueURL", "IntegrationSearchURL", "PartsLookupLink", "BuildShareLink", "SearchSnapshots", "SearchScreens", "DemoSearchEntries", "FleetSummary", "DemoFleetSummary", "AlertRulesApply", "ServiceAccountTemplates", "DiscoverOmniClusters", "SetOmniServiceAccount", "ConfigDigest"}

// The shells (and the VM console) wrap their listener in newDebugSession: status and exit
// masked, not the terminal.
var unmaskedListeners = []string{"StartDebugShell", "StartNodeDebug", "StartPodDebug", "StartPodShell", "StartVMConsole"}

// privacyPrologues are internal helpers that unmask the context and mask the result for the
// exported function delegating to them: each is checked like an exported function.
var privacyPrologues = []string{"kubeNamedMutation", "readClusterNodes"}

// TestExportedFuncsUsePrivacyHooks checks every exported function of the package: masked
// context names and nodes coming from the app are unmasked on entry, and results, errors
// and listener callbacks are masked on exit. A new exported function fails here until it
// goes through the hooks.
func TestExportedFuncsUsePrivacyHooks(t *testing.T) {
	fset := token.NewFileSet()

	//lint:ignore SA1019 the package has no build-tagged files, so ParseDir sees all of it
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}

				if !fn.Name.IsExported() && !slices.Contains(privacyPrologues, fn.Name.Name) {
					continue
				}

				if fn.Name.Name == "SetPrivacyMask" {
					continue
				}

				checked++

				for _, problem := range privacyHookProblems(fn) {
					t.Errorf("%s: %s", fn.Name.Name, problem)
				}
			}
		}
	}

	if checked < 30 {
		t.Fatalf("only %d exported functions found", checked)
	}
}

func privacyHookProblems(fn *ast.FuncDecl) []string {
	name := fn.Name.Name
	params := paramNames(fn)
	assigned := assignedFrom(fn.Body)
	deferred := deferredCalls(fn.Body)

	var problems []string

	// Delegating to a privacy prologue unmasks the context and masks the result.
	delegates := slices.ContainsFunc(calledNames(fn.Body), func(c string) bool { return slices.Contains(privacyPrologues, c) })

	require := func(param string, helpers ...string) {
		if delegates && param == "contextName" {
			return
		}

		if slices.Contains(params, param) && !slices.Contains(helpers, assigned[param]) {
			problems = append(problems, param+" must be reassigned from "+strings.Join(helpers, " or "))
		}
	}

	require("contextName", "unmaskContext", "unmaskTarget", "unmaskTargets")
	require("node", "unmaskTarget")
	require("nodes", "unmaskTargets")

	if slices.Contains(params, "listener") && !slices.Contains(unmaskedListeners, name) &&
		!strings.HasPrefix(assigned["listener"], "masked") {
		problems = append(problems, "listener must be wrapped in a masked*Listener")
	}

	results := resultTypes(fn)

	switch {
	case delegates:
	case slices.Contains(results, "string") && !slices.Contains(unmaskedResults, name):
		if !slices.Contains(deferred, "maskResult") {
			problems = append(problems, "missing defer maskResult(&out, &err)")
		}
	case slices.Contains(results, "error"):
		if !slices.Contains(deferred, "maskResult") && !slices.Contains(deferred, "maskErr") {
			problems = append(problems, "missing defer maskErr(&err)")
		}
	}

	return problems
}

func paramNames(fn *ast.FuncDecl) []string {
	var names []string

	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
	}

	return names
}

func resultTypes(fn *ast.FuncDecl) []string {
	var types []string

	if fn.Type.Results == nil {
		return nil
	}

	for _, field := range fn.Type.Results.List {
		if id, ok := field.Type.(*ast.Ident); ok {
			types = append(types, id.Name)
		}
	}

	return types
}

// assignedFrom maps each variable assigned at the top level of body to the function (or
// composite type) it is assigned from.
func assignedFrom(body *ast.BlockStmt) map[string]string {
	out := map[string]string{}

	for _, stmt := range body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Rhs) != 1 {
			continue
		}

		var source string

		switch rhs := as.Rhs[0].(type) {
		case *ast.CallExpr:
			source = exprName(rhs.Fun)
		case *ast.CompositeLit:
			source = exprName(rhs.Type)
		}

		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				out[id.Name] = source
			}
		}
	}

	return out
}

func deferredCalls(body *ast.BlockStmt) []string {
	var out []string

	for _, stmt := range body.List {
		if d, ok := stmt.(*ast.DeferStmt); ok {
			out = append(out, exprName(d.Call.Fun))
		}
	}

	return out
}

func exprName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	default:
		return ""
	}
}
