package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/prom_rules.go, prom_targets.go and prom_operator.go (CYR-35).

/** The rule groups Prometheus evaluates, those in trouble first, and counts over all of them. */
@Serializable
data class PromRules(
    val groups: List<PromRuleGroup> = emptyList(),
    val counts: PromRuleCounts = PromRuleCounts(),
    val truncated: Boolean = false,
)

@Serializable
data class PromRuleCounts(
    val groups: Int = 0,
    val rules: Int = 0,
    val firing: Int = 0,
    val pending: Int = 0,
    val errors: Int = 0,
)

/**
 * A rule group; [ruleNamespace]/[ruleName] name the PrometheusRule it comes from, "" when
 * unknown. [interval] and [evaluationTime] are seconds, [lastEvaluation] unix ms (0: never).
 */
@Serializable
data class PromRuleGroup(
    val name: String = "",
    val file: String = "",
    val ruleNamespace: String = "",
    val ruleName: String = "",
    val interval: Double = 0.0,
    val evaluationTime: Double = 0.0,
    val lastEvaluation: Long = 0,
    val lastError: String = "",
    val firing: Int = 0,
    val pending: Int = 0,
    val errors: Int = 0,
    val rules: List<PromRule> = emptyList(),
)

/** One rule: [type] alerting or recording, [state] firing, pending or inactive, [health] ok, err or unknown. */
@Serializable
data class PromRule(
    val name: String = "",
    val type: String = "",
    val state: String = "",
    val health: String = "",
    val lastError: String = "",
    val query: String = "",
    val severity: String = "",
    /** `for:` in seconds. */
    val duration: Double = 0.0,
    val alerts: Int = 0,
    val lastEvaluation: Long = 0,
)

/** The scrape pools with their counts, most down first; only the down targets are listed. */
@Serializable
data class PromTargets(
    val pools: List<PromTargetPool> = emptyList(),
    val up: Int = 0,
    val down: Int = 0,
    val unknown: Int = 0,
    val total: Int = 0,
    val truncated: Boolean = false,
)

/**
 * A scrape pool, parsed into its monitor: [kind] ServiceMonitor, PodMonitor, Probe or
 * ScrapeConfig at [namespace]/[name], "" for a job of the Prometheus configuration.
 */
@Serializable
data class PromTargetPool(
    val pool: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val endpoint: Int = -1,
    val up: Int = 0,
    val down: Int = 0,
    val unknown: Int = 0,
    val targets: List<PromTarget> = emptyList(),
)

/** A down target; [service] and [pod] are its labels, "" when it has none (a kubelet). */
@Serializable
data class PromTarget(
    val scrapeUrl: String = "",
    val lastError: String = "",
    /** Unix ms, 0 before the first scrape. */
    val lastScrape: Long = 0,
    val lastScrapeDuration: Double = 0.0,
    val job: String = "",
    val namespace: String = "",
    val service: String = "",
    val pod: String = "",
    val instance: String = "",
)

/** The Prometheus Operator's servers and how much it works from; not [installed] without its CRDs. */
@Serializable
data class PromOperatorStatus(
    val installed: Boolean = false,
    val error: String = "",
    val prometheuses: List<PromOperatorServer> = emptyList(),
    val alertmanagers: List<PromOperatorServer> = emptyList(),
    val serviceMonitors: Int = 0,
    val podMonitors: Int = 0,
    val prometheusRules: Int = 0,
    val probes: Int = 0,
)

/** A Prometheus or Alertmanager object; [health] critical, warning or ok. */
@Serializable
data class PromOperatorServer(
    val namespace: String = "",
    val name: String = "",
    val version: String = "",
    val replicas: Int = 0,
    val shards: Int = 0,
    val desired: Int = 0,
    val available: Int = 0,
    val paused: Boolean = false,
    val health: String = "",
    val conditions: List<PromOperatorCondition> = emptyList(),
)

@Serializable
data class PromOperatorCondition(
    val type: String = "",
    /** True, False, Degraded (Available) or Unknown. */
    val status: String = "",
    val reason: String = "",
    val message: String = "",
)

/** Where a Monitoring row leads: the Kubernetes screen focused on a pod, or an object screen. */
sealed interface PromLink {
    data class Focus(val focus: KubeFocus) : PromLink
    data class Object(val ref: KubeObjectRef) : PromLink
}

private const val MONITORING_GROUP = "monitoring.coreos.com"

private fun monitoringRef(resource: String, kind: String, namespace: String, name: String, version: String = "v1") =
    KubeObjectRef(MONITORING_GROUP, version, resource, kind, namespace, name, editable = true)

/** The pool's ServiceMonitor, PodMonitor, Probe or ScrapeConfig; null for a configuration job. */
val PromTargetPool.monitorRef: KubeObjectRef?
    get() {
        if (namespace.isEmpty() || name.isEmpty()) return null
        return when (kind) {
            "ServiceMonitor" -> monitoringRef("servicemonitors", kind, namespace, name)
            "PodMonitor" -> monitoringRef("podmonitors", kind, namespace, name)
            "Probe" -> monitoringRef("probes", kind, namespace, name)
            "ScrapeConfig" -> monitoringRef("scrapeconfigs", kind, namespace, name, version = "v1alpha1")
            else -> null
        }
    }

/** A down target's pod on the Kubernetes screen, else its Service, else the pool's monitor. */
fun PromTargetPool.linkOf(target: PromTarget): PromLink? = when {
    target.namespace.isNotEmpty() && target.pod.isNotEmpty() ->
        ShareTarget.pod(target.namespace, target.pod).kubeFocus?.let { PromLink.Focus(it) }
    target.namespace.isNotEmpty() && target.service.isNotEmpty() ->
        PromLink.Object(KubeObjectRef("", "v1", "services", "Service", target.namespace, target.service, editable = true))
    else -> monitorRef?.let { PromLink.Object(it) }
}

/** The PrometheusRule the group comes from; null when the core could not tell. */
val PromRuleGroup.ruleRef: KubeObjectRef?
    get() = if (ruleNamespace.isEmpty() || ruleName.isEmpty()) null else monitoringRef("prometheusrules", "PrometheusRule", ruleNamespace, ruleName)

/** The Prometheus object, or the Alertmanager one when [alertmanager]. */
fun PromOperatorServer.objectRef(alertmanager: Boolean): KubeObjectRef =
    if (alertmanager) monitoringRef("alertmanagers", "Alertmanager", namespace, name) else monitoringRef("prometheuses", "Prometheus", namespace, name)

/** A group with failing, firing or pending rules. */
val PromRuleGroup.inTrouble: Boolean get() = errors > 0 || firing > 0 || pending > 0

/** The health of an operator server or a rule, worst first. */
enum class PromHealth { CRITICAL, WARNING, OK, UNKNOWN }

val PromOperatorServer.level: PromHealth
    get() = when (health) {
        "critical" -> PromHealth.CRITICAL
        "warning" -> PromHealth.WARNING
        "ok" -> PromHealth.OK
        else -> PromHealth.UNKNOWN
    }

/** A rule's look: failing, then firing (critical when its severity says so), pending, fine. */
val PromRule.level: PromHealth
    get() = when {
        health == "err" -> PromHealth.CRITICAL
        state == "firing" -> if (severity == "critical") PromHealth.CRITICAL else PromHealth.WARNING
        state == "pending" -> PromHealth.WARNING
        health == "ok" -> PromHealth.OK
        else -> PromHealth.UNKNOWN
    }

/** A condition's look: Available False is critical, Degraded or Reconciled False a warning. */
val PromOperatorCondition.level: PromHealth
    get() = when {
        status == "True" -> PromHealth.OK
        type == "Available" && status == "False" -> PromHealth.CRITICAL
        status == "False" || status == "Degraded" -> PromHealth.WARNING
        else -> PromHealth.UNKNOWN
    }
