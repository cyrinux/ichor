package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Argo Rollouts (argoproj.io Rollout objects, the group Argo CD shares): canary and
// blue-green rollouts, read like the kubectl plugin's `get rollout`, without the plugin.

// The strategies of a Rollout.
const (
	argoRolloutCanary    = "canary"
	argoRolloutBlueGreen = "blueGreen"
)

type argoRolloutsStatus struct {
	// Installed: the cluster serves Rollouts.
	Installed bool          `json:"installed"`
	Rollouts  []argoRollout `json:"rollouts"`
}

type argoRollout struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Strategy  string `json:"strategy"` // canary|blueGreen
	// Phase is the controller's own word: Healthy, Progressing, Paused, Degraded…
	Phase   string `json:"phase"`
	Message string `json:"message"`
	// Paused: by spec.paused or a pause condition (a pause step, a blue-green awaiting
	// promotion); PauseReasons are the conditions' reasons, PausedSince the first one's start.
	Paused       bool     `json:"paused"`
	PauseReasons []string `json:"pauseReasons"`
	PausedSince  int64    `json:"pausedSince,omitempty"`
	// Step is the canary's current step index, -1 when not started (or a blue-green).
	Step  int               `json:"step"`
	Steps []argoRolloutStep `json:"steps"`
	// Weight is the canary's share of the traffic, in percent.
	Weight      int    `json:"weight"`
	Aborted     bool   `json:"aborted"`
	PromoteFull bool   `json:"promoteFull"`
	StableHash  string `json:"stableHash"`
	CurrentHash string `json:"currentHash"`
	Replicas    int    `json:"replicas"`
	Updated     int    `json:"updated"`
	Ready       int    `json:"ready"`
	Available   int    `json:"available"`
	// Analysis is the running or last analysis of the current step, the background one,
	// or a blue-green's pre/post-promotion one; nil when none.
	Analysis *argoRolloutAnalysis `json:"analysis,omitempty"`
	// AutoPromotion: a blue-green promotes by itself (spec.strategy.blueGreen.autoPromotionEnabled).
	AutoPromotion bool     `json:"autoPromotion"`
	WorkloadRef   string   `json:"workloadRef"` // "Deployment/name" when the pods come from one
	Images        []string `json:"images"`
	Level         string   `json:"level"`
	UpdatedAt     int64    `json:"updatedAt,omitempty"`
}

type argoRolloutStep struct {
	// Kind: setWeight, pause, analysis, experiment, setCanaryScale, setHeaderRoute,
	// setMirrorRoute, plugin or other.
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type argoRolloutAnalysis struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // Running, Successful, Failed, Error, Inconclusive…
	Message string `json:"message"`
}

type argoAnalysisRunStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// argoRolloutObject is the part of a Rollout read here and by the actions.
type argoRolloutObject struct {
	Metadata struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		ResourceVersion string `json:"resourceVersion"`
	} `json:"metadata"`
	Spec struct {
		Paused      bool `json:"paused"`
		WorkloadRef *struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"workloadRef"`
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
		Strategy struct {
			Canary *struct {
				Steps []map[string]json.RawMessage `json:"steps"`
			} `json:"canary"`
			BlueGreen *struct {
				AutoPromotionEnabled *bool `json:"autoPromotionEnabled"`
			} `json:"blueGreen"`
		} `json:"strategy"`
	} `json:"spec"`
	Status struct {
		Phase           string `json:"phase"`
		Message         string `json:"message"`
		PauseConditions []struct {
			Reason    string `json:"reason"`
			StartTime string `json:"startTime"`
		} `json:"pauseConditions"`
		ControllerPause   bool   `json:"controllerPause"`
		CurrentStepIndex  *int   `json:"currentStepIndex"`
		Abort             bool   `json:"abort"`
		PromoteFull       bool   `json:"promoteFull"`
		StableRS          string `json:"stableRS"`
		CurrentPodHash    string `json:"currentPodHash"`
		Replicas          int    `json:"replicas"`
		UpdatedReplicas   int    `json:"updatedReplicas"`
		ReadyReplicas     int    `json:"readyReplicas"`
		AvailableReplicas int    `json:"availableReplicas"`
		Canary            struct {
			Weights *struct {
				Canary struct {
					Weight int `json:"weight"`
				} `json:"canary"`
			} `json:"weights"`
			CurrentStepAnalysisRunStatus       *argoAnalysisRunStatus `json:"currentStepAnalysisRunStatus"`
			CurrentBackgroundAnalysisRunStatus *argoAnalysisRunStatus `json:"currentBackgroundAnalysisRunStatus"`
		} `json:"canary"`
		BlueGreen struct {
			PrePromotionAnalysisRunStatus  *argoAnalysisRunStatus `json:"prePromotionAnalysisRunStatus"`
			PostPromotionAnalysisRunStatus *argoAnalysisRunStatus `json:"postPromotionAnalysisRunStatus"`
		} `json:"blueGreen"`
		Conditions []struct {
			Type           string `json:"type"`
			LastUpdateTime string `json:"lastUpdateTime"`
		} `json:"conditions"`
	} `json:"status"`
}

// KubeArgoRollouts lists the Argo Rollouts of every namespace, worst first:
// {"installed","rollouts":[…]}; installed false (no error) when the cluster serves none.
// kubeServer: see KubePods.
func KubeArgoRollouts(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoArgoRollouts, readArgoRollouts)
}

func readArgoRollouts(ctx context.Context, k *kubeClient) (argoRolloutsStatus, error) {
	out := argoRolloutsStatus{Rollouts: []argoRollout{}}

	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return out, err
	}

	version := groups[groupArgo]
	if version == "" {
		return out, nil
	}

	var list kubeList[argoRolloutObject]
	if err := getList(ctx, k, "/apis/"+groupArgo+"/"+version+"/rollouts", &list); err != nil {
		// Argo CD without Rollouts: the group is served, the kind is not.
		return out, ignoreNotFound(err)
	}

	out.Installed = true
	for _, o := range list.Items {
		out.Rollouts = append(out.Rollouts, mapArgoRollout(o))
	}

	sortByLevel(out.Rollouts, func(r argoRollout) (string, string) { return r.Level, r.Namespace + "/" + r.Name })

	return out, nil
}

func mapArgoRollout(o argoRolloutObject) argoRollout {
	st := o.Status
	r := argoRollout{
		Namespace: o.Metadata.Namespace, Name: o.Metadata.Name, Strategy: argoRolloutCanary,
		Phase: st.Phase, Message: st.Message, Step: -1, Steps: []argoRolloutStep{}, PauseReasons: []string{},
		Aborted: st.Abort, PromoteFull: st.PromoteFull, StableHash: st.StableRS, CurrentHash: st.CurrentPodHash,
		Replicas: st.Replicas, Updated: st.UpdatedReplicas, Ready: st.ReadyReplicas, Available: st.AvailableReplicas,
		AutoPromotion: true, Images: []string{},
	}

	if bg := o.Spec.Strategy.BlueGreen; bg != nil {
		r.Strategy = argoRolloutBlueGreen
		if bg.AutoPromotionEnabled != nil {
			r.AutoPromotion = *bg.AutoPromotionEnabled
		}
	}

	if c := o.Spec.Strategy.Canary; c != nil {
		for _, s := range c.Steps {
			r.Steps = append(r.Steps, argoRolloutStepOf(s))
		}
	}

	if st.CurrentStepIndex != nil {
		r.Step = *st.CurrentStepIndex
	}

	r.Paused = o.Spec.Paused || len(st.PauseConditions) > 0
	for _, c := range st.PauseConditions {
		r.PauseReasons = append(r.PauseReasons, c.Reason)
	}

	if len(st.PauseConditions) > 0 {
		r.PausedSince = unixMilli(st.PauseConditions[0].StartTime)
	}

	r.Weight = argoRolloutWeight(o, r)

	for _, a := range []*argoAnalysisRunStatus{
		st.Canary.CurrentStepAnalysisRunStatus, st.Canary.CurrentBackgroundAnalysisRunStatus,
		st.BlueGreen.PrePromotionAnalysisRunStatus, st.BlueGreen.PostPromotionAnalysisRunStatus,
	} {
		if a != nil && a.Name != "" {
			r.Analysis = &argoRolloutAnalysis{Name: a.Name, Status: a.Status, Message: a.Message}

			break
		}
	}

	if w := o.Spec.WorkloadRef; w != nil && w.Name != "" {
		r.WorkloadRef = w.Kind + "/" + w.Name
	}

	for _, c := range o.Spec.Template.Spec.Containers {
		r.Images = append(r.Images, c.Image)
	}

	for _, c := range st.Conditions {
		if c.Type == "Progressing" {
			r.UpdatedAt = unixMilli(c.LastUpdateTime)
		}
	}

	r.Level = argoRolloutLevel(r)

	return r
}

// argoRolloutWeight is the canary's traffic share: the controller's when it says it, else the
// last setWeight reached (100 once every step ran), else 0.
func argoRolloutWeight(o argoRolloutObject, r argoRollout) int {
	if w := o.Status.Canary.Weights; w != nil {
		return w.Canary.Weight
	}

	if r.Strategy != argoRolloutCanary || r.Step < 0 {
		return 0
	}

	if r.Step >= len(r.Steps) && len(r.Steps) > 0 {
		return 100
	}

	weight := 0

	for i := 0; i <= r.Step && i < len(o.Spec.Strategy.Canary.Steps); i++ {
		if raw, ok := o.Spec.Strategy.Canary.Steps[i]["setWeight"]; ok {
			_ = json.Unmarshal(raw, &weight) //nolint:errcheck // a malformed weight leaves the last one
		}
	}

	return weight
}

// argoRolloutLevel: aborted or Degraded is critical, paused or progressing a warning,
// Healthy ok, anything else idle. The phase is free text in the CRD.
func argoRolloutLevel(r argoRollout) string {
	switch {
	case r.Aborted || r.Phase == "Degraded":
		return healthCritical
	case r.Paused || r.Phase == "Paused" || r.Phase == "Progressing":
		return healthWarning
	case r.Phase == "Healthy":
		return healthOK
	}

	return healthIdle
}

// argoRolloutStepOf names a canary step by its one key, with what it does in a few characters.
func argoRolloutStepOf(s map[string]json.RawMessage) argoRolloutStep {
	for _, kind := range []string{"setWeight", "pause", "analysis", "experiment", "setCanaryScale", "setHeaderRoute", "setMirrorRoute", "plugin"} {
		raw, ok := s[kind]
		if !ok {
			continue
		}

		step := argoRolloutStep{Kind: kind}

		switch kind {
		case "setWeight":
			var w int
			if json.Unmarshal(raw, &w) == nil {
				step.Label = strconv.Itoa(w) + "%"
			}
		case "pause":
			step.Label = argoRolloutPauseLabel(raw)
		case "analysis":
			var a struct {
				Templates []struct {
					TemplateName string `json:"templateName"`
				} `json:"templates"`
			}

			if json.Unmarshal(raw, &a) == nil && len(a.Templates) > 0 {
				step.Label = a.Templates[0].TemplateName
			}
		}

		return step
	}

	return argoRolloutStep{Kind: "other"}
}

// argoRolloutPauseLabel is a pause step's duration ("1h", "30s"), "∞" for a pause until promoted.
func argoRolloutPauseLabel(raw json.RawMessage) string {
	var p struct {
		Duration json.RawMessage `json:"duration"`
	}

	if json.Unmarshal(raw, &p) != nil || len(p.Duration) == 0 || string(p.Duration) == "null" {
		return "∞"
	}

	var text string
	if json.Unmarshal(p.Duration, &text) == nil {
		return cmp.Or(strings.TrimSpace(text), "∞")
	}

	var seconds int
	if json.Unmarshal(p.Duration, &seconds) == nil {
		return fmt.Sprintf("%ds", seconds)
	}

	return "∞"
}
