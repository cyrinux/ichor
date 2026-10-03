package name.levis.ichor.model

import androidx.annotation.StringRes
import name.levis.ichor.R

// Issuing a talosconfig through go/ichorgo/issueconfig.go (GenerateTalosconfig).

/** Roles a generated talosconfig may carry (the Go core rejects any other). */
val ISSUABLE_ROLES = listOf("os:admin", "os:operator", "os:reader", "os:etcd:backup")

/** Upper bound accepted by the Go core (10 years). */
const val MAX_CERT_TTL_HOURS = 87_600

enum class CertValidity(@StringRes val label: Int, val hours: Int) {
    DAYS_30(R.string.issue_validity_30_days, 30 * 24),
    DAYS_90(R.string.issue_validity_90_days, 90 * 24),
    YEAR_1(R.string.issue_validity_1_year, 365 * 24),
    ;

    /** When a certificate issued at [nowMillis] expires (epoch millis). */
    fun expiresAt(nowMillis: Long): Long = nowMillis + hours * 3_600_000L

    companion object {
        val DEFAULT = YEAR_1
    }
}

enum class IssueMode { RENEW, OTHER_DEVICE }

/**
 * Roles to renew a certificate with: the current certificate's roles the Go core can issue,
 * in [ISSUABLE_ROLES] order. Empty when none can be reused (the user then picks).
 */
fun renewalRoles(current: List<String>): List<String> = ISSUABLE_ROLES.filter { it in current }

/** Default roles for a config handed to another device: read-only. */
val DEFAULT_SHARED_ROLES = listOf("os:reader")

/** [roles] toggled on/off for [role], kept in [ISSUABLE_ROLES] order. */
fun List<String>.toggled(role: String): List<String> =
    if (role in this) this - role else ISSUABLE_ROLES.filter { it in this || it == role }

/** The comma-separated role list GenerateTalosconfig expects. */
fun rolesArgument(roles: List<String>): String = roles.joinToString(",")

/**
 * Bytes a version-40 QR code holds in byte mode at the lowest error correction level (L);
 * a bigger talosconfig can only be shared as a file.
 */
const val MAX_QR_BYTES = 2_953

/** Whether [yaml] fits in one QR code (UTF-8 bytes, as the QR importer reads them back). */
fun fitsInQr(yaml: String): Boolean = yaml.encodeToByteArray().size <= MAX_QR_BYTES

/** File name suggested when saving a generated config, e.g. "talosconfig-lab-reader.yaml". */
fun issuedFileName(context: String, roles: List<String>): String {
    val safeContext = context.replace(Regex("[^A-Za-z0-9._-]"), "_").ifEmpty { "cluster" }
    val roleSuffix = roles.joinToString("-") { it.removePrefix("os:").replace(':', '-') }
    return if (roleSuffix.isEmpty()) "talosconfig-$safeContext.yaml" else "talosconfig-$safeContext-$roleSuffix.yaml"
}
