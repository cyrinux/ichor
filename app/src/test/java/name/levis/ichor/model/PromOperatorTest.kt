package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PromOperatorTest {
    private val rulesJson = """
        {"groups":[{"name":"hello-ichor.rules","file":"/etc/prometheus/rules/prometheus-x-rulefiles-0/demo-hello-ichor-1234.yaml",
          "ruleNamespace":"demo","ruleName":"hello-ichor","interval":30,"evaluationTime":0.0042,"lastEvaluation":1791600000000,
          "lastError":"many-to-many matching not allowed","firing":0,"pending":0,"errors":1,
          "rules":[{"name":"hello_ichor:http_requests:rate5m","type":"recording","state":"inactive","health":"err",
                    "lastError":"many-to-many matching not allowed","query":"sum by (pod) (rate(x[5m]))","severity":"","duration":0,"alerts":0,"lastEvaluation":1791600000000}]},
          {"name":"config.rules","file":"/etc/prometheus/extra.yaml","ruleNamespace":"","ruleName":"","rules":null}],
         "counts":{"groups":4,"rules":8,"firing":1,"pending":0,"errors":1},"truncated":false}
    """.trimIndent()

    private val targetsJson = """
        {"pools":[{"pool":"serviceMonitor/demo/hello-ichor/0","kind":"ServiceMonitor","namespace":"demo","name":"hello-ichor","endpoint":0,
          "up":1,"down":2,"unknown":0,
          "targets":[{"scrapeUrl":"http://10.244.2.32:8080/metrics","lastError":"connection refused",
                      "lastScrape":1791600000000,"lastScrapeDuration":0.012,"job":"hello-ichor","namespace":"demo",
                      "service":"hello-ichor","pod":"hello-ichor-7d9c5-fghij","instance":"10.244.2.32:8080"}]},
          {"pool":"kubelet","kind":"","namespace":"","name":"","endpoint":-1,"up":3,"down":0,"unknown":0,"targets":null}],
         "up":13,"down":2,"unknown":0,"total":15,"truncated":false}
    """.trimIndent()

    private val operatorJson = """
        {"installed":true,"error":"",
         "prometheuses":[{"namespace":"monitoring","name":"main","version":"v3.5.0","replicas":2,"shards":1,
           "desired":2,"available":1,"paused":false,"health":"warning",
           "conditions":[{"type":"Available","status":"Degraded","reason":"SomePodsNotReady","message":"1/2"},{"type":"Reconciled","status":"True","reason":"","message":""}]}],
         "alertmanagers":[{"namespace":"monitoring","name":"main","replicas":1,"shards":1,"desired":1,"available":1,"health":"ok","conditions":null}],
         "serviceMonitors":14,"podMonitors":2,"prometheusRules":35,"probes":0}
    """.trimIndent()

    @Test fun rulesDecode() {
        val rules = TalosJson.decodeFromString(PromRules.serializer(), rulesJson)
        assertEquals(4, rules.counts.groups)
        assertEquals(1, rules.counts.firing)
        val group = rules.groups[0]
        assertTrue(group.inTrouble)
        assertEquals(30.0, group.interval, 0.0)
        val rule = group.rules.single()
        assertEquals("recording", rule.type)
        assertEquals(PromHealth.CRITICAL, rule.level)
        assertEquals(KubeObjectRef("monitoring.coreos.com", "v1", "prometheusrules", "PrometheusRule", "demo", "hello-ichor", editable = true), group.ruleRef)
        // A group of the Prometheus configuration names no PrometheusRule; null lists decode empty.
        assertNull(rules.groups[1].ruleRef)
        assertTrue(rules.groups[1].rules.isEmpty())
        assertFalse(rules.groups[1].inTrouble)
    }

    @Test fun ruleLevels() {
        assertEquals(PromHealth.WARNING, PromRule(type = "alerting", state = "firing", health = "ok", severity = "warning").level)
        assertEquals(PromHealth.CRITICAL, PromRule(type = "alerting", state = "firing", health = "ok", severity = "critical").level)
        assertEquals(PromHealth.WARNING, PromRule(type = "alerting", state = "pending", health = "ok").level)
        assertEquals(PromHealth.OK, PromRule(type = "alerting", state = "inactive", health = "ok").level)
        assertEquals(PromHealth.UNKNOWN, PromRule(type = "recording", state = "inactive", health = "unknown").level)
    }

    @Test fun targetsDecode() {
        val t = TalosJson.decodeFromString(PromTargets.serializer(), targetsJson)
        assertEquals(15, t.total)
        assertEquals(2, t.down)
        val pool = t.pools[0]
        assertEquals("ServiceMonitor", pool.kind)
        assertEquals(0, pool.endpoint)
        assertEquals("hello-ichor-7d9c5-fghij", pool.targets.single().pod)
        assertEquals(1791600000000, pool.targets.single().lastScrape)
        assertTrue(t.pools[1].targets.isEmpty())
    }

    @Test fun poolMapsToItsMonitor() {
        fun pool(kind: String) = PromTargetPool(kind = kind, namespace = "demo", name = "web")
        assertEquals(
            KubeObjectRef("monitoring.coreos.com", "v1", "servicemonitors", "ServiceMonitor", "demo", "web", editable = true),
            pool("ServiceMonitor").monitorRef,
        )
        assertEquals("podmonitors", pool("PodMonitor").monitorRef?.resource)
        assertEquals("probes", pool("Probe").monitorRef?.resource)
        val scrape = pool("ScrapeConfig").monitorRef
        assertEquals("scrapeconfigs", scrape?.resource)
        assertEquals("v1alpha1", scrape?.version)
        // A job of the Prometheus configuration has no object.
        assertNull(PromTargetPool(pool = "kubelet").monitorRef)
        assertNull(PromTargetPool(kind = "ServiceMonitor").monitorRef)
    }

    @Test fun targetLinksToPodThenServiceThenMonitor() {
        val pool = PromTargetPool(kind = "ServiceMonitor", namespace = "demo", name = "web")
        val withPod = PromTarget(namespace = "demo", service = "web", pod = "web-abc")
        assertEquals(PromLink.Focus(KubeFocus(1, "demo/web-abc", "demo", "web-abc")), pool.linkOf(withPod))
        val withService = PromTarget(namespace = "demo", service = "web")
        assertEquals(
            PromLink.Object(KubeObjectRef("", "v1", "services", "Service", "demo", "web", editable = true)),
            pool.linkOf(withService),
        )
        assertEquals(PromLink.Object(pool.monitorRef!!), pool.linkOf(PromTarget(instance = "10.0.0.1:10250")))
        assertNull(PromTargetPool(pool = "kubelet").linkOf(PromTarget(instance = "10.0.0.1:10250")))
    }

    @Test fun operatorDecodes() {
        val op = TalosJson.decodeFromString(PromOperatorStatus.serializer(), operatorJson)
        assertTrue(op.installed)
        assertEquals(14, op.serviceMonitors)
        val prom = op.prometheuses.single()
        assertEquals(PromHealth.WARNING, prom.level)
        assertEquals(PromHealth.WARNING, prom.conditions[0].level)
        assertEquals(PromHealth.OK, prom.conditions[1].level)
        assertEquals("prometheuses", prom.objectRef(alertmanager = false).resource)
        val am = op.alertmanagers.single()
        assertEquals(PromHealth.OK, am.level)
        assertTrue(am.conditions.isEmpty())
        assertEquals("Alertmanager", am.objectRef(alertmanager = true).kind)
        assertEquals(PromHealth.CRITICAL, PromOperatorCondition(type = "Available", status = "False").level)
        assertEquals(PromHealth.WARNING, PromOperatorCondition(type = "Reconciled", status = "False").level)
    }

    @Test fun operatorNotInstalled() {
        val op = TalosJson.decodeFromString(PromOperatorStatus.serializer(), """{"installed":false,"error":"","prometheuses":null,"alertmanagers":null}""")
        assertFalse(op.installed)
        assertTrue(op.prometheuses.isEmpty())
    }

    @Test fun checkupMonitoringSectionDecodes() {
        val raw = """
            {"status":"warning","sections":[{"id":"monitoring","status":"warning","checked":23,"findings":[
              {"kind":"scrapeTargetsDown","severity":"warning","namespace":"monitoring","name":"prometheus-operated","count":2,"limit":15,"value":13.3,"extra":"serviceMonitor/demo/hello-ichor/0"},
              {"kind":"prometheusRuleErrors","severity":"warning","namespace":"demo","name":"hello-ichor","count":1,"reason":"hello_ichor:http_requests:rate5m","message":"many-to-many matching not allowed"},
              {"kind":"ruleGroupErrors","severity":"warning","name":"extra.rules","count":1,"extra":"/etc/prometheus/extra.yaml"}]}]}
        """.trimIndent()
        val report = TalosJson.decodeFromString(CheckupReport.serializer(), raw)
        val section = report.shownSections.single()
        assertEquals(CheckupSectionId.MONITORING, section.id)
        assertEquals(
            listOf(CheckupKind.SCRAPE_TARGETS_DOWN, CheckupKind.PROMETHEUS_RULE_ERRORS, CheckupKind.RULE_GROUP_ERRORS),
            section.findings.map { it.kind },
        )
        assertEquals("demo/hello-ichor", section.findings[1].subject)
        assertEquals(setOf("monitoring|scrapeTargetsDown|monitoring/prometheus-operated", "monitoring|prometheusRuleErrors|demo/hello-ichor", "monitoring|ruleGroupErrors|extra.rules"), report.alertIssues().keys)
    }
}
