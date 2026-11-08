package name.levis.ichor.model

/** The file a backup is written as: the app's own format (the default) or a standard age file. */
enum class BackupFileFormat(val extension: String) {
    ICHOR(BACKUP_EXTENSION),
    AGE("age"),
}

/** How a new backup is sealed: the passphrase or the age public keys it opens with. */
sealed interface BackupSeal {
    val format: BackupFileFormat

    /** The app's own format (Argon2id + AES-256-GCM). */
    data class Ichor(val passphrase: String) : BackupSeal {
        override val format get() = BackupFileFormat.ICHOR
        override fun toString() = "Ichor(***)"
    }

    /** An age file with a passphrase (scrypt recipient): `age -d` asks for it, the app restores it. */
    data class AgePassphrase(val passphrase: String) : BackupSeal {
        override val format get() = BackupFileFormat.AGE
        override fun toString() = "AgePassphrase(***)"
    }

    /** An age file for [recipients] (age1… public keys): only their age keys open it, on a computer. */
    data class AgeRecipients(val recipients: List<String>) : BackupSeal {
        override val format get() = BackupFileFormat.AGE
    }
}

/** The file name a new backup is offered under, e.g. "ichor-2026-10-02.age". */
fun backupFileName(date: java.time.LocalDate, format: BackupFileFormat): String = "ichor-$date.${format.extension}"

/** The characters of a bech32 data part (BIP 173). */
private const val BECH32_CHARSET = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
private const val AGE_HRP = "age"

/** An X25519 recipient: "age1" and 58 bech32 characters (32-byte key + 6-character checksum). */
private const val AGE_RECIPIENT_LENGTH = 62
private val BECH32_GENERATOR = intArrayOf(0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3)

/**
 * Whether [line] is an age X25519 public key: "age1" and 58 bech32 characters, all lower or all
 * upper case, with a valid checksum. Plugin keys (age1tag1…, age1yubikey1…), post-quantum
 * (age1pq1…), SSH and secret keys are refused.
 */
fun isAgeRecipient(line: String): Boolean {
    if (line.length != AGE_RECIPIENT_LENGTH || (line != line.lowercase() && line != line.uppercase())) return false
    val key = line.lowercase()
    if (!key.startsWith("${AGE_HRP}1")) return false
    val data = key.substring(AGE_HRP.length + 1).map { BECH32_CHARSET.indexOf(it) }
    return data.none { it < 0 } && bech32Polymod(hrpExpanded(AGE_HRP) + data) == 1
}

private fun hrpExpanded(hrp: String): List<Int> = hrp.map { it.code shr 5 } + 0 + hrp.map { it.code and 31 }

private fun bech32Polymod(values: List<Int>): Int = values.fold(1) { chk, v ->
    val top = chk ushr 25
    BECH32_GENERATOR.indices.fold(((chk and 0x1ffffff) shl 5) xor v) { acc, i ->
        if ((top shr i) and 1 == 1) acc xor BECH32_GENERATOR[i] else acc
    }
}

/** Whether [line] (trimmed) holds no key: blank, or a "#" comment as in an age recipients file. */
private fun isSkippedRecipientLine(line: String): Boolean = line.isEmpty() || line.startsWith("#")

/** The recipients of [text], one per line; blank and "#" comment lines skipped, spaces around each trimmed. */
fun ageRecipientLines(text: String): List<String> = text.lines().map { it.trim() }.filterNot(::isSkippedRecipientLine)

/** Why [text] cannot seal a backup: the 1-based number of its first line that is no age key, or null. */
fun firstInvalidAgeRecipientLine(text: String): Int? =
    text.lines().map { it.trim() }.withIndex()
        .firstOrNull { (_, line) -> !isSkippedRecipientLine(line) && !isAgeRecipient(line) }?.index?.plus(1)
