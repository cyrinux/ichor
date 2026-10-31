package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/cyrinux/ichor/go/gitforge"
	"github.com/cyrinux/ichor/go/kube/kubediff"
)

// The review step of a file edited in the source browser, and the defaults of the pull
// request it becomes: both apps render the diff with their GitOps diff screens.

// textDiffKind is the resource kind of a file in a TextDiff answer.
const textDiffKind = "file"

var errTextDiffName = errors.New("no file name given")

// TextDiff compares two versions of the file name (its path in the repository): the
// gitOpsDiff shape of the Flux and Argo CD diffs, {"kind":"file","name",...,"resources":[one
// {"kind":"file","name","change":"changed"|"unchanged","diff","truncated",...}],"warnings":[]},
// with a unified diff from before to after. Line endings are compared as "\n". Each text is
// at most 1 MiB.
func TextDiff(before, after, name string) (out string, err error) {
	defer maskResult(&out, &err)

	name = strings.TrimSpace(name)
	if name == "" {
		return "", errTextDiffName
	}

	for _, s := range []string{before, after} {
		if len(s) > gitforge.MaxFile {
			return "", fmt.Errorf("%s is over 1 MiB: compare it on a computer", name)
		}
	}

	b, err := json.Marshal(textDiff(before, after, name))
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// textDiff is TextDiff's answer once its input is checked.
func textDiff(before, after, name string) gitOpsDiff {
	from, to := textDiffNormalize(before), textDiffNormalize(after)
	r := kubeDiffResource{Kind: textDiffKind, Name: name, Change: diffChangeUnchanged}

	if from != to {
		r.Change = diffChangeChanged
		r.Diff = unifiedDiff(from, to, "a/"+name, "b/"+name)

		if len(r.Diff) > kubediff.MaxBytes {
			cut := strings.LastIndexByte(r.Diff[:kubediff.MaxBytes], '\n')
			r.Diff, r.Truncated = r.Diff[:cut+1], true
		}
	}

	return gitOpsDiff{Kind: textDiffKind, Name: name, Resources: []kubeDiffResource{r}, Warnings: []string{}}
}

// textDiffNormalize turns CRLF (and lone CR) line endings into "\n".
func textDiffNormalize(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// gitOpsProposal is the prefilled text of a pull request opened from the source browser.
type gitOpsProposal struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
	Body    string `json:"body"`
}

// gitOpsToolLabels names the tools as the apps show them.
var gitOpsToolLabels = map[string]string{"argocd": "Argo CD", "flux": "Flux"}

// GitOpsProposalDefaults is the prefilled pull request for a change of the file path
// deployed by the GitOps object kind namespace/name of tool ("argocd" or "flux") on cluster:
// {"title","message","branch","body"}, branch ichor/<name>-<file>-<4 hex>; the body leaves out
// " on <cluster>" when cluster is "". No network.
func GitOpsProposalDefaults(tool, kind, namespace, name, path, cluster string) (out string, err error) {
	defer maskResult(&out, &err)

	p, err := gitOpsProposalDefaults(tool, kind, namespace, name, path, cluster)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func gitOpsProposalDefaults(tool, kind, namespace, name, path, cluster string) (gitOpsProposal, error) {
	label, ok := gitOpsToolLabels[strings.TrimSpace(tool)]
	if !ok {
		return gitOpsProposal{}, fmt.Errorf("unknown GitOps tool %q: argocd or flux", tool)
	}

	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return gitOpsProposal{}, errTextDiffName
	}

	kind, namespace, name = strings.TrimSpace(kind), strings.TrimSpace(namespace), strings.TrimSpace(name)
	file := strings.TrimSuffix(pathpkg.Base(path), pathpkg.Ext(path))
	title := "Update " + path

	body := fmt.Sprintf("Proposed from Ichor for %s %s %s/%s", label, kind, namespace, name)
	if cluster = strings.TrimSpace(cluster); cluster != "" {
		body += " on " + cluster
	}

	return gitOpsProposal{
		Title:   title,
		Message: title,
		Branch:  "ichor/" + gitforge.Slug(strings.Trim(name+"-"+file, "-")) + "-" + gitforge.BranchSuffix(),
		Body:    body + ".",
	}, nil
}
