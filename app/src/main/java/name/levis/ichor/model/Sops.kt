package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/sops.go.

/** A key a SOPS file is encrypted for: [type] age, pgp, kms, gcp_kms, azure_kv or hc_vault. */
@Serializable
data class SopsRecipient(val type: String = "", val id: String = "") {
    /** The id cut for a chip: an age key or a fingerprint by its ends, a URL by its last part. */
    val short: String
        get() = when {
            id.length <= SHORT_ID -> id
            type == "age" || type == "pgp" -> id.take(SHORT_END + 4) + "…" + id.takeLast(SHORT_END)
            else -> "…" + id.takeLast(SHORT_ID)
        }

    private companion object {
        const val SHORT_ID = 20
        const val SHORT_END = 6
    }
}

/**
 * A file's SOPS metadata, never its values: who it is encrypted for, how many values are
 * encrypted, and the ones meant to be that are still in clear (a half-encrypted file).
 */
@Serializable
data class SopsInfo(
    val encrypted: Boolean = false,
    /** yaml, json, dotenv or binary. */
    val format: String = "",
    val recipients: List<SopsRecipient> = emptyList(),
    val lastModified: String = "",
    val encryptedRegex: String = "",
    val encryptedKeys: Int = 0,
    val clearKeys: List<String> = emptyList(),
)

/** The .sops.yaml creation rule that covers a path, [matched] false when none does. */
@Serializable
data class SopsRule(
    val matched: Boolean = false,
    val pathRegex: String = "",
    val recipients: List<SopsRecipient> = emptyList(),
    val encryptedRegex: String = "",
    val unencryptedRegex: String = "",
)

/** An edit that would commit a clear-text secret ([what]) where [rule] encrypts. */
@Serializable
data class SopsWarning(val warn: Boolean = false, val what: String = "", val rule: SopsRule = SopsRule()) {
    /** [what] is a Kubernetes Secret (the apps word it). */
    val isSecret: Boolean get() = what == "Secret"

    /** [what] is a Talos secrets bundle (the apps word it); else a Talos key shown as is. */
    val isTalosBundle: Boolean get() = what == "TalosSecrets"
}

// One line at most: an ENC[ in ordinary text must not swallow the lines after it.
private val encValue = Regex("""ENC\[[^\]\n]*]""")

/** [content] with every SOPS-encrypted value shown as ENC[…]: the ciphertext tells nothing. */
fun collapseSopsValues(content: String): String = encValue.replace(content, "ENC[…]")
