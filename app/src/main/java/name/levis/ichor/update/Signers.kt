package name.levis.ichor.update

import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.os.Build
import java.security.MessageDigest

/** SHA-256 digests of the signing certificates of an installed package or an APK file. */
internal fun PackageInfo.signerDigests(): Set<String> {
    val certs = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
        signingInfo?.apkContentsSigners
    } else {
        @Suppress("DEPRECATION")
        signatures
    }
    return certs.orEmpty().map { sha256Hex(it.toByteArray()) }.toSet()
}

/**
 * SHA-256 digests of the certificates an APK's current signer was rotated from (APK Signature
 * Scheme v3 lineage, API 28+), its current one included; empty below API 28 or with several
 * signers (no lineage then).
 */
internal fun PackageInfo.signerLineage(): Set<String> {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.P) return emptySet()
    val info = signingInfo ?: return emptySet()
    if (info.hasMultipleSigners()) return emptySet()
    return info.signingCertificateHistory.orEmpty().map { sha256Hex(it.toByteArray()) }.toSet()
}

internal val signatureFlags: Int
    get() = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
        PackageManager.GET_SIGNING_CERTIFICATES
    } else {
        @Suppress("DEPRECATION")
        PackageManager.GET_SIGNATURES
    }

/**
 * An update may only be installed when it is signed by exactly the installed app's key, or by
 * a key rotated from it: a rotated key carries the old one in its lineage ([candidateLineage],
 * read from the candidate APK, never from the installed app). A lineage only exists for a
 * single signer, so several installed signers need the exact match.
 */
fun sameSigners(installed: Set<String>, candidate: Set<String>, candidateLineage: Set<String> = emptySet()): Boolean =
    installed.isNotEmpty() && (installed == candidate || (installed.size == 1 && installed.single() in candidateLineage))

fun sha256Hex(bytes: ByteArray): String =
    MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
