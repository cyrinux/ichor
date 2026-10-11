package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/meta.go.

/** A key of a node's META partition: boot-time values Talos reads before the machine config. */
@Serializable
data class MetaKey(
    val key: Int,
    val hex: String = "",
    /** Empty for a key Talos does not define. */
    val name: String = "",
    /** The text; the bytes in hex when [binary]; empty when [secret]. */
    val value: String = "",
    val size: Int = 0,
    val binary: Boolean = false,
    val secret: Boolean = false,
    /** The API accepts a write or delete of this key. */
    val writable: Boolean = false,
) {
    /** Only a writable text value can be edited as text. */
    val editable: Boolean get() = writable && !binary && !secret
}

@Serializable
data class MetaKeyList(
    val keys: List<MetaKey> = emptyList(),
    /** The node has read its META partition. */
    val loaded: Boolean = false,
    /** The keys Talos defines, for the "add a key" picker. */
    val known: List<MetaKey> = emptyList(),
)

/** A key typed in hex ("0x0a", "0A", "a"), or null when it is not one from 0x00 to 0xff. */
fun parseMetaKey(typed: String): Int? =
    typed.trim().removePrefix("0x").removePrefix("0X").takeIf { it.isNotEmpty() && it.length <= 2 }?.toIntOrNull(16)

/** The key as Talos names it: "0x0a". */
fun metaHex(key: Int): String = "0x%02x".format(key)
