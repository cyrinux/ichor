package name.levis.ichor.monitor

import name.levis.ichor.model.ALERT_ACTION_DIGEST
import name.levis.ichor.model.ALERT_ACTION_MUTE
import name.levis.ichor.model.ALERT_ACTION_NOTIFY
import name.levis.ichor.model.ALERT_REASON_DIGEST
import name.levis.ichor.model.ALERT_REASON_QUIET
import name.levis.ichor.model.ALERT_REASON_SEVERITY
import name.levis.ichor.model.ALERT_REASON_SNOOZED
import name.levis.ichor.model.AlertDecision
import name.levis.ichor.model.AlertDecisions
import name.levis.ichor.model.AlertDigest
import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertRulePlanTest {
    private val prod = ContextSummary(name = "prod-eu", fingerprint = "fp-prod", clusterId = "prod")
    private val staging = ContextSummary(name = "staging-eu", fingerprint = "fp-staging", clusterId = "staging")

    private val nodeDown = Alert("node:10.0.0.2", AlertKind.NODE_UNREACHABLE, true, "worker-1", "no route")
    private val nodeBack = Alert("node:10.0.0.2", AlertKind.NODE_READY, false, "worker-1", "10.0.0.2")
    private val etcd = Alert("etcd:NOSPACE", AlertKind.ETCD_ALARM, true, "NOSPACE", "member-1")
    private val amWarn = Alert("am:abc", AlertKind.AM_FIRING, true, "KubePodCrashLooping", "warning|KubePodCrashLooping|shop/web-0")
    private val amInfo = Alert("am:def", AlertKind.AM_FIRING, true, "Watchdog", "INFO|Watchdog|")
    private val argo = Alert("gitops:argocd|apps/shop", AlertKind.GITOPS_PROBLEM, true, "apps/shop", "argocd|critical|failed")
    private val data = Alert("data:longhorn|vol-1", AlertKind.DATA_PROBLEM, true, "vol-1", "longhorn|warning")
    private val checkup = Alert("checkup:volumes|pvc|shop/data", AlertKind.CHECKUP_PROBLEM, true, "shop/data", "volumes|pvc|critical")
    private val storage = Alert(
        "storage:10.0.0.3|EPHEMERAL", AlertKind.STORAGE_PROBLEM, true, "worker-2",
        StorageDetail(DATA_CRITICAL, smart = false, hostname = "worker-2", name = "EPHEMERAL", percent = 96).format(),
    )
    private val trend = Alert(
        "trend:10.0.0.3|EPHEMERAL", AlertKind.STORAGE_TREND, true, "worker-2",
        TrendDetail("worker-2", "EPHEMERAL", critical = false, days = 3, slopePerDay = 2.0, percent = 80).format(),
    )
    private val plugin = Alert("plugin:kube-green:x", AlertKind.PLUGIN_PROBLEM, true, "weekend", PluginIssue(severity = DATA_CRITICAL, plugin = "kube-green").encode())
    private val certExpired = Alert("cert", AlertKind.CERT_EXPIRED, true, days = 2)
    private val certExpiring = Alert("cert", AlertKind.CERT_EXPIRING, true, days = 2)
    private val unreachable = Alert("unreachable", AlertKind.CLUSTER_UNREACHABLE, true, detail = "3")
    private val merged = Alert("proposal:1", AlertKind.PROPOSAL_MERGED, false, "Bump", "")

    @Test
    fun eachKindHasTheSeverityItsDetailKeeps() {
        val severities = listOf(nodeDown, etcd, unreachable, amWarn, amInfo, argo, data, checkup, storage, trend, plugin, certExpired, certExpiring, merged)
            .map { it.ruleKind to it.ruleSeverity }
        assertEquals(
            listOf(
                "node" to "", "etcd" to "", "unreachable" to "", "am" to "warning", "am" to "info", "gitops" to "critical",
                "data" to "warning", "checkup" to "critical", "storage" to "critical", "trend" to "warning", "plugin" to "critical",
                "cert" to "critical", "cert" to "warning", "proposal" to "info",
            ),
            severities,
        )
    }

    @Test
    fun inputsKeepTheOrderTheProblemAndTheSnooze() {
        val inputs = alertInputs(listOf(ClusterAlerts(prod, listOf(nodeDown, amWarn)), ClusterAlerts(staging, listOf(nodeBack)))) { cluster, key ->
            cluster == "fp-prod" && key == amWarn.key
        }
        assertEquals(listOf("fp-prod" to "node:10.0.0.2", "fp-prod" to "am:abc", "fp-staging" to "node:10.0.0.2"), inputs.map { it.cluster to it.key })
        assertEquals(listOf(true, true, false), inputs.map { it.problem })
        assertEquals(listOf(false, true, false), inputs.map { it.snoozed })
    }

    @Test
    fun withoutDecisionsEveryUnsnoozedAlertIsPosted() {
        val clusters = listOf(ClusterAlerts(prod, listOf(nodeDown, amWarn)))
        val inputs = alertInputs(clusters) { _, key -> key == amWarn.key }
        val plan = alertPlan(clusters, inputs, decisions = null)
        assertEquals(listOf(ClusterAlerts(prod, listOf(nodeDown))), plan.notify)
        assertTrue(plan.muted.isEmpty() && plan.digests.isEmpty())
    }

    @Test
    fun notifyMuteAndDigestAreSplit() {
        val clusters = listOf(ClusterAlerts(prod, listOf(nodeDown, amWarn, data)), ClusterAlerts(staging, listOf(argo, amInfo)))
        val inputs = alertInputs(clusters) { _, key -> key == data.key }
        val decisions = AlertDecisions(
            decisions = listOf(
                AlertDecision("fp-prod", nodeDown.key, ALERT_ACTION_NOTIFY),
                AlertDecision("fp-prod", amWarn.key, ALERT_ACTION_MUTE, ALERT_REASON_QUIET, until = 7_000),
                AlertDecision("fp-prod", data.key, ALERT_ACTION_MUTE, ALERT_REASON_SNOOZED),
                AlertDecision("fp-staging", argo.key, ALERT_ACTION_DIGEST, ALERT_REASON_DIGEST),
                AlertDecision("fp-staging", amInfo.key, ALERT_ACTION_DIGEST, ALERT_REASON_DIGEST),
            ),
            digests = listOf(AlertDigest("fp-staging", count = 2, critical = 1, warning = 0, firstKeys = listOf(argo.key, amInfo.key))),
        )
        val plan = alertPlan(clusters, inputs, decisions)
        assertEquals(listOf(ClusterAlerts(prod, listOf(nodeDown))), plan.notify)
        // The snoozed one is not recorded: the user silenced it themselves.
        assertEquals(listOf(MutedAlert(prod, amWarn, ALERT_REASON_QUIET, 7_000)), plan.muted)
        assertEquals(listOf(DigestPost(staging, decisions.digests.single(), argo.channel)), plan.digests)
    }

    @Test
    fun aSeverityMuteIsRecorded() {
        val clusters = listOf(ClusterAlerts(staging, listOf(amWarn)))
        val inputs = alertInputs(clusters) { _, _ -> false }
        val decisions = AlertDecisions(listOf(AlertDecision("fp-staging", amWarn.key, ALERT_ACTION_MUTE, ALERT_REASON_SEVERITY)))
        val plan = alertPlan(clusters, inputs, decisions)
        assertTrue(plan.notify.isEmpty())
        assertEquals(listOf(MutedAlert(staging, amWarn, ALERT_REASON_SEVERITY, 0)), plan.muted)
    }

    @Test
    fun anAlertWithoutAMatchingDecisionIsPosted() {
        val clusters = listOf(ClusterAlerts(prod, listOf(nodeDown, amWarn)))
        val inputs = alertInputs(clusters) { _, _ -> false }
        // Go answered for another key at the second place, and not at all for a third.
        val decisions = AlertDecisions(listOf(AlertDecision("fp-prod", nodeDown.key, ALERT_ACTION_MUTE, ALERT_REASON_QUIET), AlertDecision("fp-prod", "am:other", ALERT_ACTION_MUTE)))
        val plan = alertPlan(clusters, inputs, decisions)
        assertEquals(listOf(ClusterAlerts(prod, listOf(amWarn))), plan.notify)
        assertEquals(listOf(nodeDown), plan.muted.map { it.alert })
    }

    @Test
    fun aDigestGoDidNotCountIsPostedOnItsOwn() {
        val clusters = listOf(ClusterAlerts(staging, listOf(argo)))
        val inputs = alertInputs(clusters) { _, _ -> false }
        val plan = alertPlan(clusters, inputs, AlertDecisions(listOf(AlertDecision("fp-staging", argo.key, ALERT_ACTION_DIGEST, ALERT_REASON_DIGEST))))
        assertEquals(listOf(ClusterAlerts(staging, listOf(argo))), plan.notify)
        assertTrue(plan.digests.isEmpty())
    }

    @Test
    fun oneDigestPerClusterAcrossItsGroups() {
        // A run's alerts, then its pull requests: two groups of the same cluster.
        val clusters = listOf(ClusterAlerts(staging, listOf(amWarn)), ClusterAlerts(staging, listOf(merged)))
        val inputs = alertInputs(clusters) { _, _ -> false }
        val decisions = AlertDecisions(
            decisions = listOf(
                AlertDecision("fp-staging", amWarn.key, ALERT_ACTION_DIGEST, ALERT_REASON_DIGEST),
                AlertDecision("fp-staging", merged.key, ALERT_ACTION_DIGEST, ALERT_REASON_DIGEST),
            ),
            digests = listOf(AlertDigest("fp-staging", count = 2, warning = 1)),
        )
        val plan = alertPlan(clusters, inputs, decisions)
        assertEquals(1, plan.digests.size)
        assertEquals(amWarn.channel, plan.digests.single().channel)
        assertTrue(plan.notify.isEmpty())
    }
}
