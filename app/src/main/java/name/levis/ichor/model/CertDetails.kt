package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * What explains a cert-manager certificate's state (KubeCertManagerDetails): its conditions,
 * its latest requests (the newest first) with their ACME orders and challenges, the events of
 * all of them (the newest first) and the controller log lines naming them (oldest first).
 */
@Serializable
data class CertDetails(
    val conditions: List<CertCondition> = emptyList(),
    val requests: List<CertRequestDetail> = emptyList(),
    val events: List<CertEvent> = emptyList(),
    val log: List<String> = emptyList(),
    /** What could not be read; the rest is still there. */
    val error: String = "",
)

@Serializable
data class CertCondition(
    val type: String = "",
    /** "True", "False" or "Unknown". */
    val status: String = "",
    val reason: String = "",
    val message: String = "",
    /** Unix ms of the last transition, 0 when unknown. */
    val time: Long = 0,
) {
    val isTrue: Boolean get() = status == "True"
}

@Serializable
data class CertRequestDetail(
    val name: String = "",
    /** Unix ms. */
    val created: Long = 0,
    val conditions: List<CertCondition> = emptyList(),
    val orders: List<AcmeOrder> = emptyList(),
)

@Serializable
data class AcmeOrder(
    val name: String = "",
    /** pending, ready, valid, invalid, errored…; "" before the first sync. */
    val state: String = "",
    val reason: String = "",
    val challenges: List<AcmeChallenge> = emptyList(),
)

@Serializable
data class AcmeChallenge(
    val name: String = "",
    /** HTTP-01 or DNS-01. */
    val type: String = "",
    val dnsName: String = "",
    val wildcard: Boolean = false,
    val state: String = "",
    /** Why it is not valid yet, e.g. the HTTP status cert-manager's self check got. */
    val reason: String = "",
    val presented: Boolean = false,
)

@Serializable
data class CertEvent(
    /** Unix ms of the last occurrence. */
    val time: Long = 0,
    /** Normal or Warning. */
    val type: String = "",
    val reason: String = "",
    val message: String = "",
    /** "CertificateRequest/site-1". */
    val `object`: String = "",
    val count: Int = 1,
) {
    val warning: Boolean get() = type == "Warning"
}

/** An ACME order or challenge state that will not succeed without a change. */
fun acmeFailed(state: String): Boolean = state == "invalid" || state == "errored" || state == "expired"
