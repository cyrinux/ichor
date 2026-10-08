package name.levis.ichor.model

import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable

/**
 * What an app backup holds once the Go core opened it (Ichorgo.decryptBackup): the
 * talosconfig, the kubeconfig of the clusters added without Talos, and the settings that go
 * with them. The iOS app writes and reads the same JSON,
 * so a backup moves between platforms; a setting one app does not have is left out (null)
 * and the restoring app keeps its current value.
 *
 * Not included: AI diagnosis settings and keys (a third-party service, enabled per device),
 * the app lock and screenshot settings (per-device security), and caches.
 */
@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class BackupPayload(
    /**
     * The payload version, checked by the Go core; bump it for changes older apps cannot read.
     * See [backupFormat]: 2 only when [kubeconfig] is there.
     */
    @EncodeDefault val format: Int = BACKUP_FORMAT,
    val platform: String = "",
    /** Unix seconds. */
    val createdAt: Long = 0,
    /** "" when every cluster came from a kubeconfig (format 2); always written, older apps require it. */
    @EncodeDefault val talosconfig: String = "",
    /** The stored kubeconfig (format 2), null when no cluster was added from one. */
    val kubeconfig: String? = null,
    /** The cluster on screen, by position: screenshot mode masks context names. */
    val activeContextIndex: Int = -1,
    val settings: BackupSettings = BackupSettings(),
    /** Per-cluster options by context fingerprint, which both apps compute alike in the Go core. */
    val clusters: Map<String, BackupCluster> = emptyMap(),
    /**
     * The sign-ins of kubeconfig clusters by fingerprint (format 2), as Ichorgo.kubeAuthForBackup
     * keeps them: the secrets the user entered, never this device's session tokens.
     */
    val kubeAuth: Map<String, String>? = null,
)

@Serializable
data class BackupSettings(
    /** "auto", "light", "dark" or "black". */
    val themeMode: String? = null,
    /** BCP-47 language tag, "" for the system's. Android only. */
    val language: String? = null,
    val liveClusterStats: Boolean? = null,
    /** Download app icons the app does not bundle (a third-party request, off by default). */
    val remoteAppIcons: Boolean? = null,
    val privacyMask: Boolean? = null,
    val privacyMaskWords: String? = null,
    val monitorAlerts: Boolean? = null,
    /** Android only (iOS schedules background checks itself). */
    val monitorIntervalMinutes: Long? = null,
)

@Serializable
data class BackupCluster(
    val name: String? = null,
    /** 0xRRGGBB, as iOS stores it (Android adds the opaque alpha back). */
    val color: Int? = null,
    val vpnOnly: Boolean = false,
    /** By node address, as the talosconfig names it. Android only. */
    val wakeOnLan: Map<String, BackupWolTarget> = emptyMap(),
    /** The Kubernetes API address to use instead of the kubeconfig's. */
    val kubeServer: String? = null,
    /** A Talos cluster's Kubernetes access: the fingerprint of the kubeconfig cluster it goes through. */
    val kubeAccess: String? = null,
)

@Serializable
data class BackupWolTarget(val mac: String, val broadcast: String = "", val port: Int = WOL_DEFAULT_PORT)

const val BACKUP_FORMAT = 1

/** The format that adds [BackupPayload.kubeconfig]. */
const val BACKUP_FORMAT_KUBE = 2

/**
 * The format of a backup holding [kubeconfig]: 2 only with a kubeconfig, so older apps keep
 * reading the backups they can restore whole, and refuse ("update the app") one whose
 * kubeconfig clusters they would silently drop.
 */
fun backupFormat(kubeconfig: String?): Int = if (kubeconfig.isNullOrBlank()) BACKUP_FORMAT else BACKUP_FORMAT_KUBE

/** The per-cluster settings of this device, as a backup stores them (only clusters still in [fingerprints]). */
fun backupClusters(
    fingerprints: List<String>,
    names: Map<String, String>,
    colors: Map<String, Int>,
    vpnOnly: Set<String>,
    wakeOnLan: Map<String, WolTarget>,
    kubeServers: Map<String, String>,
    kubeAccess: Map<String, String> = emptyMap(),
): Map<String, BackupCluster> = fingerprints.filter { it.isNotBlank() }.distinct().associateWith { fp ->
    BackupCluster(
        name = names[fp],
        color = colors[fp]?.and(RGB),
        vpnOnly = fp in vpnOnly,
        wakeOnLan = wakeOnLan.filterKeys { it.substringBefore('|') == fp }
            .mapKeys { (key, _) -> key.substringAfter('|') }
            .mapValues { (_, t) -> BackupWolTarget(t.mac, t.broadcast, t.port) },
        kubeServer = kubeServers[fp],
        kubeAccess = kubeAccess[fp],
    )
}

/** The per-cluster settings to restore, checked like typed ones; invalid entries are dropped. */
data class RestoredClusters(
    val names: Map<String, String>,
    val colors: Map<String, Int>,
    val vpnOnly: Set<String>,
    /** By [wolKey]. */
    val wakeOnLan: Map<String, WolTarget>,
    /** Trimmed; the Go core checks them before they are stored. */
    val kubeServers: Map<String, String>,
    /** Only links between two restored clusters. */
    val kubeAccess: Map<String, String> = emptyMap(),
)

/** [clusters] narrowed to [fingerprints] (the restored config's) and validated. */
fun restoredClusters(clusters: Map<String, BackupCluster>, fingerprints: List<String>): RestoredClusters {
    val known = clusters.filterKeys { it.isNotBlank() && it in fingerprints }
    return RestoredClusters(
        names = known.mapNotNull { (fp, c) -> c.name?.let(::normalizeClusterName)?.let { fp to it } }.toMap(),
        colors = known.mapNotNull { (fp, c) -> c.color?.let { fp to (it or OPAQUE) } }.toMap(),
        vpnOnly = known.filterValues { it.vpnOnly }.keys,
        wakeOnLan = known.flatMap { (fp, c) ->
            c.wakeOnLan.mapNotNull { (node, t) ->
                val target = decodeWolTarget(encodeWolTarget(WolTarget(t.mac, t.broadcast.trim(), t.port)))
                target?.takeIf { node.isNotBlank() && '|' !in t.broadcast }?.let { wolKey(fp, node.trim()) to it }
            }
        }.toMap(),
        kubeServers = known.mapNotNull { (fp, c) -> c.kubeServer?.trim()?.takeIf { it.isNotEmpty() }?.let { fp to it } }.toMap(),
        kubeAccess = known.mapNotNull { (fp, c) -> c.kubeAccess?.takeIf { it != fp && it in fingerprints }?.let { fp to it } }.toMap(),
    )
}

private const val RGB = 0xFFFFFF
private const val OPAQUE = 0xFF shl 24

/** The file name a new backup is offered under, e.g. "ichor-2026-10-02.ichorbackup". */
fun backupFileName(date: java.time.LocalDate): String = "ichor-$date.$BACKUP_EXTENSION"

const val BACKUP_EXTENSION = "ichorbackup"

/** The first bytes of every backup file (backupMagic in the Go core). */
private val BACKUP_MAGIC = "ICHORBAK".toByteArray(Charsets.US_ASCII)

/** Whether [file] starts like a backup: tells one opened from a file manager from any other file. */
fun looksLikeBackup(file: ByteArray): Boolean =
    file.size >= BACKUP_MAGIC.size && BACKUP_MAGIC.indices.all { file[it] == BACKUP_MAGIC[it] }

/**
 * The sign-ins a backup keeps for the kubeconfig clusters [fingerprints], from this device's
 * [states] narrowed by [forBackup] (Ichorgo.kubeAuthForBackup: "" when nothing is worth
 * keeping); null when none is left.
 */
fun backupKubeAuth(fingerprints: List<String>, states: Map<String, String>, forBackup: (String) -> String): Map<String, String>? =
    fingerprints.filter { it.isNotBlank() }.distinct()
        .mapNotNull { fp -> states[fp]?.let { runCatching { forBackup(it) }.getOrNull() }?.takeIf { it.isNotBlank() }?.let { fp to it } }
        .toMap()
        .ifEmpty { null }

/**
 * The keys of the sign-ins [contexts] keep in the auth store: a kubeconfig cluster's
 * fingerprint, an Omni cluster's auth key (one per identity and instance) and, from older
 * cores, its fingerprint (the core moves that sign-in to the auth key on first use).
 */
fun signInKeys(contexts: List<ContextSummary>): List<String> = contexts.flatMap {
    when {
        it.isKube -> listOf(it.fingerprint)
        it.omni -> listOf(it.fingerprint, it.authKey)
        else -> emptyList()
    }
}.filter { it.isNotEmpty() }.distinct()

/** The sign-ins of [kubeAuth] (a backup's) for the restored kubeconfig clusters [fingerprints]. */
fun restoredKubeAuth(kubeAuth: Map<String, String>?, fingerprints: List<String>): Map<String, String> =
    kubeAuth.orEmpty().filter { (fp, state) -> fp.isNotBlank() && fp in fingerprints && state.isNotBlank() }
