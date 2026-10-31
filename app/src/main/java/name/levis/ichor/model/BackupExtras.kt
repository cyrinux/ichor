package name.levis.ichor.model

import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable
import name.levis.ichor.data.AiSettings

/*
 * The parts of a backup added in format 3: pins, Git hosts, AI settings and, only when the
 * user opts in on the export sheet, the API keys and Git tokens. The iOS app writes and reads
 * the same JSON; every field is optional on read and applies only when present.
 */

/** A Git host as a backup keeps it: never its token (that one is in [BackupSecrets], when opted in). */
@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class BackupGitHost(
    val host: String,
    /** The [GitHostKind] wire name. */
    @EncodeDefault val kind: String = GitHostKind.AUTO.wire,
    @EncodeDefault val aliases: List<String> = emptyList(),
)

/** The AI diagnosis settings, without the keys. */
@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class BackupAi(
    val enabled: Boolean = false,
    val provider: String = AiSettings.DEFAULT_PROVIDER,
    @EncodeDefault val models: Map<String, String> = emptyMap(),
    @EncodeDefault val baseUrls: Map<String, String> = emptyMap(),
    @EncodeDefault val anonymize: Boolean = true,
)

/** What only an opted-in backup holds; an empty map is left out (null). */
@Serializable
data class BackupSecrets(
    /** By provider id. */
    val aiKeys: Map<String, String>? = null,
    /** By normalised host. */
    val gitHostTokens: Map<String, String>? = null,
)

/** The app lock as a backup names it: never its secret, a security key's data or the DEK. */
object BackupLockMode {
    const val OFF = "off"
    const val BIOMETRIC = "biometric"
    const val SECURITY_KEY = "securityKey"

    fun of(enabled: Boolean, securityKeys: Boolean): String = when {
        !enabled -> OFF
        securityKeys -> SECURITY_KEY
        else -> BIOMETRIC
    }
}

/** The real-scope pins of [fingerprints] ([pinsOf] reads them); clusters without any are left out, null when none has. */
fun backupPins(fingerprints: List<String>, pinsOf: (String) -> List<Pin>): Map<String, List<Pin>>? =
    fingerprints.filter { it.isNotBlank() }.distinct()
        .associateWith(pinsOf)
        .filterValues { it.isNotEmpty() }
        .ifEmpty { null }

/**
 * The pins to store for each restored cluster [fingerprints], checked by the same parse rules
 * as the stored ones (pinnable only, no repeats, at most [Pins.MAX]); a cluster the backup has
 * none for gets none. Null when the backup has no pins field (an older one): nothing changes.
 */
fun restoredPins(pins: Map<String, List<Pin>>?, fingerprints: List<String>): Map<String, List<Pin>>? = pins?.let { saved ->
    fingerprints.filter { it.isNotBlank() }.distinct()
        .associateWith { fp -> Pins.parse(Pins.encode(saved[fp].orEmpty())) }
}

fun backupGitHosts(hosts: List<GitHost>): List<BackupGitHost>? =
    hosts.map { BackupGitHost(it.host, it.kind.wire, it.aliases) }.ifEmpty { null }

/** The Git hosts of a backup, normalised like typed ones; blank hosts and repeats are dropped. */
fun restoredGitHosts(hosts: List<BackupGitHost>?): List<GitHost> = hosts.orEmpty().mapNotNull { saved ->
    val host = normalizeGitHost(saved.host)
    host.takeIf { it.isNotEmpty() }?.let {
        GitHost(it, GitHostKind.fromWire(saved.kind), parseGitHostAliases(saved.aliases.joinToString(","), it))
    }
}.distinctBy { it.host }

fun backupAi(settings: AiSettings): BackupAi =
    BackupAi(settings.enabled, settings.provider, settings.models, settings.baseUrls, settings.anonymize)

/**
 * [ai] over [current], narrowed to the [providers] this app knows; null when the backup has no
 * AI settings. A file must not send this device's keys or data elsewhere: a provider's base
 * URL is taken only with that provider's key from [secrets] (else this device's stays), and
 * anonymize off only when [secrets] brings keys at all.
 */
fun restoredAi(ai: BackupAi?, providers: List<String>, current: AiSettings, secrets: BackupSecrets? = null): AiSettings? = ai?.let {
    val keyed = restoredAiKeys(secrets, providers).keys
    val urls = it.baseUrls.filter { (p, u) -> p in keyed && u.isNotBlank() }.mapValues { (_, u) -> u.trim() }
    AiSettings(
        enabled = it.enabled,
        provider = it.provider.takeIf { p -> p in providers } ?: current.provider,
        models = it.models.filter { (p, m) -> p in providers && m.isNotBlank() }.mapValues { (_, m) -> m.trim() },
        baseUrls = current.baseUrls.filterKeys { p -> p !in keyed } + urls,
        anonymize = it.anonymize || keyed.isEmpty(),
        // Not in a backup: a file does not decide what the model may read on this device.
        allowTools = current.allowTools,
    )
}

/** The secrets an opted-in backup holds: blank values dropped, null when nothing is left. */
fun backupSecrets(aiKeys: Map<String, String>, gitHostTokens: Map<String, String>): BackupSecrets? {
    val keys = aiKeys.filterValues { it.isNotBlank() }.ifEmpty { null }
    val tokens = gitHostTokens.filterValues { it.isNotBlank() }.ifEmpty { null }
    return if (keys == null && tokens == null) null else BackupSecrets(keys, tokens)
}

/** The API keys of [secrets] for the [providers] this app knows. */
fun restoredAiKeys(secrets: BackupSecrets?, providers: List<String>): Map<String, String> =
    secrets?.aiKeys.orEmpty().filter { (p, k) -> p in providers && k.isNotBlank() }.mapValues { (_, k) -> k.trim() }

/** The Git tokens of [secrets] for the restored [hosts], by normalised host. */
fun restoredGitTokens(secrets: BackupSecrets?, hosts: List<GitHost>): Map<String, String> {
    val known = hosts.map { it.host }.toSet()
    return secrets?.gitHostTokens.orEmpty()
        .mapKeys { (host, _) -> normalizeGitHost(host) }
        .filter { (host, token) -> host in known && token.isNotBlank() }
        .mapValues { (_, token) -> token.trim() }
}
