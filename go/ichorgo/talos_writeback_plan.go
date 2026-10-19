package ichorgo

import (
	"errors"
	pathpkg "path"
	"slices"
	"strings"
)

// Where a write-back goes: the node's own file of the cluster repository (TalosRepoFiles'
// node file), or a new one under the Talos directory, with the values a shared patch sets
// taken out of the patch. The editor then applies the rest with the "talos" quick edit.

var errWriteBackTalhelper = errors.New("talconfig.yaml lists this node without a patch file of its own: add the change to talconfig.yaml by hand")

type talosWriteBackPlan struct {
	SHA  string `json:"sha"`
	File string `json:"file"`
	// Create: the repository has no file for the node; the proposal adds File.
	Create bool `json:"create"`
	// Patch is the patch without the skipped values: the "talos" quick edit's.
	Patch   string          `json:"patch"`
	Skipped []talosRepoSkip `json:"skipped"`
}

// TalosRepoWriteBackPlan says where patchYAML (TalosConfigPatchFrom's) is written for the node
// hostname of role in repoURL at ref, under talosDir: {"sha","file","create","patch",
// "skipped":[{"path","why"}]}. The file is the node's own; without one, a new
// <talosDir>/nodes/<hostname>.yaml (create). A value only a shared patch sets is skipped and
// left out of patch, which is empty when every value is.
func TalosRepoWriteBackPlan(hostJSON, repoURL, ref, talosDir, hostname, role, patchYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	hostname = privacy.unmaskNode(hostname)

	snap, _, err := loadRepoSnapshot(hostJSON, repoURL, ref)
	if err != nil {
		return "", err
	}

	plan, err := planTalosWriteBack(snap.files, talosDir, hostname, role, patchYAML)
	if err != nil {
		return "", err
	}

	plan.SHA = snap.sha

	return toJSON(plan)
}

func planTalosWriteBack(files map[string][]byte, talosDir, hostname, role, patchYAML string) (talosWriteBackPlan, error) {
	found := findTalosRepoFiles(files, talosDir, hostname, role)
	plan := talosWriteBackPlan{Skipped: []talosRepoSkip{}}
	shared := map[string]string{}

	for _, f := range found.Files {
		switch {
		case f.Kind == talosFileNode && f.Patch && plan.File == "":
			plan.File = f.Path
		case f.Kind == talosFileShared && f.Patch:
			shared[f.Path] = string(files[f.Path])
		}
	}

	if plan.File == "" {
		if found.Generator == talosGenTalhelper {
			return talosWriteBackPlan{}, errWriteBackTalhelper
		}

		dir := strings.Trim(talosDir, "/")
		if dir == "" {
			dir = "talos"
		}

		plan.File, plan.Create = pathpkg.Join(dir, "nodes", shortOr(hostname)+".yaml"), true
	}

	leaves, err := talosPatchLeaves(patchYAML)
	if err != nil {
		return talosWriteBackPlan{}, err
	}

	var kept []talosLeaf

	for _, l := range leaves {
		if !plan.Create && holdsLeaf(string(files[plan.File]), l) {
			kept = append(kept, l)

			continue
		}

		if owner := sharedOwner(shared, l); owner != "" {
			plan.Skipped = append(plan.Skipped, talosRepoSkip{Path: strings.Join(l.path, "."), Why: "set in " + owner})

			continue
		}

		kept = append(kept, l)
	}

	patch, err := leavesPatch(kept)
	if err != nil {
		return talosWriteBackPlan{}, err
	}

	plan.Patch = patch

	return plan, nil
}

// holdsLeaf tells whether content's document of l holds l's path.
func holdsLeaf(content string, l talosLeaf) bool {
	docs, err := fileDocs(content)
	if err != nil {
		return false
	}

	i := slices.IndexFunc(docs, func(d fileDoc) bool { return d.key == l.doc })

	return i >= 0 && holdsPath(docs[i].node, l.path)
}

// leavesPatch writes leaves back as a patch, a document per document key.
func leavesPatch(leaves []talosLeaf) (string, error) {
	byDoc := map[string]map[string]any{}

	var order []string

	for _, l := range leaves {
		doc := byDoc[l.doc]
		if doc == nil {
			doc = map[string]any{}
			for k, v := range l.ident {
				doc[k] = v
			}

			byDoc[l.doc] = doc
			order = append(order, l.doc)
		}

		setYAMLPath(doc, l.path, l.value)
	}

	docs := make([]map[string]any, 0, len(order))
	for _, k := range order {
		docs = append(docs, byDoc[k])
	}

	if len(docs) == 0 {
		return "", nil
	}

	return encodeYAMLDocs(docs)
}

// shortOr is a hostname's first label, the hostname when it has none (an address).
func shortOr(hostname string) string {
	if s := shortHost(hostname); s != "" {
		return s
	}

	return hostname
}

// talosQuickEdit is the "talos" quick edit: e.Patch written into content (an empty content and
// e.Create make a new node file named after e.Hostname); e.Skipped, the plan's skipped values,
// are reported with the write-back's own.
func talosQuickEdit(content string, e quickEdit) (quickEditResult, error) {
	res, err := talosRepoWriteBack(content, e.Patch, talosWriteBackOptions{Hostname: e.Hostname, Create: e.Create})
	if err != nil {
		return quickEditResult{}, err
	}

	skipped := slices.Clone(e.Skipped)
	for _, s := range res.Skipped {
		skipped = append(skipped, s.Path+": "+s.Why)
	}

	return quickEditResult{Content: res.Content, Changed: res.Changed, Line: res.Line, Skipped: skipped}, nil
}
