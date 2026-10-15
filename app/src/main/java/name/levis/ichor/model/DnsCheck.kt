package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/dns_check.go (see DataServices.kt).

/**
 * One hostname to check, as the Go core reads it: the record ExternalDNS is asked to publish,
 * plus the covering controller's owner id and TXT prefix for the ownership check.
 */
@Serializable
data class DnsCheckRequest(
    val dnsName: String,
    val recordType: String,
    val targets: List<String>,
    val ownerId: String = "",
    val txtPrefix: String = "",
)

/** What resolving a hostname from the phone found, as the Go core names it. */
enum class DnsCheckStatus(val wire: String) {
    /** An answer is one of the record's targets. */
    MATCH("match"),

    /** It resolves, but to something else: split-horizon DNS, propagation, another owner. */
    MISMATCH("mismatch"),

    /** It does not resolve at all. */
    MISSING("missing"),

    /** The resolver could not answer. */
    ERROR("error"),
    ;

    companion object {
        fun from(wire: String): DnsCheckStatus? = entries.firstOrNull { it.wire == wire }
    }
}

/**
 * One hostname's verdict. [ownerMatch] is null when no TXT registry record was readable, so
 * the app says "owner unknown" rather than "not ours".
 */
@Serializable
data class DnsCheckEntry(
    val dnsName: String = "",
    /** Wire value of [DnsCheckStatus]. */
    val status: String = "",
    val answers: List<String> = emptyList(),
    /** The ExternalDNS owner id the TXT record names, "" when none was read. */
    val owner: String = "",
    val ownerMatch: Boolean? = null,
    /** How long the lookup took. */
    val ms: Long = 0,
    /** Why it could not be checked, "" otherwise. */
    val message: String = "",
) {
    val verdict: DnsCheckStatus? get() = DnsCheckStatus.from(status)
}

/** The whole check's answer: one entry per record, in the order they were sent. */
@Serializable
data class DnsCheckResult(
    val results: List<DnsCheckEntry> = emptyList(),
    /** "system" or "demo". */
    val resolver: String = "",
)
