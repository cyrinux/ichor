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

internal val signatureFlags: Int
    get() = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
        PackageManager.GET_SIGNING_CERTIFICATES
    } else {
        @Suppress("DEPRECATION")
        PackageManager.GET_SIGNATURES
    }

/** An update may only be installed when it is signed by exactly the installed app's key. */
fun sameSigners(installed: Set<String>, candidate: Set<String>): Boolean =
    installed.isNotEmpty() && installed == candidate

fun sha256Hex(bytes: ByteArray): String =
    MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
