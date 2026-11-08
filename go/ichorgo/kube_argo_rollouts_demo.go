package ichorgo

import "time"

// demoArgoRollouts are the demo cluster's Argo Rollouts, in every state the screen shows: a
// canary paused at its second step, a blue-green awaiting promotion, an aborted canary and a
// healthy one.
func demoArgoRollouts() argoRolloutsStatus {
	now := time.Now()
	ago := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

	canarySteps := []argoRolloutStep{
		{Kind: "setWeight", Label: "20%"}, {Kind: "pause", Label: "∞"}, {Kind: "setWeight", Label: "50%"},
		{Kind: "analysis", Label: "success-rate"}, {Kind: "setWeight", Label: "100%"},
	}

	rollout := func(name, strategy, phase string) argoRollout {
		return argoRollout{
			Namespace: "demo", Name: name, Strategy: strategy, Phase: phase, Step: -1, Steps: []argoRolloutStep{},
			PauseReasons: []string{}, AutoPromotion: true, Replicas: 4, Updated: 4, Ready: 4, Available: 4,
			StableHash: "6c9d8f7b5", CurrentHash: "6c9d8f7b5", Images: []string{"ghcr.io/example-org/" + name + ":1.8.0"},
			UpdatedAt: ago(2 * time.Hour),
		}
	}

	shop := rollout("shop", argoRolloutCanary, "Paused")
	shop.Message = "CanaryPauseStep"
	shop.Paused, shop.PauseReasons, shop.PausedSince = true, []string{"CanaryPauseStep"}, ago(12*time.Minute)
	shop.Step, shop.Steps, shop.Weight = 1, canarySteps, 20
	shop.CurrentHash, shop.Updated = "7f4b2c9d1", 1
	shop.Images = []string{"ghcr.io/example-org/shop:1.9.0"}

	api := rollout("api", argoRolloutBlueGreen, "Paused")
	api.Message = "BlueGreenPause"
	api.Paused, api.PauseReasons, api.PausedSince, api.AutoPromotion = true, []string{"BlueGreenPause"}, ago(25*time.Minute), false
	api.CurrentHash, api.Updated = "5a1e9b3c7", 4
	api.Images = []string{"ghcr.io/example-org/api:2.4.0"}
	api.Analysis = &argoRolloutAnalysis{Name: "api-5a1e9b3c7-pre", Status: "Successful", Message: "Metric \"error-rate\" assessed Successful"}

	checkout := rollout("checkout", argoRolloutCanary, "Degraded")
	checkout.Message = "RolloutAborted: Rollout aborted update to revision 7: Metric \"success-rate\" assessed Failed due to failed (3) > failureLimit (2)"
	checkout.Aborted, checkout.Step, checkout.Steps, checkout.Weight = true, 0, canarySteps, 0
	checkout.CurrentHash, checkout.Updated = "9d2f6a4e8", 0
	checkout.Analysis = &argoRolloutAnalysis{Name: "checkout-9d2f6a4e8-3", Status: "Failed", Message: "Metric \"success-rate\" assessed Failed due to failed (3) > failureLimit (2)"}

	web := rollout("web", argoRolloutCanary, "Healthy")
	web.Step, web.Steps, web.Weight = len(canarySteps), canarySteps, 100

	out := argoRolloutsStatus{Installed: true, Rollouts: []argoRollout{shop, api, checkout, web}}
	for i := range out.Rollouts {
		out.Rollouts[i].Level = argoRolloutLevel(out.Rollouts[i])
	}

	sortByLevel(out.Rollouts, func(r argoRollout) (string, string) { return r.Level, r.Namespace + "/" + r.Name })

	return out
}
