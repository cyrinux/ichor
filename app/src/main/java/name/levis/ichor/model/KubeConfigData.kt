package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_configdata.go: a Secret or ConfigMap key by key. A Secret's values
// come only for the one key asked for (KubeConfigData's key), never in screenshot mode.

/** The first certificate of a PEM value; [count] how many the value holds (a chain). Times in Unix seconds. */
@Serializable
data class ConfigCert(
    val subject: String = "",
    val issuer: String = "",
    val notBefore: Long = 0,
    val notAfter: Long = 0,
    val dnsNames: List<String> = emptyList(),
    val count: Int = 0,
) {
    /** Whole days until it expires, negative once expired (rounded towards the past). */
    fun daysLeft(nowSeconds: Long): Long = Math.floorDiv(notAfter - nowSeconds, 86_400L)

    /** Bad once expired, warn within [CERT_WARN_DAYS] days. */
    fun tone(nowSeconds: Long): CellTone = when {
        notAfter <= nowSeconds -> CellTone.BAD
        daysLeft(nowSeconds) < CERT_WARN_DAYS -> CellTone.WARN
        else -> CellTone.OK
    }

    companion object {
        const val CERT_WARN_DAYS = 30
    }
}

/**
 * One key: its decoded [size] in bytes, what it looks like ([hint]: json, pem, text, binary),
 * and its [value] when [revealed] (base64 when [base64], the value being binary).
 */
@Serializable
data class ConfigKey(
    val key: String = "",
    val size: Long = 0,
    val hint: String = HINT_TEXT,
    val revealed: Boolean = false,
    val value: String = "",
    val base64: Boolean = false,
    val cert: ConfigCert? = null,
) {
    companion object {
        const val HINT_JSON = "json"
        const val HINT_PEM = "pem"
        const val HINT_TEXT = "text"
        const val HINT_BINARY = "binary"
    }
}

/** A registry of a docker config Secret; [username] "" in screenshot mode. Never its password. */
@Serializable
data class ConfigRegistry(val registry: String = "", val username: String = "")

/** A pod of the namespace using the object, and how ([via]: env, envFrom, volume, projected, imagePullSecret). */
@Serializable
data class ConfigUse(val pod: String = "", val via: List<String> = emptyList())

@Serializable
data class KubeConfigData(
    val kind: String = "",
    /** The Secret's type, "" for a ConfigMap. */
    val type: String = "",
    val keys: List<ConfigKey> = emptyList(),
    val registries: List<ConfigRegistry> = emptyList(),
    val usedBy: List<ConfigUse> = emptyList(),
    /** The pods could not be listed (RBAC): [usedBy] says nothing. */
    val usedByUnknown: Boolean = false,
) {
    /** [key] with its value from [revealed] (a read of that one key), the rest as listed. */
    fun withRevealed(revealed: ConfigKey?): List<ConfigKey> =
        if (revealed == null) keys else keys.map { if (it.key == revealed.key) revealed else it }
}

/** The kinds that have a data tab. */
val KubeObjectRef.hasConfigData: Boolean get() = group.isEmpty() && (resource == "secrets" || resource == "configmaps")

/** The kind KubeConfigData takes. */
val KubeObjectRef.configDataKind: String get() = if (isSecret) "Secret" else "ConfigMap"
