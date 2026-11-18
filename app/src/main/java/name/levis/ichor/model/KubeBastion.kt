package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_bastion.go (KubeBastionInfo, KubeBastionHostKey) and the codes of
// go/internal/httpx/bastion.go.

/** A kubeconfig cluster's SSH bastion as the core shows it: never a secret. */
@Serializable
data class KubeBastionInfo(
    val configured: Boolean = false,
    /** host:port of the bastion. */
    val host: String = "",
    val user: String = "",
    /** [AUTH_KEY] or [AUTH_PASSWORD]. */
    val auth: String = "",
    val hostKeyType: String = "",
    /** The pinned host key as SHA256:…, what the user compared before trusting it. */
    val hostKeyFingerprint: String = "",
    /** Why the cluster cannot have a bastion: proxy-url, demo, not-kubeconfig; "" when it can. */
    val unavailable: String = "",
) {
    val usesKey: Boolean get() = auth == AUTH_KEY

    companion object {
        const val AUTH_KEY = "key"
        const val AUTH_PASSWORD = "password"
    }
}

/** The key a bastion presents, read without signing in, for the user to compare and trust. */
@Serializable
data class KubeBastionHostKey(
    val host: String = "",
    val hostKeyType: String = "",
    val hostKeyFingerprint: String = "",
    /** The key as one authorized_keys line: what is sent to pin it. */
    val hostKey: String = "",
)

/** Why a call through the bastion failed, each with its own sentence and the bastion settings as the way out. */
enum class BastionProblem(val code: String) {
    /** The bastion did not answer, or the SSH handshake failed. */
    UNREACHABLE("kube-bastion-unreachable"),

    /** The bastion presented another host key than the pinned one: nothing goes through until it is replaced. */
    HOST_KEY("kube-bastion-host-key"),

    /** The bastion refused the user, the key or the password. */
    AUTH("kube-bastion-auth"),

    /** The private key cannot be read: malformed, or its passphrase missing or wrong. */
    KEY("kube-bastion-key"),

    /** The bastion would not open a connection to the API server (AllowTcpForwarding, PermitOpen). */
    FORWARD("kube-bastion-forward"),
}

/**
 * The bastion problem a failed call reports in its error [message], null for any other failure.
 * The code may follow what wrapped it ("kube nodes: kube-bastion-auth: …"); "kube-bastion-host-key"
 * is not mistaken for another code that shares its start.
 */
fun bastionProblem(message: String?): BastionProblem? {
    if (message.isNullOrEmpty()) return null
    return BastionProblem.entries.firstOrNull { problem ->
        Regex("""(^|[^a-z-])${Regex.escape(problem.code)}(?![a-z-])""").containsMatchIn(message)
    }
}

/** The fields sent to the core to set or edit a bastion; an empty field keeps the stored value. */
data class BastionFields(
    val host: String = "",
    val user: String = "",
    val privateKey: String = "",
    val passphrase: String = "",
    val password: String = "",
    /** A host key line from [KubeBastionHostKey.hostKey]: trusts (or replaces) the pin. */
    val hostKey: String = "",
) {
    /** The JSON object of non-empty fields KubeSetBastion takes. */
    fun toMap(): Map<String, String> = mapOf(
        "host" to host.trim(),
        "user" to user.trim(),
        "privateKey" to privateKey,
        "passphrase" to passphrase,
        "password" to password,
        "hostKey" to hostKey,
    ).filterValues { it.isNotEmpty() }
}

/** Whether [summary] may be given a bastion: a kubeconfig cluster, not the demo, without a proxy-url. */
fun bastionOffered(summary: ContextSummary, masked: Boolean): Boolean =
    summary.isKube && !summary.demo && !masked && summary.proxyURL.isEmpty()
