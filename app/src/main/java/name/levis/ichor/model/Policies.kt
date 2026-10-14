package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_policies.go: the Kyverno admission policies and their PolicyReport findings.

const val POLICY_ACTION_ENFORCE = "Enforce"
const val POLICY_ACTION_AUDIT = "Audit"

/** Ours: the rules' own failure actions disagree with the policy's. */
const val POLICY_ACTION_MIXED = "Mixed"

/** What the webhook does to requests while Kyverno is down. */
const val WEBHOOK_FAIL = "Fail"
const val WEBHOOK_IGNORE = "Ignore"

/** Kyverno's policies (os:admin); [installed] false and nothing else on a cluster without it. */
@Serializable
data class PolicyReport(
    val installed: Boolean = false,
    /** The served kyverno.io version. */
    val version: String = "",
    /** A policy or engine read that failed; the rest still shows. */
    val error: String = "",
    /** The policy reports could not be read (often a 403): the counts stay at 0. */
    val reportsError: String = "",
    val engine: PolicyEngine = PolicyEngine(),
    /** Not ready first, then Enforce, Mixed, Audit, the most failing first (the Go order). */
    val policies: List<KyvernoPolicy> = emptyList(),
    val namespaces: List<PolicyNamespaceTotals> = emptyList(),
    val reportsRead: Int = 0,
    /** The read stopped taking reports: the counts cover the first [reportsRead]. */
    val reportsTruncated: Boolean = false,
)

/** The engine: its controllers, and what its webhook does while it is down. */
@Serializable
data class PolicyEngine(
    val namespace: String = "",
    val controllers: List<PolicyController> = emptyList(),
    val webhook: PolicyWebhook = PolicyWebhook(),
)

@Serializable
data class PolicyController(val name: String, val ready: Int = 0, val desired: Int = 0) {
    val healthy: Boolean get() = desired > 0 && ready >= desired
}

@Serializable
data class PolicyWebhook(val present: Boolean = false, val failurePolicy: String = "")

@Serializable
data class KyvernoPolicy(
    /** Empty for a ClusterPolicy. */
    val namespace: String = "",
    val name: String,
    /** [POLICY_ACTION_ENFORCE], [POLICY_ACTION_AUDIT] or [POLICY_ACTION_MIXED]. */
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
) {
    val key: String get() = "$namespace/$name"
    val clusterWide: Boolean get() = namespace.isEmpty()
}

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
 * namespace keeps its own policies and the cluster-wide ones, which apply to it too.
 */
fun PolicyReport.filtered(namespace: String?, query: String): List<KyvernoPolicy> {
    val q = query.trim()
    return policies
        .filter { namespace == null || it.clusterWide || it.namespace == namespace }
        .filter { p ->
            q.isEmpty() || (listOf(p.name, p.namespace, p.category, p.severity, p.description, p.action) + p.ruleNames)
                .any { it.contains(q, ignoreCase = true) }
        }
}

/** The namespaces with findings or policies, for the filter chips. */
val PolicyReport.policyNamespaces: List<String>
    get() = (namespaces.map { it.namespace } + policies.map { it.namespace }).filter { it.isNotEmpty() }.distinct().sorted()
