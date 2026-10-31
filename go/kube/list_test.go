package kube_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubetest"
)

type listItem struct {
	Name string `json:"name"`
}

func TestAPIGroups(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET /apis": `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`})

	groups, err := kube.APIGroups(context.Background(), f.KubeClient(t))
	if err != nil || groups["apps"] != "v1" {
		t.Fatalf("got %v %v", groups, err)
	}
}

func TestGetListReadsEveryPage(t *testing.T) {
	f := kubetest.New(t, nil)
	f.AnswerQuery = func(_, query string) (string, bool) {
		q, _ := url.ParseQuery(query)
		if q.Get("limit") != "500" || q.Get("labelSelector") != "app=x" {
			t.Errorf("query %q", query)
		}

		if q.Get("continue") == "" {
			return `{"metadata":{"continue":"more"},"items":[{"name":"a"}]}`, true
		}

		return `{"items":[{"name":"b"}]}`, true
	}

	var list kube.List[listItem]
	if err := kube.GetList(context.Background(), f.KubeClient(t), "/api/v1/pods?labelSelector=app%3Dx", &list); err != nil {
		t.Fatal(err)
	}

	if len(list.Items) != 2 || list.Items[0].Name != "a" || list.Items[1].Name != "b" {
		t.Fatalf("items %+v", list.Items)
	}
}

func TestGetListStartsAgainWhenExpired(t *testing.T) {
	var calls atomic.Int32

	f := kubetest.New(t, map[string]string{"GET /api/v1/pods": `{"kind":"Status","code":410,"reason":"Expired","message":"too old"}`})
	f.AnswerWith("GET /api/v1/pods", http.StatusGone, `{"kind":"Status","code":410,"reason":"Expired","message":"too old"}`)
	f.AnswerQuery = func(_, query string) (string, bool) {
		if calls.Add(1) == 1 {
			return `{"metadata":{"continue":"more"},"items":[{"name":"dropped"}]}`, true
		}

		if strings.Contains(query, "continue=") {
			return "", false
		}

		return `{"items":[{"name":"kept"}]}`, true
	}

	var list kube.List[listItem]
	if err := kube.GetList(context.Background(), f.KubeClient(t), "/api/v1/pods", &list); err != nil {
		t.Fatal(err)
	}

	if len(list.Items) != 1 || list.Items[0].Name != "kept" {
		t.Fatalf("items %+v", list.Items)
	}
}

func TestGetListRefusesOtherParameters(t *testing.T) {
	f := kubetest.New(t, nil)

	var list kube.List[listItem]
	if err := kube.GetList(context.Background(), f.KubeClient(t), "/api/v1/pods?watch=1", &list); err == nil {
		t.Fatal("a watch parameter was accepted")
	}
}

func TestConditionsAndHealth(t *testing.T) {
	c := kube.Conditions{{Type: "Ready", Status: "True"}}
	if !c.Is("Ready") || c.Is("Stalled") || c.Get("Stalled").Status != "" {
		t.Fatalf("conditions %+v", c)
	}

	if kube.HealthRank(kube.HealthCritical) >= kube.HealthRank(kube.HealthOK) || kube.UnixMilli("nope") != 0 || kube.UnixMilli("1970-01-01T00:00:01Z") != 1000 {
		t.Fatal("health or time")
	}
}
