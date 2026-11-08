package kube

// Pod is a pod as the apps show it.
type Pod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Status is what `kubectl get pods` shows: Running, Pending, CrashLoopBackOff,
	// Init:Error, Terminating, Completed...
	Status     string   `json:"status"`
	Healthy    bool     `json:"healthy"` // running with every container ready, or completed
	Ready      int      `json:"ready"`
	Containers int      `json:"containers"`
	Restarts   int      `json:"restarts"`
	Node       string   `json:"node"`
	Owner      string   `json:"owner"` // "ReplicaSet/web-5d8f", "" when none
	Created    int64    `json:"created"`
	Images     []string `json:"images"`
	// ContainerNames are the pod's containers, to pick one for its logs.
	ContainerNames []string `json:"containerNames"`
	// LastTermination is why a restarted container last stopped ("OOMKilled (exit 137)"),
	// "" when none did: what the previous run's log explains.
	LastTermination string `json:"lastTermination"`
	// Waiting is the kubelet's message for the container whose waiting reason is the pod's
	// status ("" when none); Pull says why its image cannot be pulled, nil when it can.
	Waiting string       `json:"waiting,omitempty"`
	Pull    *PullProblem `json:"pull,omitempty"`
}

// PullProblem says why an image cannot be pulled.
type PullProblem struct {
	Kind     string   `json:"kind"`
	Image    string   `json:"image"`
	Registry string   `json:"registry"`
	Secrets  []string `json:"secrets"`
}
