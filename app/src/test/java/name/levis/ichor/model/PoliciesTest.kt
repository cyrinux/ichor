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
          "version": "v1",
          "engine": {
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
              "present": true,
              "failurePolicy": "Fail"
            }
          },
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
              "byNamespace": null
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
              "byNamespace": [
                {
                  "namespace": "shop",
                  "pass": 0,
                  "fail": 7,
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
              "fail": 7,
              "warn": 2,
              "error": 0
            },
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
        assertEquals("kyverno", demo.engine.namespace)
        assertEquals(4, demo.engine.controllers.size)
        assertTrue(demo.engine.controllers.all { it.healthy })
        assertEquals(WEBHOOK_FAIL, demo.engine.webhook.failurePolicy)
        // Not ready first, then Enforce, Mixed, Audit.
        assertEquals(
            listOf("verify-image-signatures", "disallow-privileged", "restrict-image-registries", "require-labels"),
            demo.policies.map { it.name },
        )
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
        // and the not-ready one stays (a broken policy matters everywhere).
        assertEquals(listOf("verify-image-signatures", "disallow-privileged"), demo.filtered("billing", "").map { it.name })
        assertEquals(
            listOf("verify-image-signatures", "disallow-privileged", "restrict-image-registries", "require-labels"),
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
