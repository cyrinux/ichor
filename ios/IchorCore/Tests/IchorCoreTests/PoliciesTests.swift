import XCTest
@testable import IchorCore

final class PoliciesTests: XCTestCase {
    // KubePolicies' demo answer (go/ichorgo/kube_policies_demo.go).
    private let json = #"""
    {
      "installed": true,
      "kyverno": true,
      "gatekeeper": true,
      "version": "v1",
      "engines": [
        {
          "name": "kyverno",
          "namespace": "kyverno",
          "controllers": [
            {
              "name": "admission-controller",
              "ready": 1,
              "desired": 1
            },
            {
              "name": "background-controller",
              "ready": 1,
              "desired": 1
            },
            {
              "name": "cleanup-controller",
              "ready": 1,
              "desired": 1
            },
            {
              "name": "reports-controller",
              "ready": 1,
              "desired": 1
            }
          ],
          "webhook": {
            "name": "kyverno-resource-validating-webhook-cfg",
            "present": true,
            "failurePolicy": "Fail"
          }
        },
        {
          "name": "gatekeeper",
          "namespace": "gatekeeper-system",
          "controllers": [
            {
              "name": "gatekeeper-audit",
              "ready": 1,
              "desired": 1
            },
            {
              "name": "gatekeeper-controller-manager",
              "ready": 3,
              "desired": 3
            }
          ],
          "webhook": {
            "name": "gatekeeper-validating-webhook-configuration",
            "present": true,
            "failurePolicy": "Ignore"
          }
        }
      ],
      "policies": [
        {
          "namespace": "",
          "name": "verify-image-signatures",
          "action": "Enforce",
          "background": false,
          "ready": false,
          "readyReason": "WebhookNotConfigured",
          "readyMessage": "the validating webhook configuration is missing: policies are not applied",
          "category": "Software Supply Chain",
          "severity": "high",
          "description": "Signatures on new images must be verified before a Pod is admitted",
          "rules": {
            "validate": 1,
            "mutate": 0,
            "generate": 0,
            "verifyImages": 1
          },
          "ruleNames": [
            "verify-signature"
          ],
          "pass": 0,
          "fail": 0,
          "warn": 0,
          "error": 0,
          "skip": 0,
          "failing": null,
          "engine": "kyverno",
          "byNamespace": null
        },
        {
          "namespace": "",
          "name": "must-have-team",
          "action": "Enforce",
          "background": true,
          "ready": true,
          "description": "Requires resources to contain specified labels",
          "rules": {
            "validate": 0,
            "mutate": 0,
            "generate": 0,
            "verifyImages": 0
          },
          "ruleNames": [],
          "pass": 0,
          "fail": 3,
          "warn": 0,
          "error": 0,
          "skip": 0,
          "failing": [
            {
              "kind": "Namespace",
              "name": "sandbox",
              "message": "you must provide labels: {\"team\"}",
              "at": 1760000060
            },
            {
              "kind": "Deployment",
              "namespace": "shop",
              "name": "cart",
              "message": "you must provide labels: {\"team\"}",
              "at": 1760000060
            },
            {
              "kind": "Deployment",
              "namespace": "shop",
              "name": "search",
              "message": "you must provide labels: {\"team\"}",
              "at": 1760000060
            }
          ],
          "engine": "gatekeeper",
          "kind": "K8sRequiredLabels",
          "auditAt": 1760000060,
          "byNamespace": [
            {
              "namespace": "shop",
              "pass": 0,
              "fail": 2,
              "warn": 0,
              "error": 0
            }
          ]
        },
        {
          "namespace": "",
          "name": "disallow-privileged",
          "action": "Enforce",
          "background": false,
          "ready": true,
          "category": "Pod Security",
          "severity": "high",
          "description": "Privileged containers are not allowed",
          "rules": {
            "validate": 1,
            "mutate": 0,
            "generate": 0,
            "verifyImages": 0
          },
          "ruleNames": [
            "privileged-containers"
          ],
          "pass": 15,
          "fail": 0,
          "warn": 0,
          "error": 0,
          "skip": 0,
          "failing": null,
          "engine": "kyverno",
          "byNamespace": [
            {
              "namespace": "billing",
              "pass": 9,
              "fail": 0,
              "warn": 0,
              "error": 0
            },
            {
              "namespace": "media",
              "pass": 3,
              "fail": 0,
              "warn": 0,
              "error": 0
            },
            {
              "namespace": "shop",
              "pass": 1,
              "fail": 0,
              "warn": 0,
              "error": 0
            },
            {
              "namespace": "vault",
              "pass": 2,
              "fail": 0,
              "warn": 0,
              "error": 0
            }
          ]
        },
        {
          "namespace": "shop",
          "name": "restrict-image-registries",
          "action": "Mixed",
          "background": true,
          "ready": true,
          "category": "Best Practices",
          "severity": "medium",
          "description": "Images must come from the registry of this cluster",
          "rules": {
            "validate": 3,
            "mutate": 0,
            "generate": 0,
            "verifyImages": 0
          },
          "ruleNames": [
            "check-registry",
            "check-latest",
            "check-digest"
          ],
          "pass": 3,
          "fail": 0,
          "warn": 2,
          "error": 0,
          "skip": 0,
          "failing": null,
          "engine": "kyverno",
          "byNamespace": [
            {
              "namespace": "shop",
              "pass": 3,
              "fail": 0,
              "warn": 2,
              "error": 0
            }
          ]
        },
        {
          "namespace": "",
          "name": "require-labels",
          "action": "Audit",
          "background": false,
          "ready": true,
          "category": "Best Practices",
          "severity": "medium",
          "description": "Every workload must carry its team and owner labels",
          "rules": {
            "validate": 2,
            "mutate": 0,
            "generate": 0,
            "verifyImages": 0
          },
          "ruleNames": [
            "require-team",
            "require-owner"
          ],
          "pass": 0,
          "fail": 7,
          "warn": 0,
          "error": 0,
          "skip": 0,
          "failing": [
            {
              "kind": "Pod",
              "namespace": "shop",
              "name": "web-0",
              "rule": "require-team",
              "message": "Validation error: missing label team",
              "severity": "medium",
              "at": 1760000002
            },
            {
              "kind": "Pod",
              "namespace": "shop",
              "name": "web-1",
              "rule": "require-team",
              "message": "Validation error: missing label team",
              "severity": "medium",
              "at": 1760000001
            },
            {
              "kind": "Pod",
              "namespace": "shop",
              "name": "api-0",
              "rule": "require-owner",
              "message": "Validation error: missing label owner",
              "severity": "medium",
              "at": 1760000000
            }
          ],
          "engine": "kyverno",
          "byNamespace": [
            {
              "namespace": "shop",
              "pass": 0,
              "fail": 7,
              "warn": 0,
              "error": 0
            }
          ]
        },
        {
          "namespace": "",
          "name": "psp-privileged-container",
          "action": "Audit",
          "background": true,
          "ready": true,
          "description": "Controls the ability of any container to enable privileged mode",
          "rules": {
            "validate": 0,
            "mutate": 0,
            "generate": 0,
            "verifyImages": 0
          },
          "ruleNames": [],
          "pass": 0,
          "fail": 1,
          "warn": 0,
          "error": 0,
          "skip": 0,
          "failing": [
            {
              "kind": "Pod",
              "namespace": "billing",
              "name": "ledger-backup-29318400-x7k2p",
              "message": "Privileged container is not allowed: backup",
              "at": 1760000060
            }
          ],
          "engine": "gatekeeper",
          "kind": "K8sPSPPrivilegedContainer",
          "auditAt": 1760000060,
          "byNamespace": [
            {
              "namespace": "billing",
              "pass": 0,
              "fail": 1,
              "warn": 0,
              "error": 0
            }
          ]
        }
      ],
      "namespaces": [
        {
          "namespace": "shop",
          "pass": 4,
          "fail": 9,
          "warn": 2,
          "error": 0
        },
        {
          "namespace": "billing",
          "pass": 9,
          "fail": 1,
          "warn": 0,
          "error": 0
        },
        {
          "namespace": "media",
          "pass": 3,
          "fail": 0,
          "warn": 0,
          "error": 0
        },
        {
          "namespace": "vault",
          "pass": 2,
          "fail": 0,
          "warn": 0,
          "error": 0
        }
      ],
      "reportsRead": 6,
      "reportsTruncated": false
    }
    """#

    private func decode(_ text: String) throws -> PolicyReport {
        try JSONDecoder().decode(PolicyReport.self, from: Data(text.utf8))
    }

    func testDecodesTheDemoInTheReadersOrder() throws {
        let report = try decode(json)
        XCTAssertTrue(report.installed)
        XCTAssertTrue(report.bothEngines)
        XCTAssertEqual(report.engines.map(\.name), [.kyverno, .gatekeeper])
        let kyverno = try XCTUnwrap(report.engines.first)
        XCTAssertEqual(kyverno.namespace, "kyverno")
        XCTAssertEqual(kyverno.controllers.count, 4)
        XCTAssertTrue(kyverno.controllers.allSatisfy(\.healthy))
        XCTAssertEqual(kyverno.webhook.failurePolicy, .fail)
        XCTAssertEqual(report.engines.last?.webhook.failurePolicy, .ignore)
        XCTAssertEqual(report.policies.map(\.name), [
            "verify-image-signatures", "must-have-team", "disallow-privileged", "restrict-image-registries",
            "require-labels", "psp-privileged-container",
        ])
        let team = try XCTUnwrap(report.policies.first { $0.name == "must-have-team" })
        XCTAssertEqual(team.engine, .gatekeeper)
        XCTAssertEqual(team.kind, "K8sRequiredLabels")
        XCTAssertEqual(team.action, .enforce)
        XCTAssertEqual(team.auditAt, 1760000060)
        XCTAssertEqual(team.failing.count, 3)
        XCTAssertFalse(team.failingCapped)
        XCTAssertEqual(report.policies.first { $0.name == "psp-privileged-container" }?.action, .audit)
        let notReady = try XCTUnwrap(report.policies.first)
        XCTAssertFalse(notReady.ready)
        XCTAssertEqual(notReady.readyReason, "WebhookNotConfigured")
        XCTAssertEqual(notReady.action, .enforce)
        XCTAssertEqual(notReady.rules.text, "validate 1  ·  verifyImages 1")
        // Go writes a nil slice as null: it reads as empty.
        XCTAssertTrue(notReady.failing.isEmpty)
        let labels = try XCTUnwrap(report.policies.first { $0.name == "require-labels" })
        XCTAssertEqual(labels.action, .audit)
        XCTAssertEqual(labels.fail, 7)
        XCTAssertEqual(labels.failing.count, 3)
        XCTAssertEqual(report.policies.first { $0.name == "restrict-image-registries" }?.action, .mixed)
        XCTAssertEqual(report.reportsRead, 6)
        XCTAssertFalse(report.reportsTruncated)
    }

    func testAClusterWithoutKyvernoIsNotAnError() throws {
        let report = try decode(#"{"installed":false,"engine":{"controllers":null,"webhook":{"present":false}},"policies":[],"namespaces":null}"#)
        XCTAssertFalse(report.installed)
        XCTAssertTrue(report.policies.isEmpty)
        XCTAssertEqual(report.error, "")
        XCTAssertTrue(report.engines.isEmpty)
    }

    func testAConstraintIsFoundByItsKindAndKeyedApartFromAPolicy() throws {
        let report = try decode(json)
        XCTAssertEqual(report.filtered(namespace: nil, query: "K8sRequiredLabels").map(\.name), ["must-have-team"])
        let twins = [
            KyvernoPolicy(name: "same"),
            KyvernoPolicy(name: "same", engine: .gatekeeper, kind: "K8sRequiredLabels"),
            KyvernoPolicy(name: "same", engine: .gatekeeper, kind: "K8sAllowedRepos"),
        ]
        XCTAssertEqual(Set(twins.map(\.id)).count, 3)
    }

    func testWarnAndCappedViolationsDecode() throws {
        let report = try decode(#"{"installed":true,"gatekeeper":true,"policies":[{"name":"w","engine":"gatekeeper","action":"Warn","fail":40,"failingCapped":true,"failing":[{"kind":"Pod","name":"p"}]}]}"#)
        let w = try XCTUnwrap(report.policies.first)
        XCTAssertEqual(w.action, .warn)
        XCTAssertEqual(w.engine, .gatekeeper)
        XCTAssertTrue(w.failingCapped)
        XCTAssertEqual(w.fail, 40)
        XCTAssertFalse(report.bothEngines)
    }

    func testForbiddenReportsKeepThePolicies() throws {
        let report = try decode(#"{"installed":true,"reportsError":"forbidden","policies":[{"name":"a","action":"Audit","ready":true,"error":2}]}"#)
        XCTAssertEqual(report.reportsError, "forbidden")
        XCTAssertEqual(report.policies.map(\.name), ["a"])
        XCTAssertEqual(report.policies.first?.errors, 2)
    }

    func testANamespaceKeepsItsOwnPoliciesAndTheClusterWideOnesThatCheckedIt() throws {
        let report = try decode(json)
        XCTAssertEqual(report.filtered(namespace: nil, query: ""), report.policies)
        // billing: require-labels has no result there; the not-ready one stays, and the constraint
        // with a violation there joins.
        XCTAssertEqual(report.filtered(namespace: "billing", query: "").map(\.name),
                       ["verify-image-signatures", "disallow-privileged", "psp-privileged-container"])
        XCTAssertEqual(report.filtered(namespace: "shop", query: "").map(\.name),
                       ["verify-image-signatures", "must-have-team", "disallow-privileged", "restrict-image-registries", "require-labels"])
        XCTAssertEqual(report.namespaceNames, ["billing", "media", "shop", "vault"])
    }

    func testAPickedNamespaceShowsThePolicysCountsThere() throws {
        let report = try decode(json)
        let labels = try XCTUnwrap(report.policies.first { $0.name == "require-labels" })
        XCTAssertEqual(labels.counts(namespace: nil), PolicyResultCounts(fail: 7))
        XCTAssertEqual(labels.counts(namespace: "shop"), PolicyResultCounts(fail: 7))
        XCTAssertEqual(labels.counts(namespace: "billing"), PolicyResultCounts())
        let privileged = try XCTUnwrap(report.policies.first { $0.name == "disallow-privileged" })
        XCTAssertEqual(privileged.counts(namespace: nil), PolicyResultCounts(pass: 15))
        XCTAssertEqual(privileged.counts(namespace: "billing"), PolicyResultCounts(pass: 9))
        XCTAssertTrue(privileged.checks("vault"))
        XCTAssertFalse(labels.checks("vault"))
    }

    func testSearchesNameCategorySeverityAndRules() throws {
        let report = try decode(json)
        XCTAssertEqual(report.filtered(namespace: nil, query: "supply").map(\.name), ["verify-image-signatures"])
        XCTAssertEqual(report.filtered(namespace: nil, query: "  REQUIRE-OWNER ").map(\.name), ["require-labels"])
        XCTAssertEqual(report.filtered(namespace: nil, query: "high").map(\.name), ["verify-image-signatures", "disallow-privileged"])
        XCTAssertTrue(report.filtered(namespace: "shop", query: "nothing like it").isEmpty)
    }

    func testAFailureOfAKnownKindOpensItsObject() {
        let pod = PolicyFailure(kind: "Pod", namespace: "shop", name: "web-0")
        XCTAssertEqual(pod.resource?.resource, "pods")
        XCTAssertEqual(pod.resource?.group, "")
        XCTAssertEqual(pod.objectNamespace, "shop")
        XCTAssertEqual(PolicyFailure(kind: "Deployment", namespace: "shop", name: "web").resource?.group, "apps")
        // A Namespace is cluster-scoped: no namespace for its object.
        XCTAssertEqual(PolicyFailure(kind: "Namespace", namespace: "shop", name: "shop").objectNamespace, "")
        XCTAssertNil(PolicyFailure(kind: "Widget", namespace: "shop", name: "w").resource)
        XCTAssertNil(PolicyFailure(kind: "Pod", namespace: "shop").resource)
    }
}
