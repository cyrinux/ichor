package ichorgo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

// auditedEntryPoints are the exported functions that change a cluster: each records itself
// in the audit log. A function calling one of mutationPrimitives is found without being
// listed; the list also holds those that change a cluster another way (a background run,
// a Talos session, a pod exec).
var auditedEntryPoints = []string{
	"AlertmanagerExpire", "AlertmanagerSilence",
	"EtcdAlarmDisarm", "EtcdDefragment", "EtcdForfeitLeadership", "EtcdRemoveMember",
	"KubeArgoAction", "KubeArgoFreeze", "KubeCNPGBackup", "KubeCertManagerRenew", "KubeCordon",
	"KubeDeletePod", "KubeFluxAction", "KubeGarageRepairBlocks", "KubeGarageSetTranquility",
	"KubeHelmRollback", "KubeLonghornAction", "KubeNodeCordon", "KubeObjectUpdate",
	"KubeRollbackDeployment", "KubeRolloutRestart", "KubeScale", "KubeSuspendCronJob",
	"KubeTriggerCronJob", "NetPerfDeleteNamespace", "Reboot", "Rollback", "ServiceAction", "Shutdown", "StartClusterUpgrade", "StartConfigApply", "StartConfigTry",
	"StartEtcdNospaceFix", "StartK8sUpgrade", "StartKubeDrain", "StartNodeDebug", "StartNodeMaintenance", "StartNodeMaintenanceUpgrade", "StartPodDebug", "StartUpgrade",
}

// mutationPrimitives are the internal helpers only a change to a cluster goes through.
var mutationPrimitives = []string{"kubeMutate", "kubeMutationError", "nodeAction"}

// TestMutatingEntryPointsRecordThemselves checks every exported function that changes a
// cluster records an audit entry: a deferred recordAction after maskErr/maskResult (so it
// sees the unmasked error), or recordedRun in a background run. A new mutating function
// fails here until it does.
func TestMutatingEntryPointsRecordThemselves(t *testing.T) {
	fset := token.NewFileSet()

	//lint:ignore SA1019 the package has no build-tagged files, so ParseDir sees all of it
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	var found []string

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() {
					continue
				}

				calls := calledNames(fn.Body)
				mutates := slices.ContainsFunc(mutationPrimitives, func(p string) bool { return slices.Contains(calls, p) })

				if !mutates && !slices.Contains(auditedEntryPoints, fn.Name.Name) {
					continue
				}

				found = append(found, fn.Name.Name)

				if problem := auditProblem(fn, calls); problem != "" {
					t.Errorf("%s: %s", fn.Name.Name, problem)
				}
			}
		}
	}

	sort.Strings(found)

	for _, name := range auditedEntryPoints {
		if !slices.Contains(found, name) {
			t.Errorf("%s is listed in auditedEntryPoints but not found", name)
		}
	}

	if len(found) < len(auditedEntryPoints) {
		t.Fatalf("only %d mutating entry points found", len(found))
	}
}

func auditProblem(fn *ast.FuncDecl, calls []string) string {
	deferred := deferredCalls(fn.Body)

	if i := slices.Index(deferred, "recordAction"); i >= 0 {
		mask := slices.IndexFunc(deferred, func(d string) bool { return d == "maskErr" || d == "maskResult" })
		if mask < 0 || mask > i {
			return "defer recordAction must come after defer maskErr/maskResult"
		}

		return ""
	}

	if slices.Contains(calls, "recordedRun") {
		return ""
	}

	if slices.Contains(calls, "recordOutcome") {
		return "a background run records through recordedRun, so a panic is recorded too"
	}

	return "changes a cluster but records no audit entry (defer recordAction or recordedRun)"
}

// calledNames lists the functions called anywhere in body, closures included.
func calledNames(body *ast.BlockStmt) []string {
	var out []string

	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			out = append(out, exprName(call.Fun))
		}

		return true
	})

	return out
}
