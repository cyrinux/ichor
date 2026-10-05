package ichorgo

import (
	"fmt"
	"time"
)

// demoCertDetails explains the demo certificates (see demoDataServices): shop-tls stuck on an
// HTTP-01 challenge, legacy-tls waiting for its missing CA, the others issued.
func demoCertDetails(namespace, name string, now time.Time) certDetails {
	ago := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	stamp := func(d time.Duration) string { return now.Add(-d).UTC().Format(time.RFC3339) }

	out := certDetails{Requests: []certRequestDetail{}, Events: []certEvent{}, Log: []string{}}

	switch name {
	case "shop-tls":
		req, order := name+"-1", name+"-1-2468"
		challenge := order + "-1357"
		reason := `Waiting for HTTP-01 challenge propagation: wrong status code '404', expected '200'`

		out.Conditions = []certCondition{
			{Type: "Ready", Status: "False", Reason: "Failed", Message: `The certificate request has failed to complete and will be retried: Failed to wait for order resource "` + order + `" to become ready`, Time: ago(19 * 24 * time.Hour)},
			{Type: "Issuing", Status: "False", Reason: "Failed", Message: "The certificate request has failed to complete and will be retried", Time: ago(2 * time.Hour)},
		}
		out.Requests = []certRequestDetail{{
			Name: req, Created: ago(3 * time.Hour),
			Conditions: []certCondition{
				{Type: "Approved", Status: "True", Reason: "cert-manager.io", Message: "Certificate request has been approved by cert-manager.io", Time: ago(3 * time.Hour)},
				{Type: "Ready", Status: "False", Reason: "Pending", Message: `Waiting on certificate issuance from order ` + namespace + `/` + order + `: "pending"`, Time: ago(3 * time.Hour)},
			},
			Orders: []acmeOrderDetail{{Name: order, State: "pending", Challenges: []acmeChallengeDetail{
				{Name: challenge, Type: "HTTP-01", DNSName: "shop.example.com", State: "pending", Reason: reason, Presented: true},
				{Name: challenge + "-www", Type: "HTTP-01", DNSName: "www.shop.example.com", State: "valid", Presented: false},
			}}},
		}}
		out.Events = []certEvent{
			{Time: ago(4 * time.Minute), Type: "Warning", Reason: "PresentError", Message: reason, Object: "Challenge/" + challenge, Count: 37},
			{Time: ago(3 * time.Hour), Type: "Normal", Reason: "Created", Message: "Created Challenge resource \"" + challenge + "\" for domain \"shop.example.com\"", Object: "Order/" + order, Count: 1},
			{Time: ago(3 * time.Hour), Type: "Normal", Reason: "OrderCreated", Message: "Created Order resource " + namespace + "/" + order, Object: "CertificateRequest/" + req, Count: 1},
			{Time: ago(3 * time.Hour), Type: "Normal", Reason: "Requested", Message: "Created new CertificateRequest resource \"" + req + "\"", Object: "Certificate/" + name, Count: 1},
		}
		for _, d := range []time.Duration{14 * time.Minute, 9 * time.Minute, 4 * time.Minute} {
			out.Log = append(out.Log, fmt.Sprintf(`E%s 1 sync.go:190] "propagation check failed" err="%s" logger="cert-manager.controller" resource_name="%s" resource_namespace="%s" resource_kind="Challenge" dnsName="shop.example.com" type="HTTP-01"`,
				now.Add(-d).UTC().Format("0102 15:04:05.000000"), reason, challenge, namespace))
		}
	case "legacy-tls":
		req := name + "-7"
		message := `Referenced "Issuer" not found: issuer.cert-manager.io "internal-ca" not found`

		out.Conditions = []certCondition{
			{Type: "Ready", Status: "False", Reason: "Expired", Message: "Certificate expired on " + stamp(2*24*time.Hour), Time: ago(2 * 24 * time.Hour)},
			{Type: "Issuing", Status: "True", Reason: "Expired", Message: "Issuing certificate as Secret was previously issued by Issuer.cert-manager.io/internal-ca", Time: ago(2 * 24 * time.Hour)},
		}
		out.Requests = []certRequestDetail{{
			Name: req, Created: ago(6 * time.Hour), Orders: []acmeOrderDetail{},
			Conditions: []certCondition{
				{Type: "Approved", Status: "True", Reason: "cert-manager.io", Message: "Certificate request has been approved by cert-manager.io", Time: ago(6 * time.Hour)},
				{Type: "Ready", Status: "False", Reason: "Pending", Message: message, Time: ago(6 * time.Hour)},
			},
		}}
		out.Events = []certEvent{
			{Time: ago(10 * time.Minute), Type: "Warning", Reason: "IssuerNotFound", Message: message, Object: "CertificateRequest/" + req, Count: 72},
		}
	default:
		out.Conditions = []certCondition{
			{Type: "Ready", Status: "True", Reason: "Ready", Message: "Certificate is up to date and has not expired", Time: ago(27 * 24 * time.Hour)},
		}
		out.Requests = []certRequestDetail{{
			Name: name + "-4", Created: ago(27 * 24 * time.Hour), Orders: []acmeOrderDetail{},
			Conditions: []certCondition{
				{Type: "Approved", Status: "True", Reason: "cert-manager.io", Message: "Certificate request has been approved by cert-manager.io", Time: ago(27 * 24 * time.Hour)},
				{Type: "Ready", Status: "True", Reason: "Issued", Message: "Certificate fetched from issuer successfully", Time: ago(27 * 24 * time.Hour)},
			},
		}}
	}

	return out
}
