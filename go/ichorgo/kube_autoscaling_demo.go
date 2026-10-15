package ichorgo

// demoAutoscaling is what the demo cluster answers: every group installed, each problem
// state once (an HPA at its max, one limited, a KEDA object paused, one on its fallback, a
// VPA without a recommendation yet, a pool at its CPU limit, a pending claim). Everything
// invented; nothing readable as a real cluster.
func demoAutoscaling() autoscalingReport {
	r := autoscalingReport{
		HPA: hpaGroup{
			autoscalingGroupState: autoscalingGroupState{Installed: true, Version: "v2"},
			Items:                 demoHPAs(),
		},
		KEDA: kedaGroup{
			autoscalingGroupState: autoscalingGroupState{Installed: true, Version: "v1alpha1"},
			ScaledObjects:         demoScaledObjects(),
			ScaledJobs: []scaledJobRow{
				{Namespace: "media", Name: "thumbnailer", Triggers: []kedaTrigger{{Type: "rabbitmq", Summary: "thumbnails"}}, MaxReplicaCount: 20, Ready: true, State: autoscaleOK},
			},
		},
		VPA: vpaGroup{
			autoscalingGroupState: autoscalingGroupState{Installed: true, Version: "v1"},
			Items:                 demoVPAs(),
		},
		Karpenter: karpenterGroup{
			autoscalingGroupState: autoscalingGroupState{Installed: true, Version: "v1"},
			NodePools:             demoNodePools(),
			NodeClaims:            demoNodeClaims(),
		},
	}
	r.Workloads = autoscaledWorkloadRows(r.HPA.Items, r.KEDA.ScaledObjects, r.VPA.Items)
	sortAutoscaling(&r)

	return r
}

func demoHPAs() []hpaRow {
	cpu := func(current, target string) []hpaMetric {
		return []hpaMetric{{Type: "Resource", Name: "cpu", Current: current, Target: target}}
	}

	return []hpaRow{
		{
			Namespace: "shop", Name: "web", Target: autoscaleTarget{Kind: "Deployment", Name: "web"},
			Min: 2, Max: 6, Current: 6, Desired: 6, Metrics: cpu("94%", "70%"), AtMax: true, LastScaleAt: 1760000000000,
			Conditions: hpaConditions{AbleToScale: true, ScalingActive: true, ScalingLimited: true, Message: "the desired replica count is more than the maximum replica count"},
			State:      autoscaleAtMax,
		},
		{
			Namespace: "shop", Name: "api", Target: autoscaleTarget{Kind: "Deployment", Name: "api"},
			Min: 3, Max: 10, Current: 3, Desired: 3, Metrics: cpu("12%", "60%"), LastScaleAt: 1759990000000,
			Conditions: hpaConditions{AbleToScale: true, ScalingActive: true, ScalingLimited: true, Message: "the desired replica count is less than the minimum replica count"},
			State:      autoscaleLimited,
		},
		{
			Namespace: "billing", Name: "invoices", Target: autoscaleTarget{Kind: "Deployment", Name: "invoices"},
			Min: 1, Max: 4, Current: 1, Desired: 1, Metrics: cpu("-", "75%"),
			Conditions: hpaConditions{AbleToScale: true, Message: "the HPA was unable to compute the replica count: unable to get metrics for resource cpu"},
			State:      autoscaleNotReady,
		},
		{
			Namespace: "media", Name: "keda-hpa-transcoder", Target: autoscaleTarget{Kind: "Deployment", Name: "transcoder"},
			Min: 1, Max: 8, Current: 4, Desired: 4,
			Metrics:    []hpaMetric{{Type: "External", Name: "s0-prometheus", Current: "41", Target: "10"}},
			Conditions: hpaConditions{AbleToScale: true, ScalingActive: true},
			ManagedBy:  autoscaledByKEDA, ScaledObject: "transcoder", State: autoscaleOK,
		},
	}
}

func demoScaledObjects() []scaledObjectRow {
	five := 5

	return []scaledObjectRow{
		{
			Namespace: "media", Name: "transcoder", Target: autoscaleTarget{Kind: "Deployment", Name: "transcoder"},
			Min: 1, Max: 8, Current: 4, Desired: 4,
			Triggers: []kedaTrigger{{Type: "prometheus", Summary: "sum(rate(jobs_queued_total[2m]))"}},
			Ready:    true, Active: true, HPAName: "keda-hpa-transcoder", State: autoscaleOK,
		},
		{
			Namespace: "media", Name: "uploader", Target: autoscaleTarget{Kind: "Deployment", Name: "uploader"},
			Min: 0, Max: 20, Triggers: []kedaTrigger{{Type: "kafka", Summary: "uploads"}},
			Paused: true, PausedReplicas: &five, Ready: true, State: autoscalePaused,
		},
		{
			Namespace: "shop", Name: "mailer", Target: autoscaleTarget{Kind: "Deployment", Name: "mailer"},
			Min: 1, Max: 5, Current: 2, Desired: 2, FallbackReplicas: 2,
			Triggers: []kedaTrigger{{Type: "redis", Summary: "outbox"}},
			Ready:    true, Fallback: true, Message: "scaler redis failed 3 times: falling back to 2 replicas",
			HPAName: "keda-hpa-mailer", State: autoscaleFallback,
		},
		{
			Namespace: "shop", Name: "office-hours", Target: autoscaleTarget{Kind: "Deployment", Name: "backoffice"},
			Min: 0, Max: 2, Triggers: []kedaTrigger{{Type: "cron", Summary: "0 8 * * 1-5 → 0 19 * * 1-5 Europe/Berlin"}},
			Message: "ScaledObject doesn't have correct scaleTargetRef specification", State: autoscaleNotReady,
		},
	}
}

func demoVPAs() []vpaRow {
	return []vpaRow{
		{
			Namespace: "billing", Name: "invoices", Target: autoscaleTarget{Kind: "Deployment", Name: "invoices"}, UpdateMode: "Off",
			Containers: []vpaContainer{{
				Name: "app", CPUTarget: "250m", MemoryTarget: "384Mi", CPULower: "120m", CPUUpper: "900m",
				MemoryLower: "256Mi", MemoryUpper: "1Gi", CPURequest: "100m", MemoryRequest: "256Mi",
			}},
			RecommendationProvided: true, State: autoscaleOK,
		},
		{
			Namespace: "vault", Name: "vault", Target: autoscaleTarget{Kind: "StatefulSet", Name: "vault"}, UpdateMode: "Initial",
			Containers: []vpaContainer{}, Message: "no pods match this VPA object", State: autoscaleNotReady,
		},
	}
}

func demoNodePools() []nodePoolRow {
	return []nodePoolRow{
		{
			Name: "general", Limits: nodePoolLimits{CPU: "64", Memory: "256Gi"},
			Resources: nodePoolResources{CPU: "64", Memory: "212Gi", Nodes: 8}, AtLimit: true, Weight: 10, Ready: true,
			CapacityTypes: []string{"spot", "on-demand"},
			Disruption: nodePoolDisruption{ConsolidationPolicy: "WhenEmptyOrUnderutilized", ConsolidateAfter: "1m", Budgets: []nodePoolBudget{
				{Nodes: "10%", Reasons: []string{}},
				{Nodes: "0", Schedule: "0 9 * * mon-fri", Duration: "8h", Reasons: []string{"Underutilized"}},
			}},
		},
		{
			Name: "gpu", Limits: nodePoolLimits{CPU: "32"}, Resources: nodePoolResources{CPU: "8", Memory: "60Gi", Nodes: 1}, Ready: true,
			CapacityTypes: []string{"on-demand"},
			Disruption:    nodePoolDisruption{ConsolidationPolicy: "WhenEmpty", ConsolidateAfter: "10m", Budgets: []nodePoolBudget{{Nodes: "1", Reasons: []string{}}}},
		},
	}
}

func demoNodeClaims() []nodeClaimRow {
	return []nodeClaimRow{
		{
			Name: "general-x7k2p", Pool: "general", InstanceType: "m7g.2xlarge", CapacityType: "spot", Launched: true,
			Message: "Node not registered with cluster", Created: 1760000000000, Pending: true,
		},
		{
			Name: "general-4mq9d", Pool: "general", NodeName: "node-10-0-12-7", InstanceType: "m7g.2xlarge", CapacityType: "spot",
			Launched: true, Registered: true, Initialized: true, Ready: true, Created: 1759900000000, Drifted: true,
		},
		{
			Name: "gpu-s2v8w", Pool: "gpu", NodeName: "node-10-0-40-3", InstanceType: "g6.2xlarge", CapacityType: "on-demand",
			Launched: true, Registered: true, Initialized: true, Ready: true, Created: 1759800000000,
		},
	}
}
