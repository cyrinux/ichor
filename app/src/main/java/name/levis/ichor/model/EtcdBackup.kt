package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/** Members that can serve a snapshot: queried successfully and reporting no errors. */
fun snapshotCandidates(statuses: List<EtcdNodeStatus>): List<EtcdNodeStatus> =
    statuses.filter { it.error == null && it.errors.isEmpty() && it.memberId.isNotEmpty() }
        // Followers first (the snapshot loads the member serving it), learners and the leader last.
        .sortedWith(compareBy<EtcdNodeStatus> { it.isLeader }.thenBy { it.isLearner }.thenBy { it.node })

/**
 * `etcd-<context>-<hostname>-<yyyyMMdd-HHmm>.snapshot`, with unsafe file name characters
 * replaced; `.snapshot.age` when [encrypted].
 */
fun snapshotFileName(
    context: String,
    hostname: String,
    now: Date = Date(),
    zone: TimeZone = TimeZone.getDefault(),
    encrypted: Boolean = false,
): String {
    val stamp = SimpleDateFormat("yyyyMMdd-HHmm", Locale.ROOT).apply { timeZone = zone }.format(now)
    fun safe(part: String) = part.replace(Regex("[^A-Za-z0-9._-]+"), "-").trim('-').ifEmpty { "unknown" }
    return "etcd-${safe(context)}-${safe(hostname)}-$stamp.snapshot" + if (encrypted) ".age" else ""
}

/** How a snapshot is protected (plans/etcd-encrypted-snapshot): age public keys, an age passphrase, or nothing. */
sealed interface SnapshotEncryption {
    val mode: SnapshotMode

    /** Public keys, one per line (age or SSH), already checked by Go. */
    data class Keys(val recipients: String) : SnapshotEncryption {
        override val mode get() = SnapshotMode.KEYS
    }

    class Passphrase(val passphrase: String) : SnapshotEncryption {
        override val mode get() = SnapshotMode.PASSPHRASE
        override fun toString() = "Passphrase(***)"
    }

    data object None : SnapshotEncryption {
        override val mode get() = SnapshotMode.NONE
    }
}

enum class SnapshotMode { KEYS, PASSPHRASE, NONE }

/** One public key a snapshot is encrypted for, as Ichorgo.checkSnapshotRecipients describes it. */
@Serializable
data class SnapshotRecipient(val type: String = "", val comment: String = "")

/**
 * Shell commands restoring [fileName] on a Unix machine: decrypt with age (unless [mode] is
 * NONE), check the SHA-256 of the clear snapshot, then recover the cluster with talosctl.
 */
fun snapshotRestoreCommands(fileName: String, mode: SnapshotMode, sha256: String): String {
    val file = shellQuote(fileName)
    val clear = if (mode == SnapshotMode.NONE) file else "etcd.snapshot"
    val decrypt = when (mode) {
        SnapshotMode.KEYS -> listOf(
            "# install age: apt install age | dnf install age | pacman -S age | brew install age",
            "# with the private key matching one of the public keys (SSH key or age identity file):",
            "age -d -i ~/.ssh/id_ed25519 -o etcd.snapshot $file",
            "# YubiKey (age1tag1 key): plug it in, install age-plugin-yubikey, use its identity file:",
            "# age -d -i age-yubikey-identity-XXXXXXXX.txt -o etcd.snapshot $file",
        )
        SnapshotMode.PASSPHRASE -> listOf(
            "# install age: apt install age | dnf install age | pacman -S age | brew install age",
            "# asks for the passphrase:",
            "age -d -o etcd.snapshot $file",
        )
        SnapshotMode.NONE -> emptyList()
    }
    return (
        decrypt + listOf(
            "sha256sum $clear  # expect $sha256",
            "talosctl -n <control-plane-ip> bootstrap --recover-from=./$clear",
        )
    ).joinToString("\n")
}

private fun shellQuote(text: String): String =
    if (text.matches(Regex("[A-Za-z0-9._@%+=:,/-]+"))) text else "'" + text.replace("'", "'\\''") + "'"
