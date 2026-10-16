package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_policies.go: the Kyverno admission policies with their PolicyReport
// findings, and the Gatekeeper constraints with their audit violations.

const val POLICY_ACTION_ENFORCE = "Enforce"
const val POLICY_ACTION_AUDIT = "Audit"

/** Ours: the rules' own failure actions disagree with the policy's (Gatekeeper's "scoped"). */
const val POLICY_ACTION_MIXED = "Mixed"

/** Gatekeeper's warn: a request that breaks the constraint is admitted with a warning. */
const val POLICY_ACTION_WARN = "Warn"

const val POLICY_ENGINE_KYVERNO = "kyverno"
const val POLICY_ENGINE_GATEKEEPER = "gatekeeper"

/** What the webhook does to requests while the engine is down. */
const val WEBHOOK_FAIL = "Fail"
const val WEBHOOK_IGNORE = "Ignore"

/** Kyverno's policies and Gatekeeper's constraints (os:admin); [installed] false and nothing else on a cluster with neither. */
@Serializable
data class PolicyReport(
    val installed: Boolean = false,
    val kyverno: Boolean = false,
    val gatekeeper: Boolean = false,
    /** The served kyverno.io version. */
    val version: String = "",
    /** A Kyverno policy or engine read that failed; the rest still shows. */
    val error: String = "",
    /** A Gatekeeper template, constraint or engine read that failed. */
    val gatekeeperError: String = "",
    /** The policy reports could not be read (often a 403): the counts stay at 0. */
    val reportsError: String = "",
    /** One per engine that runs: Kyverno first. */
    val engines: List<PolicyEngine> = emptyList(),
    /** Not ready first, then Enforce, Mixed, Warn, Audit, the most failing first (the Go order). */
    val policies: List<KyvernoPolicy> = emptyList(),
    val namespaces: List<PolicyNamespaceTotals> = emptyList(),
    val reportsRead: Int = 0,
    /** The read stopped taking reports: the counts cover the first [reportsRead]. */
    val reportsTruncated: Boolean = false,
)

/** Both engines run: each row says which one it belongs to. */
val PolicyReport.bothEngines: Boolean get() = kyverno && gatekeeper

/** An engine: its controllers, and what its webhook does while it is down. */
@Serializable
data class PolicyEngine(
    /** [POLICY_ENGINE_KYVERNO] or [POLICY_ENGINE_GATEKEEPER]. */
    val name: String = "",
    val namespace: String = "",
    val controllers: List<PolicyController> = emptyList(),
    val webhook: PolicyWebhook = PolicyWebhook(),
)

@Serializable
data class PolicyController(val name: String, val ready: Int = 0, val desired: Int = 0) {
    val healthy: Boolean get() = desired > 0 && ready >= desired
}

@Serializable
data class PolicyWebhook(val name: String = "", val present: Boolean = false, val failurePolicy: String = "")

/** A Kyverno policy or a Gatekeeper constraint ([engine] says which). */
@Serializable
data class KyvernoPolicy(
    /** Empty for a ClusterPolicy or a constraint. */
    val namespace: String = "",
    val name: String,
    /** [POLICY_ENGINE_KYVERNO] or [POLICY_ENGINE_GATEKEEPER]. */
    val engine: String = POLICY_ENGINE_KYVERNO,
    /** A constraint's kind (its template's): K8sRequiredLabels. */
    val kind: String = "",
    /** [POLICY_ACTION_ENFORCE], [POLICY_ACTION_AUDIT], [POLICY_ACTION_MIXED] or [POLICY_ACTION_WARN]. */
    val action: String = "",
    /** It also checks the resources already there, not only new requests. */
    val background: Boolean = false,
    val ready: Boolean = false,
    val readyReason: String = "",
    val readyMessage: String = "",
    val category: String = "",
    val severity: String = "",
    val description: String = "",
    val rules: PolicyRules = PolicyRules(),
    val ruleNames: List<String> = emptyList(),
    val pass: Int = 0,
    val fail: Int = 0,
    val warn: Int = 0,
    @SerialName("error") val errors: Int = 0,
    val skip: Int = 0,
    /** The newest failing resources, at most 20; [fail] counts them all. */
    val failing: List<PolicyFailure> = emptyList(),
    /** [failing] is the first violations Gatekeeper kept (its own limit), not all of [fail]. */
    val failingCapped: Boolean = false,
    /** When Gatekeeper's audit last wrote the violations, unix seconds; 0 for Kyverno. */
    val auditAt: Long = 0,
    /** The same counts per namespace: a ClusterPolicy's results spread over every namespace. */
    val byNamespace: List<PolicyNamespaceTotals> = emptyList(),
) {
    /** Unique in a report: a Gatekeeper constraint's name is unique per kind only. */
    val key: String get() = "$engine/$kind/$namespace/$name"
    val clusterWide: Boolean get() = namespace.isEmpty()
    val isGatekeeper: Boolean get() = engine == POLICY_ENGINE_GATEKEEPER

    /** Its counts in [namespace], or across the cluster for null. */
    fun counts(namespace: String?): PolicyResultCounts {
        if (namespace == null) return PolicyResultCounts(fail, warn, pass)
        val row = byNamespace.firstOrNull { it.namespace == namespace } ?: return PolicyResultCounts()
        return PolicyResultCounts(row.fail, row.warn, row.pass)
    }

    /** It has results in [namespace]: it checked something there. */
    fun checks(namespace: String): Boolean =
        byNamespace.any { it.namespace == namespace && it.pass + it.fail + it.warn + it.errors > 0 }
}

/** A policy's failing, warning and passing results, over the cluster or in one namespace. */
data class PolicyResultCounts(val fail: Int = 0, val warn: Int = 0, val pass: Int = 0)

/** A policy's rules by kind. */
@Serializable
data class PolicyRules(val validate: Int = 0, val mutate: Int = 0, val generate: Int = 0, val verifyImages: Int = 0)

/** A resource a policy's rule fails. */
@Serializable
data class PolicyFailure(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val rule: String = "",
    val message: String = "",
    val severity: String = "",
    /** Unix seconds, 0 when the result had none. */
    val at: Long = 0,
) {
    val key: String get() = "$kind/$namespace/$name/$rule"

    /** The resource browser's reference of a well-known kind; null for another (shown as text). */
    fun objectRef(): KubeObjectRef? {
        val (group, resource) = FAILURE_KINDS[kind] ?: return null
        if (name.isEmpty()) return null
        val namespaced = kind != "Namespace"
        return KubeObjectRef(group, "v1", resource, kind, if (namespaced) namespace else "", name, editable = true)
    }

    private companion object {
        /** The kinds policies usually check: their group and resource, at v1. */
        val FAILURE_KINDS = mapOf(
            "Pod" to ("" to "pods"),
            "Service" to ("" to "services"),
            "ConfigMap" to ("" to "configmaps"),
            "Namespace" to ("" to "namespaces"),
            "ServiceAccount" to ("" to "serviceaccounts"),
            "Deployment" to ("apps" to "deployments"),
            "StatefulSet" to ("apps" to "statefulsets"),
            "DaemonSet" to ("apps" to "daemonsets"),
            "ReplicaSet" to ("apps" to "replicasets"),
            "Job" to ("batch" to "jobs"),
            "CronJob" to ("batch" to "cronjobs"),
            "Ingress" to ("networking.k8s.io" to "ingresses"),
        )
    }
}

/** One namespace's report results, for the namespace filter. */
@Serializable
data class PolicyNamespaceTotals(
    val namespace: String,
    val pass: Int = 0,
    val fail: Int = 0,
    val warn: Int = 0,
    @SerialName("error") val errors: Int = 0,
)

/**
 * The policies a [namespace] (null: every one) and a [query] keep, in the report's order. A
 * namespace keeps its own policies, and the cluster-wide ones that checked something there or
 * are not ready (a broken policy matters everywhere).
 */
fun PolicyReport.filtered(namespace: String?, query: String): List<KyvernoPolicy> {
    val q = query.trim()
    return policies
        .filter { namespace == null || it.namespace == namespace || (it.clusterWide && (!it.ready || it.checks(namespace))) }
        .filter { p ->
            q.isEmpty() || (listOf(p.name, p.namespace, p.kind, p.category, p.severity, p.description, p.action) + p.ruleNames)
                .any { it.contains(q, ignoreCase = true) }
        }
}

/** The namespaces with findings or policies, for the filter chips. */
val PolicyReport.policyNamespaces: List<String>
    get() = (namespaces.map { it.namespace } + policies.map { it.namespace }).filter { it.isNotEmpty() }.distinct().sorted()
