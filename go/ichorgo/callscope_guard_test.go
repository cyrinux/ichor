package ichorgo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// screenBoundRuns are the Start* that do not hold their cluster's scope: their screen is gone
// when the app leaves the cluster, and the overview stream, almost always live, would make
// CancelCalls a permanent no-op.
var screenBoundRuns = []string{
	"StartClusterOverview", "StartKubeWatch", "StartKubeWorkloadPodsWatch", "StartKubeNodePodsWatch",
	"StartKubeChangeWatch", "StartKubeRolloutWatch", "StartKubeObjectWatch", "StartPodLogFollow",
	"StartLogFollow", "StartContainerLogFollow", "StartEvents", "StartKubeEvents", "StartHubbleFlows",
	"StartPortForward", "StartDebugShell", "StartNodeDebug", "StartPodDebug", "StartPodShell",
	"StartVMConsole", "StartClusterHealth",
}

// clusterlessRuns have no cluster to hold: they sign in to an account, before any cluster exists.
var clusterlessRuns = []string{"StartDiscoverSignIn", "StartOmniAccountSignIn"}

// delegatedRuns take their hold in the function they hand the run to.
var delegatedRuns = map[string]string{
	"StartEtcdSnapshot":          "startSnapshot",
	"StartEtcdSnapshotEncrypted": "startSnapshot",
	"StartHetznerCreate":         "cloudCreateRun.start",
	"StartOVHCreate":             "cloudCreateRun.start",
	"StartHetznerSnapshot":       "hetznerSnapshotRun.start",
	"StartNodeJoin":              "nodeJoin.start",
	"StartOmniAllocate":          "omniAllocate.start",
	"StartRunbook":               "startRunbook",
	"StartRunbookWith":           "startRunbook",
}

// TestCallScopeGuard checks every Start*: a new run fails here until it holds its cluster's
// scope (`defer holdCalls(…)()` in its goroutine) or is listed above.
func TestCallScopeGuard(t *testing.T) {
	fset := token.NewFileSet()

	//lint:ignore SA1019 the package has no build-tagged files, so ParseDir sees all of it
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	decls := map[string]*ast.FuncDecl{}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					decls[declName(fn)] = fn
				}
			}
		}
	}

	checkCallScopeLists(t, decls)

	runs := 0

	for name, fn := range decls {
		if !strings.HasPrefix(name, "Start") {
			continue
		}

		runs++

		if slicesHave(screenBoundRuns, name) || slicesHave(clusterlessRuns, name) {
			if holdsCalls(fn) {
				t.Errorf("%s holds the scope but is listed as not holding it", name)
			}

			continue
		}

		holder := fn
		if target, ok := delegatedRuns[name]; ok {
			holder = decls[target]
		}

		if !holdsCalls(holder) {
			t.Errorf("%s: add `defer holdCalls(configYAML, contextName)()` after its onPanic, or list it in screenBoundRuns", name)
		}
	}

	if runs < 30 {
		t.Fatalf("only %d runs found", runs)
	}
}

// checkCallScopeLists fails on a stale entry of the lists.
func checkCallScopeLists(t *testing.T, decls map[string]*ast.FuncDecl) {
	t.Helper()

	for _, list := range [][]string{screenBoundRuns, clusterlessRuns} {
		for _, name := range list {
			if decls[name] == nil {
				t.Errorf("stale entry: %s does not exist", name)
			}
		}
	}

	for name, target := range delegatedRuns {
		if decls[name] == nil || decls[target] == nil {
			t.Errorf("stale entry: %s -> %s", name, target)
		}
	}
}

func declName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}

	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}

	return exprName(recv) + "." + fn.Name.Name
}

func slicesHave(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}

	return false
}

// holdsCalls tells whether fn defers holdCalls(…)(), in its nested function literals too.
func holdsCalls(fn *ast.FuncDecl) bool {
	found := false

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}

		if call, ok := d.Call.Fun.(*ast.CallExpr); ok && exprName(call.Fun) == "holdCalls" {
			found = true
		}

		return true
	})

	return found
}

func TestCallScopeGuardDetectsAHold(t *testing.T) {
	parse := func(src string) *ast.FuncDecl {
		t.Helper()

		file, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+src, 0)
		if err != nil {
			t.Fatal(err)
		}

		return file.Decls[0].(*ast.FuncDecl)
	}

	if !holdsCalls(parse(`func StartX() { go func() { defer holdCalls(a, b)() }() }`)) {
		t.Error("a hold in the goroutine not seen")
	}

	if holdsCalls(parse(`func StartX() { go func() { defer onPanic(f)() }() }`)) {
		t.Error("another defer taken for a hold")
	}

	// Without the second call, the hold would be taken at the end and never released.
	if holdsCalls(parse(`func StartX() { defer holdCalls(a, b) }`)) {
		t.Error("a hold never released taken for one")
	}
}
