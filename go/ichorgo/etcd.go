package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

type etcdOverview struct {
	Error    string           `json:"error,omitempty"`
	LeaderID string           `json:"leaderId"`
	Members  []etcdMember     `json:"members"`
	Statuses []etcdNodeStatus `json:"statuses"`
	Alarms   []etcdAlarm      `json:"alarms"`
	// AlarmsError is set when no control-plane node answered the alarm list: Alarms is then
	// unknown, not empty.
	AlarmsError string `json:"alarmsError,omitempty"`
}

type etcdMember struct {
	ID         string   `json:"id"`
	Hostname   string   `json:"hostname"`
	PeerURLs   []string `json:"peerUrls"`
	ClientURLs []string `json:"clientUrls"`
	IsLearner  bool     `json:"isLearner"`
}

type etcdNodeStatus struct {
	Node        string `json:"node"`
	Error       string `json:"error,omitempty"`
	MemberID    string `json:"memberId"`
	IsLeader    bool   `json:"isLeader"`
	IsLearner   bool   `json:"isLearner"`
	DbSize      int64  `json:"dbSize"`
	DbSizeInUse int64  `json:"dbSizeInUse"`
	RaftIndex   uint64 `json:"raftIndex"`
	RaftTerm    uint64 `json:"raftTerm"`
	// RaftAppliedIndex trails RaftIndex (committed) while the member applies its backlog.
	RaftAppliedIndex uint64   `json:"raftAppliedIndex"`
	Version          string   `json:"version"`
	Errors           []string `json:"errors"`
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

	if isDemoContext(configYAML, contextName) {
		return demoRead("EtcdStatus", configYAML, contextName, "")
	}

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
	cps, err := s.controlPlanes(ctx)
	if err != nil {
		return etcdOverview{}, err
	}

	return fetchEtcd(ctx, s.client, cps), nil
}

// fetchEtcd asks every control-plane node for its etcd status, then reads the member list
// and the alarms through the first node that answers them.
func fetchEtcd(ctx context.Context, c *client.Client, cps []string) etcdOverview {
	probes := make([]etcdProbe, len(cps))

	forEachNode(cps, func(i int, node string) {
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

	order := queryOrder(probes)

	members, membersErr := firstAnswer(order, func(node string) (*machineapi.EtcdMembers, error) {
		resp, err := c.EtcdMemberList(client.WithNode(ctx, node), &machineapi.EtcdMemberListRequest{QueryLocal: false})
		if err != nil {
			return nil, err
		}

		return first(resp.GetMessages()), nil
	})

	alarms, alarmsErr := firstAnswer(order, func(node string) ([]*machineapi.EtcdMemberAlarm, error) {
		resp, err := c.EtcdAlarmList(client.WithNode(ctx, node))
		if err != nil {
			return nil, err
		}

		return first(resp.GetMessages()).GetMemberAlarms(), nil
	})

	return buildEtcdOverview(members, membersErr, probes, alarms, alarmsErr)
}

// queryOrder lists the probed nodes with those whose etcd answered the status probe first,
// so cluster-wide reads skip a down member instead of failing on it.
func queryOrder(probes []etcdProbe) []string {
	up := make([]string, 0, len(probes))
	down := make([]string, 0, len(probes))

	for _, p := range probes {
		if p.err == nil {
			up = append(up, p.node)
		} else {
			down = append(down, p.node)
		}
	}

	return append(up, down...)
}

// firstAnswer calls ask on each node in turn and returns the first success, or the last
// error when every node fails.
func firstAnswer[T any](nodes []string, ask func(node string) (T, error)) (T, error) {
	err := errors.New("no control-plane node to ask")

	for _, node := range nodes {
		v, askErr := ask(node)
		if askErr == nil {
			return v, nil
		}

		err = askErr
	}

	var zero T

	return zero, err
}

func buildEtcdOverview(
	members *machineapi.EtcdMembers, membersErr error, probes []etcdProbe,
	alarms []*machineapi.EtcdMemberAlarm, alarmsErr error,
) etcdOverview {
	out := etcdOverview{
		Members:  []etcdMember{},
		Statuses: []etcdNodeStatus{},
		Alarms:   []etcdAlarm{},
	}

	if membersErr != nil {
		out.Error = fmt.Sprintf("member list: %s", friendlyError(membersErr))
	}

	if alarmsErr != nil {
		out.AlarmsError = friendlyError(alarmsErr)
	}

	for _, m := range members.GetMembers() {
		out.Members = append(out.Members, etcdMember{
			ID:         hexID(m.GetId()),
			Hostname:   m.GetHostname(),
			PeerURLs:   orEmpty(m.GetPeerUrls()),
			ClientURLs: orEmpty(m.GetClientUrls()),
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
			st.RaftAppliedIndex = s.GetRaftAppliedIndex()
			st.Version = s.GetStorageVersion()
			st.Errors = orEmpty(s.GetErrors())

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
