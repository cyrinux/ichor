package ichorgo

import (
	_ "embed"
	"sync"
)

// The demo cluster's schema help: a trimmed OpenAPI v3 document for its Deployments, Pods
// and ConfigMaps (the fields an edit is likely to touch). Other kinds answer that help is
// unavailable.

//go:embed kube_explain_demo.json
var demoExplainJSON []byte

var demoExplainDoc = sync.OnceValues(func() (*explainDoc, error) {
	return decodeExplainDoc(demoExplainJSON)
})
