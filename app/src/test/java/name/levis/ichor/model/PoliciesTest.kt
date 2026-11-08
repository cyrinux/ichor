package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PoliciesTest {

    /** What go/ichorgo demoPolicies() answers, as KubePolicies writes it. */
    private val demo = TalosJson.decodeFromString(
        PolicyReport.serializer(),
        """
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
        """.trimIndent(),
    )

    @Test
    fun decodesTheDemoInTheReadersOrder() {
        assertTrue(demo.installed)
        assertTrue(demo.bothEngines)
        assertEquals(listOf(POLICY_ENGINE_KYVERNO, POLICY_ENGINE_GATEKEEPER), demo.engines.map { it.name })
        val kyverno = demo.engines.first()
        assertEquals("kyverno", kyverno.namespace)
        assertEquals(4, kyverno.controllers.size)
        assertTrue(kyverno.controllers.all { it.healthy })
        assertEquals(WEBHOOK_FAIL, kyverno.webhook.failurePolicy)
        assertEquals(WEBHOOK_IGNORE, demo.engines.last().webhook.failurePolicy)
        // Not ready first, then Enforce, Mixed, Audit; both engines in one list.
        assertEquals(
            listOf(
                "verify-image-signatures", "must-have-team", "disallow-privileged", "restrict-image-registries",
                "require-labels", "psp-privileged-container",
            ),
            demo.policies.map { it.name },
        )
        val team = demo.policies.single { it.name == "must-have-team" }
        assertTrue(team.isGatekeeper)
        assertEquals("K8sRequiredLabels", team.kind)
        assertEquals(POLICY_ACTION_ENFORCE, team.action)
        assertEquals(1760000060L, team.auditAt)
        assertEquals(3, team.failing.size)
        assertFalse(team.failingCapped)
        assertEquals(POLICY_ACTION_AUDIT, demo.policies.single { it.name == "psp-privileged-container" }.action)
        val notReady = demo.policies.first()
        assertFalse(notReady.ready)
        assertEquals("WebhookNotConfigured", notReady.readyReason)
        assertEquals(1, notReady.rules.verifyImages)
        // Go writes a nil slice as null: it reads as empty.
        assertTrue(notReady.failing.isEmpty())
        val labels = demo.policies.single { it.name == "require-labels" }
        assertEquals(7, labels.fail)
        assertEquals(3, labels.failing.size)
        assertEquals("shop", labels.failing.first().namespace)
        assertEquals(6, demo.reportsRead)
        assertFalse(demo.reportsTruncated)
    }

    @Test
    fun aConstraintIsFoundByItsKindAndKeyedApartFromAPolicy() {
        assertEquals(listOf("must-have-team"), demo.filtered(null, "K8sRequiredLabels").map { it.name })
        val twins = PolicyReport(
            policies = listOf(
                KyvernoPolicy(name = "same"),
                KyvernoPolicy(name = "same", engine = POLICY_ENGINE_GATEKEEPER, kind = "K8sRequiredLabels"),
                KyvernoPolicy(name = "same", engine = POLICY_ENGINE_GATEKEEPER, kind = "K8sAllowedRepos"),
            ),
        )
        assertEquals(3, twins.policies.map { it.key }.distinct().size)
    }

    @Test
    fun warnAndCappedViolationsDecode() {
        val report = TalosJson.decodeFromString(
            PolicyReport.serializer(),
            """{"installed":true,"gatekeeper":true,"policies":[{"name":"w","engine":"gatekeeper","action":"Warn","fail":40,"failingCapped":true,"failing":[{"kind":"Pod","name":"p"}]}]}""",
        )
        val w = report.policies.single()
        assertEquals(POLICY_ACTION_WARN, w.action)
        assertTrue(w.failingCapped)
        assertEquals(40, w.fail)
        assertFalse(report.bothEngines)
    }

    @Test
    fun aKyvernoPolicySwitchesToTheOtherModeAndSaysWhoWouldRevertIt() {
        assertEquals(listOf(POLICY_ACTION_AUDIT), KyvernoPolicy(name = "a", action = POLICY_ACTION_ENFORCE).switchTargets)
        assertEquals(listOf(POLICY_ACTION_ENFORCE), KyvernoPolicy(name = "a", action = POLICY_ACTION_AUDIT).switchTargets)
        assertEquals(listOf(POLICY_ACTION_AUDIT, POLICY_ACTION_ENFORCE), KyvernoPolicy(name = "a", action = POLICY_ACTION_MIXED).switchTargets)
        // A Gatekeeper constraint is not switched from here.
        assertTrue(KyvernoPolicy(name = "c", engine = POLICY_ENGINE_GATEKEEPER, action = POLICY_ACTION_ENFORCE).switchTargets.isEmpty())

        assertEquals("clusterpolicies", KyvernoPolicy(name = "a").kyvernoResource)
        assertEquals("policies", KyvernoPolicy(namespace = "shop", name = "a").kyvernoResource)
        assertEquals("Argo CD", KyvernoPolicy(name = "a", managedBy = "argocd").gitopsTool)
        assertEquals("Flux", KyvernoPolicy(name = "a", managedBy = "flux").gitopsTool)
        assertNull(KyvernoPolicy(name = "a").gitopsTool)
    }

    @Test
    fun aClusterWithoutKyvernoIsNotAnError() {
        val none = TalosJson.decodeFromString(PolicyReport.serializer(), """{"installed":false,"version":"","engine":{"namespace":"","controllers":null,"webhook":{"present":false}},"policies":[],"namespaces":[],"reportsRead":0,"reportsTruncated":false}""")
        assertFalse(none.installed)
        assertTrue(none.policies.isEmpty())
        assertEquals("", none.error)
    }

    @Test
    fun forbiddenReportsKeepThePolicies() {
        val report = TalosJson.decodeFromString(
            PolicyReport.serializer(),
            """{"installed":true,"reportsError":"forbidden","policies":[{"name":"a","action":"Audit","ready":true,"error":2}]}""",
        )
        assertEquals("forbidden", report.reportsError)
        assertEquals(listOf("a"), report.policies.map { it.name })
        assertEquals(2, report.policies.single().errors)
    }

    @Test
    fun aNamespaceKeepsItsOwnPoliciesAndTheClusterWideOnesThatCheckedIt() {
        assertEquals(demo.policies, demo.filtered(null, ""))
        // billing: no policy of its own; require-labels has no result there, so it is left out,
        // the not-ready one stays (a broken policy matters everywhere), and the constraint with a
        // violation there joins.
        assertEquals(
            listOf("verify-image-signatures", "disallow-privileged", "psp-privileged-container"),
            demo.filtered("billing", "").map { it.name },
        )
        assertEquals(
            listOf("verify-image-signatures", "must-have-team", "disallow-privileged", "restrict-image-registries", "require-labels"),
            demo.filtered("shop", "").map { it.name },
        )
        assertEquals(listOf("billing", "media", "shop", "vault"), demo.policyNamespaces)
    }

    @Test
    fun aPickedNamespaceShowsThePolicysCountsThere() {
        val labels = demo.policies.single { it.name == "require-labels" }
        assertEquals(PolicyResultCounts(fail = 7), labels.counts(null))
        assertEquals(PolicyResultCounts(fail = 7), labels.counts("shop"))
        assertEquals(PolicyResultCounts(), labels.counts("billing"))
        val privileged = demo.policies.single { it.name == "disallow-privileged" }
        assertEquals(PolicyResultCounts(pass = 15), privileged.counts(null))
        assertEquals(PolicyResultCounts(pass = 9), privileged.counts("billing"))
        assertTrue(privileged.checks("vault"))
        assertFalse(labels.checks("vault"))
    }

    @Test
    fun searchesNameCategorySeverityAndRules() {
        assertEquals(listOf("verify-image-signatures"), demo.filtered(null, "supply").map { it.name })
        assertEquals(listOf("require-labels"), demo.filtered(null, "  REQUIRE-OWNER ").map { it.name })
        assertEquals(listOf("verify-image-signatures", "disallow-privileged"), demo.filtered(null, "high").map { it.name })
        assertTrue(demo.filtered("shop", "nothing like it").isEmpty())
    }

    @Test
    fun aFailureOfAKnownKindOpensItsObject() {
        val pod = PolicyFailure(kind = "Pod", namespace = "shop", name = "web-0")
        assertEquals(KubeObjectRef("", "v1", "pods", "Pod", "shop", "web-0", editable = true), pod.objectRef())
        val deploy = PolicyFailure(kind = "Deployment", namespace = "shop", name = "web")
        assertEquals("apps", deploy.objectRef()?.group)
        assertEquals("deployments", deploy.objectRef()?.resource)
        // A Namespace is cluster-scoped: no namespace in its reference.
        assertEquals("", PolicyFailure(kind = "Namespace", namespace = "shop", name = "shop").objectRef()?.namespace)
        assertNull(PolicyFailure(kind = "Widget", namespace = "shop", name = "w").objectRef())
        assertNull(PolicyFailure(kind = "Pod", namespace = "shop").objectRef())
    }
}
