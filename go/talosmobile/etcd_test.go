package talosmobile

import (
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

func TestBuildEtcdOverview(t *testing.T) {
	members := &machineapi.EtcdMembers{Members: []*machineapi.EtcdMember{
		{Id: 0xbeef, Hostname: "cp-2", PeerUrls: []string{"https://10.0.0.3:2380"}},
		{Id: 0xabc, Hostname: "cp-1", PeerUrls: []string{"https://10.0.0.2:2380"}, IsLearner: true},
	}}

	statuses := []etcdProbe{
		{node: "10.0.0.2", status: &machineapi.EtcdMemberStatus{
			MemberId: 0xabc, Leader: 0xbeef, DbSize: 2048, DbSizeInUse: 1024,
			RaftIndex: 10, RaftTerm: 3, StorageVersion: "3.6.0",
		}},
		{node: "10.0.0.3", status: &machineapi.EtcdMemberStatus{MemberId: 0xbeef, Leader: 0xbeef, Errors: []string{"NOSPACE"}}},
		{node: "10.0.0.4", err: errors.New("down")},
	}

	alarms := []*machineapi.EtcdMemberAlarm{{MemberId: 0xbeef, Alarm: machineapi.EtcdMemberAlarm_NOSPACE}}

	got := buildEtcdOverview(members, nil, statuses, alarms)

	if got.Error != "" {
		t.Fatalf("unexpected error %q", got.Error)
	}

	if len(got.Members) != 2 || got.Members[0].Hostname != "cp-1" || got.Members[0].ID != "abc" || !got.Members[0].IsLearner {
		t.Errorf("members = %+v", got.Members)
	}

	if got.LeaderID != "beef" {
		t.Errorf("leader = %q", got.LeaderID)
	}

	if len(got.Statuses) != 3 {
		t.Fatalf("statuses = %+v", got.Statuses)
	}

	s0 := got.Statuses[0]
	if s0.MemberID != "abc" || s0.IsLeader || s0.DbSize != 2048 || s0.Version != "3.6.0" {
		t.Errorf("status[0] = %+v", s0)
	}

	if !got.Statuses[1].IsLeader || len(got.Statuses[1].Errors) != 1 {
		t.Errorf("status[1] = %+v", got.Statuses[1])
	}

	if got.Statuses[2].Error == "" {
		t.Errorf("status[2] should carry error: %+v", got.Statuses[2])
	}

	if len(got.Alarms) != 1 || got.Alarms[0].Alarm != "NOSPACE" || got.Alarms[0].MemberID != "beef" {
		t.Errorf("alarms = %+v", got.Alarms)
	}
}

func TestBuildEtcdOverviewMemberListError(t *testing.T) {
	got := buildEtcdOverview(nil, errors.New("boom"), nil, nil)

	if got.Error == "" || got.Members == nil || got.Statuses == nil || got.Alarms == nil {
		t.Errorf("got %+v", got)
	}
}
