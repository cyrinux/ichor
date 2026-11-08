package ichorgo

import (
	"regexp"
	"strings"
)

// What an admission policy denial says. A ReplicaSet whose pods a policy refuses gets
// Warning events `FailedCreate` whose message buries the policy name:
//
//	admission webhook "validate.kyverno.svc-fail" denied the request: resource Pod/shop/web-1
//	was blocked due to the following policies require-labels: autogen-check-for-labels: validation error: ...
//
// and Gatekeeper's:
//
//	admission webhook "validation.gatekeeper.sh" denied the request: [require-labels] Container "web" has no label app.
//
// The checkup turns them into policyBlocks findings and KubeEvents adds a policy field, both
// from this one parser.

// policyDenialSource names the engine whose webhook denied the request, "" when unknown.
const (
	policySourceKyverno    = "kyverno"
	policySourceGatekeeper = "gatekeeper"
	policyDenialMessageMax = 300
)

// policyDenial is one policy named by a denial event: the engine, the policy ("" when the
// message did not name one), the rule or constraint, and the policy's own message.
type policyDenial struct {
	Source  string `json:"source"`
	Policy  string `json:"policy"`
	Rule    string `json:"rule,omitempty"`
	Message string `json:"message,omitempty"`
}

// kyverno denial: "… was blocked due to the following policies <p1>, <p2>: <rule>: <message>".
var kyvernoDenialPattern = regexp.MustCompile(`was blocked due to the following policies (.+?): (.*)`)

// gatekeeper denial: "[<constraint>] <message>" right after "denied the request: ".
var gatekeeperDenialPattern = regexp.MustCompile(`\[([^\]]+)\] (.*)`)

// isPolicyDenial tells whether an event message is an admission webhook denial at all.
func isPolicyDenial(message string) bool {
	return strings.Contains(message, `admission webhook "`) && strings.Contains(message, "denied the request")
}

// parsePolicyDenials reads the policies one denial event names, one per policy when Kyverno
// blocked the request for several. Empty when the message is not a denial; a denial whose
// policy name cannot be read still comes back, with Policy "" and the webhook's message.
func parsePolicyDenials(message string) []policyDenial {
	if !isPolicyDenial(message) {
		return nil
	}

	_, after, ok := strings.Cut(message, "denied the request: ")
	if !ok {
		return nil
	}

	after = strings.TrimSpace(after)

	switch {
	case strings.Contains(message, "was blocked due to the following policies"):
		m := kyvernoDenialPattern.FindStringSubmatch(message)
		if m == nil {
			return []policyDenial{{Source: policySourceKyverno, Message: clipUTF8(after, policyDenialMessageMax)}}
		}

		// "<rule>: <message>" follows the policy list; the message may hold colons itself.
		rule, msg, _ := strings.Cut(strings.TrimSpace(m[2]), ": ")

		var out []policyDenial
		for _, name := range strings.Split(m[1], ", ") {
			out = append(out, policyDenial{
				Source:  policySourceKyverno,
				Policy:  strings.TrimSpace(name),
				Rule:    rule,
				Message: clipUTF8(strings.TrimSpace(msg), policyDenialMessageMax),
			})
		}

		return out

	case strings.Contains(message, "gatekeeper.sh"):
		m := gatekeeperDenialPattern.FindStringSubmatch(after)
		if m == nil {
			return []policyDenial{{Source: policySourceGatekeeper, Message: clipUTF8(after, policyDenialMessageMax)}}
		}

		return []policyDenial{{
			Source:  policySourceGatekeeper,
			Policy:  strings.TrimSpace(m[1]),
			Message: clipUTF8(strings.TrimSpace(m[2]), policyDenialMessageMax),
		}}

	default:
		// Another engine's wording: keep the finding, the policy name is just unknown.
		return []policyDenial{{Message: clipUTF8(after, policyDenialMessageMax)}}
	}
}
