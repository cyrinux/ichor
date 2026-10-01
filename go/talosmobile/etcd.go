package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

type etcdOverview struct {
	Error    string           `json:"error,omitempty"`
	LeaderID string           `json:"leaderId"`
	Members  []etcdMember     `json:"members"`
	Statuses []etcdNodeStatus `json:"statuses"`
	Alarms   []etcdAlarm      `json:"alarms"`
}

type etcdMember struct {
	ID         string   `json:"id"`
	Hostname   string   `json:"hostname"`
	PeerURLs   []string `json:"peerUrls"`
	ClientURLs []string `json:"clientUrls"`
	IsLearner  bool     `json:"isLearner"`
}

type etcdNodeStatus struct {
	Node        string   `json:"node"`
	Error       string   `json:"error,omitempty"`
	MemberID    string   `json:"memberId"`
	IsLeader    bool     `json:"isLeader"`
	IsLearner   bool     `json:"isLearner"`
	DbSize      int64    `json:"dbSize"`
	DbSizeInUse int64    `json:"dbSizeInUse"`
	RaftIndex   uint64   `json:"raftIndex"`
	RaftTerm    uint64   `json:"raftTerm"`
	Version     string   `json:"version"`
	Errors      []string `json:"errors"`
}

type etcdAlarm struct {
	MemberID string `json:"memberId"`
	Alarm    string `json:"alarm"`
}

type etcdProbe struct {
	node   string
	status *machineapi.EtcdMemberStatus
	err    error
}

// EtcdStatus discovers control-plane nodes in the context and returns JSON etcdOverview.
func EtcdStatus(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		out, err := gatherEtcdOverview(ctx, s)
		if err != nil {
			return "", err
		}

		for _, m := range out.Members {
			privacy.learnHost(m.Hostname, "controlplane")
		}

		return toJSON(out)
	})
}

// gatherEtcdOverview finds the context's control-plane nodes and reads etcd through them.
func gatherEtcdOverview(ctx context.Context, s *session) (etcdOverview, error) {
	cps := classifyNodes(ctx, s.client, targetNodes(s.context)).GetControlPlaneNodes()
	if len(cps) == 0 {
		return etcdOverview{}, errors.New("no reachable control-plane node found in this context")
	}

	return fetchEtcd(ctx, s.client, cps), nil
}

// fetchEtcd asks every control-plane node for its etcd status, and the first one for the
// member list and the alarms.
func fetchEtcd(ctx context.Context, c *client.Client, cps []string) etcdOverview {
	probes := make([]etcdProbe, len(cps))

	var wg sync.WaitGroup

	for i, node := range cps {
		wg.Go(func() {
			probes[i] = etcdProbe{node: node}

			resp, err := c.EtcdStatus(client.WithNode(ctx, node))
			if err != nil {
				probes[i].err = err

				return
			}

			if m := first(resp.GetMessages()); m != nil {
				probes[i].status = m.GetMemberStatus()
			}
		})
	}

	wg.Wait()

	cpCtx := client.WithNode(ctx, cps[0])

	var members *machineapi.EtcdMembers

	membersResp, membersErr := c.EtcdMemberList(cpCtx, &machineapi.EtcdMemberListRequest{QueryLocal: false})
	if membersErr == nil {
		members = first(membersResp.GetMessages())
	}

	var alarms []*machineapi.EtcdMemberAlarm

	if alarmResp, err := c.EtcdAlarmList(cpCtx); err == nil {
		if m := first(alarmResp.GetMessages()); m != nil {
			alarms = m.GetMemberAlarms()
		}
	}

	return buildEtcdOverview(members, membersErr, probes, alarms)
}

func buildEtcdOverview(
	members *machineapi.EtcdMembers, membersErr error, probes []etcdProbe, alarms []*machineapi.EtcdMemberAlarm,
) etcdOverview {
	out := etcdOverview{
		Members:  []etcdMember{},
		Statuses: []etcdNodeStatus{},
		Alarms:   []etcdAlarm{},
	}

	if membersErr != nil {
		out.Error = fmt.Sprintf("member list: %s", friendlyError(membersErr))
	}

	for _, m := range members.GetMembers() {
		out.Members = append(out.Members, etcdMember{
			ID:         hexID(m.GetId()),
			Hostname:   m.GetHostname(),
			PeerURLs:   nonNil(m.GetPeerUrls()),
			ClientURLs: nonNil(m.GetClientUrls()),
			IsLearner:  m.GetIsLearner(),
		})
	}

	sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].Hostname < out.Members[j].Hostname })

	for _, p := range probes {
		st := etcdNodeStatus{Node: p.node, Errors: []string{}}

		if p.err != nil {
			st.Error = friendlyError(p.err)
		} else if s := p.status; s != nil {
			st.MemberID = hexID(s.GetMemberId())
			st.IsLeader = s.GetLeader() != 0 && s.GetLeader() == s.GetMemberId()
			st.IsLearner = s.GetIsLearner()
			st.DbSize = s.GetDbSize()
			st.DbSizeInUse = s.GetDbSizeInUse()
			st.RaftIndex = s.GetRaftIndex()
			st.RaftTerm = s.GetRaftTerm()
			st.Version = s.GetStorageVersion()
			st.Errors = nonNil(s.GetErrors())

			if out.LeaderID == "" && s.GetLeader() != 0 {
				out.LeaderID = hexID(s.GetLeader())
			}
		}

		out.Statuses = append(out.Statuses, st)
	}

	for _, a := range alarms {
		out.Alarms = append(out.Alarms, etcdAlarm{MemberID: hexID(a.GetMemberId()), Alarm: a.GetAlarm().String()})
	}

	return out
}

func hexID(id uint64) string {
	return strconv.FormatUint(id, 16)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}
