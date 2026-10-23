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
 * Format 3 adds pins, Git hosts, AI settings, the screenshot setting, the storage thresholds
 * and the app-lock mode (never its secret); API keys and Git tokens only in [secrets], when the
 * user opts in. Not included: the lock secret and security-key data (device-bound), the audit
 * log, and caches.
 */
@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class BackupPayload(
    /**
     * The payload version, checked by the Go core; bump it for changes older apps cannot read.
     * [backupFormatFor] when written (3; 4 with proposals; 5 with plugins); 1 to 5 are read.
     */
    @EncodeDefault val format: Int = BACKUP_FORMAT_SETTINGS,
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
    /** Real-scope pins by fingerprint (format 3), only of clusters in the backup. */
    val pins: Map<String, List<Pin>>? = null,
    /** Git hosts without their tokens (format 3). */
    val gitHosts: List<BackupGitHost>? = null,
    /** AI diagnosis settings without the keys (format 3). */
    val ai: BackupAi? = null,
    /** Only with "Include API keys and Git tokens"; otherwise absent from the JSON. */
    val secrets: BackupSecrets? = null,
    /** The pull requests opened from the phone, per cluster fingerprint (format 4, only when there are some). */
    val proposals: Map<String, List<GitProposalRecord>>? = null,
    /** The imported plugins (format 5, only when there are some): Ichorgo.pluginUserExport's answer. */
    val plugins: BackupPlugins? = null,
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
    /** The "Allow screenshots" switch (format 3). */
    val allowScreenshots: Boolean? = null,
    /** A [BackupLockMode]: restoring never turns a lock on, the app offers to set it up. */
    val appLockMode: String? = null,
    val storageWarnPercent: Int? = null,
    val storageCriticalPercent: Int? = null,
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
    /** The cluster's own Git repository (Settings → cluster → Cluster repository); never a token. */
    val repository: BackupClusterRepo? = null,
    /** "Record actions in the cluster" (a Kubernetes Event per change); absent in older backups: off. */
    val auditEvents: Boolean = false,
)

@Serializable
data class BackupClusterRepo(val repoURL: String, val branch: String = "", val talosDir: String = "", val kubernetesDir: String = "")

@Serializable
data class BackupWolTarget(val mac: String, val broadcast: String = "", val port: Int = WOL_DEFAULT_PORT)

/**
 * The newest format this app reads and writes: 3 adds pins, Git hosts, AI settings and the
 * opt-in secrets; 4 the pull requests opened from the phone; 5 the imported plugins. Older apps refuse a newer format
 * ("update the app") rather than silently drop what it holds, so a backup is written in the
 * lowest format its content needs (see [backupFormatFor]).
 */
const val BACKUP_FORMAT = 5

/** The format that added pins, Git hosts, AI settings and the secrets. */
const val BACKUP_FORMAT_SETTINGS = 3

/** The format that added [BackupPayload.proposals]. */
const val BACKUP_FORMAT_PROPOSALS = 4

/**
 * The format that added [BackupPayload.plugins]: not 4, which an app already released reads
 * while ignoring the plugins, so a backup with plugins is refused there ("update the app").
 */
const val BACKUP_FORMAT_PLUGINS = 5

/**
 * The format a backup holding [proposals] and [plugins] is written in: 5 with plugins, 4 with
 * proposals only, else 3, so a backup opens in every app that can restore all it holds.
 */
fun backupFormatFor(proposals: Map<String, List<GitProposalRecord>>?, plugins: BackupPlugins? = null): Int = when {
    !plugins?.plugins.isNullOrEmpty() -> BACKUP_FORMAT_PLUGINS
    !proposals.isNullOrEmpty() -> BACKUP_FORMAT_PROPOSALS
    else -> BACKUP_FORMAT_SETTINGS
}

/** The proposals of the clusters still in [fingerprints]; null when none has any. */
fun backupProposals(fingerprints: List<String>, all: Map<String, List<GitProposalRecord>>): Map<String, List<GitProposalRecord>>? =
    all.filterKeys { it in fingerprints }.filterValues { it.isNotEmpty() }.takeIf { it.isNotEmpty() }

/** The proposals of a restored [payload] for the clusters it restored ([fingerprints]); null before format 4. */
fun restoredProposals(payload: BackupPayload, fingerprints: List<String>): Map<String, List<GitProposalRecord>>? =
    payload.proposals?.takeIf { payload.format >= BACKUP_FORMAT_PROPOSALS }
        ?.filterKeys { it in fingerprints }
        ?.mapValues { (_, list) -> list.take(MAX_PROPOSALS) }

/** The format that added [BackupPayload.kubeconfig]. */
const val BACKUP_FORMAT_KUBE = 2

/** The per-cluster settings of this device, as a backup stores them (only clusters still in [fingerprints]). */
fun backupClusters(
    fingerprints: List<String>,
    names: Map<String, String>,
    colors: Map<String, Int>,
    vpnOnly: Set<String>,
    wakeOnLan: Map<String, WolTarget>,
    kubeServers: Map<String, String>,
    kubeAccess: Map<String, String> = emptyMap(),
    repos: Map<String, ClusterRepo> = emptyMap(),
    auditEvents: Set<String> = emptySet(),
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
        repository = repos[fp]?.let { BackupClusterRepo(it.repoURL, it.branch, it.talosDir, it.kubernetesDir) },
        auditEvents = fp in auditEvents,
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
    /** Normalized; one without an address is dropped. */
    val repos: Map<String, ClusterRepo> = emptyMap(),
    /** The clusters that record actions. */
    val auditEvents: Set<String> = emptySet(),
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
        repos = known.mapNotNull { (fp, c) ->
            c.repository?.let { normalizeClusterRepo(ClusterRepo(it.repoURL, it.branch, it.talosDir, it.kubernetesDir)) }?.let { fp to it }
        }.toMap(),
        auditEvents = known.filterValues { it.auditEvents }.keys,
    )
}

private const val RGB = 0xFFFFFF
private const val OPAQUE = 0xFF shl 24

/** The file name a new backup in the app's own format is offered under, e.g. "ichor-2026-10-02.ichorbackup". */
fun backupFileName(date: java.time.LocalDate): String = backupFileName(date, BackupFileFormat.ICHOR)

const val BACKUP_EXTENSION = "ichorbackup"

/** The first bytes of every backup file (backupMagic in the Go core). */
private val BACKUP_MAGIC = "ICHORBAK".toByteArray(Charsets.US_ASCII)

/** The first line of every age file (an age backup, or any other), binary or ASCII-armored. */
private val AGE_MAGICS = listOf("age-encryption.org/v1\n", "-----BEGIN AGE ENCRYPTED FILE-----").map { it.toByteArray(Charsets.US_ASCII) }

/**
 * Whether [file] starts like a backup, in the app's format or as an age file: tells one opened
 * from a file manager from any other file (the Go core tells an age backup from another age file).
 */
fun looksLikeBackup(file: ByteArray): Boolean = file.startsWith(BACKUP_MAGIC) || AGE_MAGICS.any { file.startsWith(it) }

private fun ByteArray.startsWith(prefix: ByteArray): Boolean = size >= prefix.size && prefix.indices.all { this[it] == prefix[it] }

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

/**
 * Whether removing the cluster [name] signs it out too. Not an Omni cluster whose sign-in
 * another stored cluster shares (one key per identity and instance): that one keeps it, and
 * the store drops the key once no cluster names it.
 */
fun signsOutOnRemoval(contexts: List<ContextSummary>, name: String): Boolean {
    val removed = contexts.firstOrNull { it.name == name } ?: return false
    if (removed.signIn.isEmpty()) return false
    if (!removed.omni || removed.authKey.isEmpty()) return true
    return contexts.none { it.name != name && it.authKey == removed.authKey }
}

/** The sign-ins of [kubeAuth] (a backup's) for the restored kubeconfig clusters [fingerprints]. */
fun restoredKubeAuth(kubeAuth: Map<String, String>?, fingerprints: List<String>): Map<String, String> =
    kubeAuth.orEmpty().filter { (fp, state) -> fp.isNotBlank() && fp in fingerprints && state.isNotBlank() }
