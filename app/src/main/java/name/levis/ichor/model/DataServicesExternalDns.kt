package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_externaldns.go (see DataServices.kt).

@Serializable
data class ExternalDnsStatus(
    /** The DNSEndpoint CRD's API version, "" when the CRD is not served. */
    val version: String = "",
    val error: String = "",
    /** One per external-dns Deployment; several are normal (one per provider or zone). */
    val controllers: List<ExternalDnsController> = emptyList(),
    /** Problems first. */
    val records: List<ExternalDnsRecord> = emptyList(),
    /** All of them; [records] is capped at 500. */
    val recordCount: Int = 0,
    val truncated: Boolean = false,
)

@Serializable
data class ExternalDnsController(
    val namespace: String = "",
    val name: String = "",
    val ready: Boolean = false,
    val replicas: Int = 0,
    val readyReplicas: Int = 0,
    val provider: String = "",
    /** sync, upsert-only, create-only; "" is the controller's default. */
    val policy: String = "",
    val registry: String = "",
    val ownerId: String = "",
    val txtPrefix: String = "",
    val txtSuffix: String = "",
    val domainFilters: List<String> = emptyList(),
    val sources: List<String> = emptyList(),
    val dryRun: Boolean = false,
    /** --interval in seconds, 60 (its default) when unset. */
    val intervalSeconds: Int = 60,
    val annotationFilter: String = "",
    /** --managed-record-types, upper-cased; A, AAAA, CNAME (its default) when unset. */
    val managedRecordTypes: List<String> = emptyList(),
    val health: String = "",
    /** Wire values of [ExternalDnsReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<ExternalDnsReason> get() = reasons.mapNotNull(ExternalDnsReason::from)
}

/**
 * One hostname a controller is asked to publish, where it comes from and what it should point
 * at. ExternalDNS keeps no status on records: this never says one exists.
 */
@Serializable
data class ExternalDnsRecord(
    val dnsName: String = "",
    /** A, AAAA or CNAME. */
    val recordType: String = "",
    val targets: List<String> = emptyList(),
    /** Seconds, 0 when unset. */
    val ttl: Int = 0,
    /** Ingress, Service, HTTPRoute or DNSEndpoint. */
    val sourceKind: String = "",
    val sourceNamespace: String = "",
    val sourceName: String = "",
    /** Open the source object's summary. */
    val sourceGroup: String = "",
    val sourceVersion: String = "",
    val sourceResource: String = "",
    /** The covering controller's name, "" when none covers it. */
    val controller: String = "",
    val health: String = "",
    /** Wire values of [ExternalDnsReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    /** "from Ingress demo/app": where the hostname comes from. */
    val sourceLabel: String get() = "$sourceKind/$sourceNamespace/$sourceName"
    val reasonList: List<ExternalDnsReason> get() = reasons.mapNotNull(ExternalDnsReason::from)
}

/** Why an ExternalDNS controller or record is not ok, as the Go core names it. */
enum class ExternalDnsReason(val wire: String) {
    NOT_READY("notReady"),
    DRY_RUN("dryRun"),
    STALE("stale"),
    NO_TARGET("noTarget"),
    UNMANAGED("unmanaged"),
    ;

    companion object {
        fun from(wire: String): ExternalDnsReason? = entries.firstOrNull { it.wire == wire }
    }
}
