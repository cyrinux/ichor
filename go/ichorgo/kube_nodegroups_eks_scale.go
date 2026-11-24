package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Changing an EKS managed node group's size (UpdateNodegroupConfig) and following the update
// it starts (DescribeUpdate), with the credentials of the cluster's own sign-in. No
// Kubernetes RBAC verb is involved: AWS' refusal is the gate.

const (
	eksActionUpdateNodegroupConfig = "eks:UpdateNodegroupConfig"
	eksActionDescribeUpdate        = "eks:DescribeUpdate"

	// eksUpdateSuccessful is AWS' own status of a finished update.
	eksUpdateSuccessful = "Successful"
)

var errNotEKSNodeGroups = errors.New("this cluster does not sign in through EKS: its node groups cannot be changed from here")

// nodeGroupUpdate is an update AWS runs on a node group: its id, status (InProgress,
// Failed, Cancelled, Successful) and type, and why it failed.
type nodeGroupUpdate struct {
	ID     string           `json:"id"`
	Status string           `json:"status"`
	Type   string           `json:"type"`
	Errors []nodeGroupIssue `json:"errors"`
}

type eksUpdate struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Type   string `json:"type"`
	Errors []struct {
		Code        string   `json:"errorCode"`
		Message     string   `json:"errorMessage"`
		ResourceIDs []string `json:"resourceIds"`
	} `json:"errors"`
}

func (u eksUpdate) row() nodeGroupUpdate {
	out := nodeGroupUpdate{ID: u.ID, Status: u.Status, Type: u.Type, Errors: []nodeGroupIssue{}}

	for _, e := range u.Errors {
		ids := e.ResourceIDs
		if ids == nil {
			ids = []string{}
		}

		out.Errors = append(out.Errors, nodeGroupIssue{Code: e.Code, Message: e.Message, ResourceIDs: ids})
	}

	return out
}

// KubeNodeGroupScale sets the EKS managed node group's desired, minimum and maximum size:
// {"id","status","type","errors"} of the update AWS starts (see nodeGroupUpdate). Refused
// before any call for an impossible size, a cluster that does not sign in through EKS, a
// group already updating, and demo contexts. A POST that got no answer is never retried.
// kubeServer: see KubePods.
func KubeNodeGroupScale(configYAML, contextName, kubeServer, group string, desired, minSize, maxSize int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	group = privacy.reveal(strings.TrimSpace(group))

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "scale-nodegroup", Object: "NodeGroup/" + group,
		Params: fmt.Sprintf("desired=%d,min=%d,max=%d", desired, minSize, maxSize),
	})

	if err := validateNodeGroupSize(desired, minSize, maxSize); err != nil {
		return "", err
	}

	if group == "" {
		return "", errors.New("no node group named")
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return "", errDemoUnavailable
	}

	update, err := withKubeFrom(context.Background(), target, func(ctx context.Context, k *kubeClient) (nodeGroupUpdate, error) {
		return scaleEKSNodeGroup(ctx, k, group, desired, minSize, maxSize)
	})
	if err != nil {
		return "", err
	}

	return toJSON(update)
}

// validateNodeGroupSize refuses what UpdateNodegroupConfig would.
func validateNodeGroupSize(desired, minSize, maxSize int) error {
	switch {
	case minSize < 0:
		return errors.New("the minimum size must be 0 or more")
	case maxSize < 1:
		return errors.New("the maximum size must be at least 1")
	case minSize > maxSize:
		return fmt.Errorf("the minimum size (%d) must not be above the maximum (%d)", minSize, maxSize)
	case desired < minSize || desired > maxSize:
		return fmt.Errorf("the desired size (%d) must be between the minimum (%d) and the maximum (%d)", desired, minSize, maxSize)
	}

	return nil
}

func scaleEKSNodeGroup(ctx context.Context, k *kubeClient, group string, desired, minSize, maxSize int) (nodeGroupUpdate, error) {
	m, creds, err := eksSigner(ctx, k)
	if err != nil {
		return nodeGroupUpdate{}, err
	}

	path := eksNodeGroupPath(m, group)

	if _, err := describeUpdatableNodeGroup(ctx, m, creds, path, "change its size"); err != nil {
		return nodeGroupUpdate{}, err
	}

	body, err := json.Marshal(map[string]any{
		"scalingConfig":      map[string]int{"minSize": minSize, "maxSize": maxSize, "desiredSize": desired},
		"clientRequestToken": randomToken(),
	})
	if err != nil {
		return nodeGroupUpdate{}, err
	}

	var answer struct {
		Update eksUpdate `json:"update"`
	}

	if err := eksCall(ctx, m, creds, http.MethodPost, path+"/update-config", nil, body, &answer); err != nil {
		return nodeGroupUpdate{}, eksUpdateError(err, eksActionUpdateNodegroupConfig)
	}

	return answer.Update.row(), nil
}

func eksNodeGroupPath(m *eksMethod, group string) string {
	return "/clusters/" + url.PathEscape(m.cluster) + "/node-groups/" + url.PathEscape(group)
}

// describeUpdatableNodeGroup reads the group at path and refuses one AWS would not update:
// what names the change in the refusal. A degraded group (instances that do not launch) may
// still change: that is often why it is changed. One already changing may not.
func describeUpdatableNodeGroup(ctx context.Context, m *eksMethod, creds awsCredentials, path, what string) (eksNodegroup, error) {
	var described struct {
		Nodegroup eksNodegroup `json:"nodegroup"`
	}

	if err := eksCall(ctx, m, creds, http.MethodGet, path, nil, nil, &described); err != nil {
		return eksNodegroup{}, eksReadError(err, eksActionDescribeNodegroup)
	}

	if s := described.Nodegroup.Status; s == eksNodeGroupUpdating {
		return eksNodegroup{}, errors.New("the group is updating: wait for it to finish")
	} else if s != "ACTIVE" && s != "DEGRADED" {
		return eksNodegroup{}, fmt.Errorf("the group is %s: only an active group can %s", strings.ToLower(s), what)
	}

	return described.Nodegroup, nil
}

// eksUpdateError words AWS' answer to a node group update (UpdateNodegroupConfig,
// UpdateNodegroupVersion): a refusal names action; a POST that got none may have been
// applied (kubeMutationError).
func eksUpdateError(err error, action string) error {
	var e *cloudHTTPError
	if !errors.As(err, &e) {
		return kubeMutationError(err)
	}

	switch e.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New(action + " is not allowed for this role")
	case http.StatusConflict:
		return errors.New("another update is running on this group")
	default:
		return errors.New(e.message)
	}
}

// eksReadError words AWS' answer to a read: a refusal names action.
func eksReadError(err error, action string) error {
	if unauthorized(err) {
		return errors.New(action + " is not allowed for this role")
	}

	var e *cloudHTTPError
	if errors.As(err, &e) {
		return errors.New(e.message)
	}

	return err
}

// KubeNodeGroupUpdate reads the update updateID of the EKS managed node group:
// {"id","status","type","errors"} (see nodeGroupUpdate). The demo answers a finished update.
// kubeServer: see KubePods.
func KubeNodeGroupUpdate(configYAML, contextName, kubeServer, group, updateID string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	group, updateID = privacy.reveal(strings.TrimSpace(group)), strings.TrimSpace(updateID)
	target := kubeTarget{configYAML, contextName, kubeServer}

	demo := func() nodeGroupUpdate {
		return nodeGroupUpdate{ID: updateID, Status: eksUpdateSuccessful, Type: "ConfigUpdate", Errors: []nodeGroupIssue{}}
	}

	return kubeReadJSON(target, demo, func(ctx context.Context, k *kubeClient) (nodeGroupUpdate, error) {
		if group == "" || updateID == "" {
			return nodeGroupUpdate{}, errors.New("no node group or update named")
		}

		m, creds, err := eksSigner(ctx, k)
		if err != nil {
			return nodeGroupUpdate{}, err
		}

		path := "/clusters/" + url.PathEscape(m.cluster) + "/updates/" + url.PathEscape(updateID)

		var answer struct {
			Update eksUpdate `json:"update"`
		}

		if err := eksCall(ctx, m, creds, http.MethodGet, path, url.Values{"nodegroupName": {group}}, nil, &answer); err != nil {
			return nodeGroupUpdate{}, eksReadError(err, eksActionDescribeUpdate)
		}

		return answer.Update.row(), nil
	})
}

// eksSigner is the cluster's EKS sign-in and the credentials it signs AWS calls with.
func eksSigner(ctx context.Context, k *kubeClient) (*eksMethod, awsCredentials, error) {
	if k.tokens == nil {
		return nil, awsCredentials{}, errNotEKSNodeGroups
	}

	m, ok := k.tokens.method.(*eksMethod)
	if !ok {
		return nil, awsCredentials{}, errNotEKSNodeGroups
	}

	creds, err := eksSourceCredentials(ctx, k.tokens, m)
	if err != nil {
		return nil, awsCredentials{}, errors.New(nodeGroupsCredentialsError(err))
	}

	return m, creds, nil
}

// eksCall sends a signed call to the EKS API of m's region (body signed when there is one)
// and decodes its JSON answer into out.
func eksCall(ctx context.Context, m *eksMethod, creds awsCredentials, method, path string, query url.Values, body []byte, out any) error {
	u := eksEndpoint(m.region) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	hash := emptySHA256

	var reader io.Reader
	if body != nil {
		hash, reader = sha256Hex(body), bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	sigV4Sign(req, hash, creds, m.region, "eks", time.Now())

	return cloudJSON(req, out)
}
