package ichorgo

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

type lhAttachmentVolume struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		DeletionTimestamp string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Spec struct {
		NodeID          string `json:"nodeID"`
		MigrationNodeID string `json:"migrationNodeID"`
	} `json:"spec"`
	Status struct {
		State           string          `json:"state"`
		Robustness      string          `json:"robustness"`
		CurrentNodeID   string          `json:"currentNodeID"`
		RestoreRequired bool            `json:"restoreRequired"`
		IsStandby       bool            `json:"isStandby"`
		Conditions      []kubeCondition `json:"conditions"`
	} `json:"status"`
}
type lhAttachmentNode struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		DeletionTimestamp string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}
type longhornAttachmentNode struct {
	Name     string `json:"name"`
	Eligible bool   `json:"eligible"`
}
type longhornAttachmentPreview struct {
	Namespace string                   `json:"namespace"`
	Name      string                   `json:"name"`
	State     string                   `json:"state"`
	Node      string                   `json:"node"`
	CanAttach bool                     `json:"canAttach"`
	CanDetach bool                     `json:"canDetach"`
	Error     string                   `json:"error"`
	Nodes     []longhornAttachmentNode `json:"nodes"`
}

// KubeLonghornAttachmentPreview refreshes volume state and available attachment nodes.
func KubeLonghornAttachmentPreview(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	if err := validateKubeName("volume", namespace, name); err != nil {
		return "", err
	}
	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() longhornAttachmentPreview {
		p := longhornAttachmentPreview{Namespace: namespace, Name: name, Nodes: []longhornAttachmentNode{}}
		d := demoDataServices(time.Now()).Longhorn
		for _, v := range d.Volumes {
			if v.Namespace == namespace && v.Name == name {
				p.State, p.Node = v.State, v.Node
				p.CanAttach = v.State == "detached" && v.Robustness != "faulted"
				p.CanDetach = v.State == "attached" && v.Node != ""
				for _, n := range d.Nodes {
					p.Nodes = append(p.Nodes, longhornAttachmentNode{Name: n.Name, Eligible: n.Ready})
				}
				return p
			}
		}
		p.Error = "the demo volume was not found"
		return p
	}, func(ctx context.Context, k *kubeClient) (longhornAttachmentPreview, error) {
		return readLonghornAttachmentPreview(ctx, k, namespace, name)
	})
}

// KubeLonghornAttachment requests a regular manager attachment or detachment (os:admin).
func KubeLonghornAttachment(configYAML, contextName, kubeServer, namespace, name, node string, attach bool) (err error) {
	defer maskErr(&err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, name, node = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name)), privacy.reveal(strings.TrimSpace(node))
	contextName, node = unmaskTarget(configYAML, contextName, node)
	action := "longhorn-detach"
	if attach {
		action = "longhorn-attach"
	}
	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: action, Namespace: namespace, Object: name, Params: "node=" + node})
	if err := validateKubeName("volume", namespace, name); err != nil {
		return err
	}
	if err := validateKubeName("node", namespace, node); err != nil {
		return err
	}
	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return changeLonghornAttachment(ctx, k, namespace, name, node, attach)
	})
}
func longhornAttachmentBase(ctx context.Context, k *kubeClient, namespace string) (string, error) {
	g, e := readAPIGroups(ctx, k)
	if e != nil {
		return "", e
	}
	v, ok := g[groupLonghorn]
	if !ok {
		return "", errLonghornMissing
	}
	return "/apis/" + groupLonghorn + "/" + v + "/namespaces/" + url.PathEscape(namespace), nil
}
func longhornAttachmentAllowed(v lhAttachmentVolume) (bool, bool) {
	stable := v.Metadata.DeletionTimestamp == "" && v.Spec.MigrationNodeID == "" && !v.Status.IsStandby
	attach := stable && v.Status.State == "detached" && v.Spec.NodeID == "" && v.Status.CurrentNodeID == "" && v.Status.Robustness != "faulted" && !v.Status.RestoreRequired && conditionStatus(v.Status.Conditions, "Restore") != "True"
	detach := stable && v.Status.State == "attached" && v.Status.CurrentNodeID != "" && v.Spec.NodeID == v.Status.CurrentNodeID
	return attach, detach
}
func readLonghornAttachmentPreview(ctx context.Context, k *kubeClient, namespace, name string) (longhornAttachmentPreview, error) {
	p := longhornAttachmentPreview{Namespace: namespace, Name: name, Nodes: []longhornAttachmentNode{}}
	base, e := longhornAttachmentBase(ctx, k, namespace)
	if e != nil {
		return p, e
	}
	var v lhAttachmentVolume
	if e := k.get(ctx, base+"/volumes/"+url.PathEscape(name), &v); e != nil {
		return p, e
	}
	p.State, p.Node = v.Status.State, v.Status.CurrentNodeID
	p.CanAttach, p.CanDetach = longhornAttachmentAllowed(v)
	var nodes kubeList[lhAttachmentNode]
	if e := getList(ctx, k, base+"/nodes", &nodes); e != nil {
		p.Error = e.Error()
		p.CanAttach = false
		return p, nil
	}
	for _, n := range nodes.Items {
		p.Nodes = append(p.Nodes, longhornAttachmentNode{Name: n.Metadata.Name, Eligible: n.Metadata.DeletionTimestamp == "" && conditionStatus(n.Status.Conditions, "Ready") == "True"})
	}
	slices.SortFunc(p.Nodes, func(a, b longhornAttachmentNode) int { return strings.Compare(a.Name, b.Name) })
	return p, nil
}
func changeLonghornAttachment(ctx context.Context, k *kubeClient, namespace, name, node string, attach bool) error {
	base, e := longhornAttachmentBase(ctx, k, namespace)
	if e != nil {
		return e
	}
	var v lhAttachmentVolume
	if e := k.get(ctx, base+"/volumes/"+url.PathEscape(name), &v); e != nil {
		return e
	}
	canAttach, canDetach := longhornAttachmentAllowed(v)
	if attach && !canAttach {
		return errors.New("attach requires a detached non-faulted volume without a pending attachment or restore")
	}
	if !attach && (!canDetach || v.Status.CurrentNodeID != node) {
		return errors.New("the volume is no longer attached to the selected node")
	}
	if attach {
		var n lhAttachmentNode
		if e := k.get(ctx, base+"/nodes/"+url.PathEscape(node), &n); e != nil {
			return e
		}
		if n.Metadata.Name != node || n.Metadata.DeletionTimestamp != "" || conditionStatus(n.Status.Conditions, "Ready") != "True" {
			return errors.New("the attachment node is not ready")
		}
	}
	op := "detach"
	payload := map[string]any{"hostId": node, "attachmentID": "", "forceDetach": false}
	if attach {
		op = "attach"
		payload = map[string]any{"hostId": node, "attachmentID": "", "disableFrontend": false, "attachedBy": "ichor"}
	}
	return k.post(ctx, serviceProxyPath(namespace, lhManagerService, "/v1/volumes/"+url.PathEscape(name)+"?action="+op), payload, nil)
}
