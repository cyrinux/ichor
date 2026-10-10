package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt).

@Serializable
data class CertManagerStatus(
    val version: String = "",
    val error: String = "",
    /** Worst first, then the soonest expiry. */
    val certificates: List<Certificate> = emptyList(),
    val issuers: List<CertIssuer> = emptyList(),
)

@Serializable
data class Certificate(
    val namespace: String = "",
    val name: String = "",
    val secretName: String = "",
    /** The common name then the DNS names, the first few; [dnsNameCount] counts them all. */
    val dnsNames: List<String> = emptyList(),
    val dnsNameCount: Int = 0,
    /** "ClusterIssuer/letsencrypt", "Issuer/internal-ca". */
    val issuer: String = "",
    val health: String = "",
    /** Wire values of [CertReason]. */
    val reasons: List<String> = emptyList(),
    val ready: Boolean = false,
    /** cert-manager is issuing it now (a renewal, or one forced from the app). */
    val issuing: Boolean = false,
    /** The Ready condition's message when not ready. */
    val message: String = "",
    /** Unix ms, 0 before the first issuance. */
    val notAfter: Long = 0,
    /** Unix ms, 0 when no renewal is planned. */
    val renewalTime: Long = 0,
    val failedAttempts: Int = 0,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<CertReason> get() = reasons.mapNotNull(CertReason::from)
}

@Serializable
data class CertIssuer(
    /** Issuer or ClusterIssuer. */
    val kind: String = "",
    /** "" for a ClusterIssuer. */
    val namespace: String = "",
    val name: String = "",
    /** acme, ca, selfSigned, vault or venafi; "" for another. */
    val type: String = "",
    /** The ACME server's host. */
    val server: String = "",
    val ready: Boolean = false,
    val message: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)

    /** "ClusterIssuer/letsencrypt", "Issuer/app/internal-ca": unique across both kinds. */
    val label: String get() = listOf(kind, namespace, name).filter { it.isNotEmpty() }.joinToString("/")
}

/** Why a certificate is not ok, as the Go core names it. */
enum class CertReason(val wire: String) {
    EXPIRED("expired"),
    EXPIRING("expiring"),
    RENEWAL_OVERDUE("renewalOverdue"),
    NOT_READY("notReady"),
    ISSUER("issuer"),
    ;

    companion object {
        fun from(wire: String): CertReason? = entries.firstOrNull { it.wire == wire }
    }
}
