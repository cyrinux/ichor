package name.levis.ichor.data

import name.levis.ichor.R
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.PluginRestoreResult
import name.levis.ichor.model.backupFormatFor
import name.levis.ichor.model.restoredPlugins
import name.levis.ichor.model.backupProposals
import name.levis.ichor.model.restoredProposals
import name.levis.ichor.model.BackupPayload
import name.levis.ichor.model.BackupSeal
import name.levis.ichor.model.BackupSecrets
import name.levis.ichor.model.BackupSettings
import name.levis.ichor.model.backupAi
import name.levis.ichor.model.backupClusters
import name.levis.ichor.model.backupGitHosts
import name.levis.ichor.model.backupKubeAuth
import name.levis.ichor.model.backupPins
import name.levis.ichor.model.backupSecrets
import name.levis.ichor.model.restoredAi
import name.levis.ichor.model.restoredAiKeys
import name.levis.ichor.model.restoredClusters
import name.levis.ichor.model.restoredGitHosts
import name.levis.ichor.model.restoredGitTokens
import name.levis.ichor.model.restoredKubeAuth
import name.levis.ichor.model.restoredPins
import name.levis.ichor.model.signInKeys
import name.levis.ichor.monitor.MonitorStore
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichorgo.Ichorgo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer

/**
 * What a restore changed that the screen on display has to act on; [plugins] when the backup
 * carried imported plugins (format 5).
 */
data class RestoreOutcome(val languageChanged: Boolean, val language: String, val plugins: PluginRestoreResult? = null)

/**
 * Backs up the talosconfig, the kubeconfig and the settings that go with them into a file sealed with a
 * passphrase (Argon2id + AES-256-GCM, in the Go core) or as an age file, and restores one, on this device or
 * on another, Android or iOS. The stored config cannot be copied as it is: its key never
 * leaves this device's keystore.
 */
class BackupManager(
    private val configs: ConfigRepository,
    private val ui: UiPreferences,
    private val colors: ClusterColors,
    private val names: ClusterNames,
    private val vpnOnly: VpnOnlyClusters,
    private val kubeServers: KubeServers,
    private val kubeAccess: KubeAccess,
    private val kubeAuth: KubeAuthStore,
    private val wakeOnLan: WakeOnLanStore,
    private val monitor: MonitorStore,
    private val pins: PinStore,
    private val gitHosts: GitHostStore,
    private val ai: AiPreferences,
    private val clusterRepos: ClusterRepoStore,
    /** This device's [name.levis.ichor.model.BackupLockMode]; a restore never sets the lock. */
    private val lockMode: () -> String,
    private val setPrivacyMask: (PrivacyMask) -> Unit,
    private val notificationsAllowed: () -> Boolean,
    private val onRestored: () -> Unit,
    /** The pull requests opened from the phone (format 4); null leaves them out. */
    private val proposals: ProposalsStore? = null,
    /** The imported plugins (format 5); null leaves them out. */
    private val plugins: PluginRepository? = null,
    /** The clusters that record actions; null leaves them out. */
    private val auditEvents: AuditEventClusters? = null,
) {
    /**
     * The backup file of the stored config and settings, sealed as [seal] says (the app's format,
     * or an age file with a passphrase or for age public keys); the API keys and Git tokens only
     * with [includeSecrets] (the payload has no "secrets" key otherwise). The payload is never logged.
     */
    suspend fun create(seal: BackupSeal, includeSecrets: Boolean = false, now: Long = System.currentTimeMillis() / 1000): ByteArray =
        withContext(Dispatchers.Default) {
            val stored = configs.config.value ?: throw NoConfigException()
            val mask = ui.privacyMask.value
            val kubeconfig = stored.kubeYaml.takeIf { it.isNotBlank() }
            val fingerprints = stored.summary.contexts.map { it.fingerprint }
            val kept = proposals?.let { backupProposals(fingerprints, it.proposals.value) }
            val imported = plugins?.export()
            val payload = BackupPayload(
                format = backupFormatFor(kept, imported),
                platform = "android",
                createdAt = now,
                talosconfig = stored.talosYaml,
                kubeconfig = kubeconfig,
                activeContextIndex = configs.activeIndex(),
                settings = BackupSettings(
                    themeMode = ui.themeMode.value.name.lowercase(),
                    language = ui.language.value,
                    liveClusterStats = ui.liveClusterStats.value,
                    remoteAppIcons = ui.remoteAppIcons.value,
                    privacyMask = mask.enabled,
                    privacyMaskWords = mask.extraWords,
                    monitorAlerts = monitor.alertsEnabled.value,
                    monitorIntervalMinutes = monitor.intervalMinutes.value,
                    allowScreenshots = ui.allowScreenshots.value,
                    appLockMode = lockMode(),
                    storageWarnPercent = monitor.storageWarnPercent.value,
                    storageCriticalPercent = monitor.storageCriticalPercent.value,
                ),
                clusters = backupClusters(
                    fingerprints,
                    names.names.value,
                    colors.colors.value,
                    vpnOnly.fingerprints.value,
                    wakeOnLan.targets.value,
                    kubeServers.servers.value,
                    kubeAccess.links.value,
                    clusterRepos.repos.value,
                    auditEvents?.fingerprints?.value.orEmpty(),
                ),
                kubeAuth = backupKubeAuth(signInKeys(stored.summary.contexts), kubeAuth.all(), Ichorgo::kubeAuthForBackup),
                // The real scope only (pinScope with screenshot mode off): masked pins hold made-up names.
                pins = backupPins(fingerprints) { pins.pins(it).value },
                gitHosts = backupGitHosts(gitHosts.hosts.value),
                ai = backupAi(ai.settings.value),
                secrets = if (includeSecrets) secrets() else null,
                proposals = kept,
                plugins = imported,
                fingerprintLineage = backupFingerprintLineage(),
            )
            val json = TalosJson.encodeToString(BackupPayload.serializer(), payload)
            backupCall {
                when (seal) {
                    is BackupSeal.Ichor -> Ichorgo.encryptBackup(json, seal.passphrase)
                    is BackupSeal.AgePassphrase -> Ichorgo.encryptBackupAge(json, seal.passphrase, "")
                    is BackupSeal.AgeRecipients -> Ichorgo.encryptBackupAge(json, "", seal.recipients.joinToString("\n"))
                }
            }
        }

    /**
     * Whether [file] is an age backup sealed for age keys only, which this phone cannot open:
     * tried with an empty passphrase (cheap, no scrypt stanza is opened) before any is asked.
     */
    suspend fun needsIdentity(file: ByteArray): Boolean = withContext(Dispatchers.Default) {
        val isAge = runCatching { Ichorgo.backupFormatOf(file) }.getOrNull() == Ichorgo.BackupFormatAge
        needsAgeIdentity(isAge) { Ichorgo.decryptBackup(file, "") }
    }

    /**
     * Replaces the stored config and settings with those of [file], in either format (the Go core
     * tells them apart). Throws a
     * [BackupPassphraseException] for a wrong passphrase, so the user can try again.
     */
    suspend fun restore(file: ByteArray, passphrase: String): RestoreOutcome {
        val payload = withContext(Dispatchers.Default) {
            val json = backupCall { Ichorgo.decryptBackup(file, passphrase) }
            // Never the parser's message: it can quote the payload, secrets included.
            try {
                TalosJson.decodeFromString(BackupPayload.serializer(), json)
            } catch (_: IllegalArgumentException) {
                throw LocalizedException(UiText.Res(R.string.backup_err_invalid))
            }
        }
        val settings = payload.settings
        // The config first: on failure nothing else changed. Screenshot mode comes after it,
        // its re-parse then reads the restored config (applied before, it could race the write).
        // Both stores in one go: a backup without a kubeconfig removes the clusters added from one.
        // Before the config is parsed: a cluster whose CA was rotated keeps its fingerprint.
        restoreFingerprintLineage(payload.fingerprintLineage)
        configs.replace(payload.talosconfig, payload.kubeconfig.orEmpty(), payload.activeContextIndex)
        val summary = configs.config.value?.summary ?: throw NoConfigException()
        val fingerprints = summary.contexts.map { it.fingerprint }
        // After the configs, as the per-cluster settings: what was signed in on this device stays.
        kubeAuth.restore(restoredKubeAuth(payload.kubeAuth, signInKeys(summary.contexts)))

        // Syncing the stores to the new config already forgot the clusters no longer stored.
        val restored = restoredClusters(payload.clusters, fingerprints)
        restored.colors.forEach { (fp, color) -> colors.set(fp, color) }
        fingerprints.forEach { fp ->
            names.set(fp, restored.names[fp].orEmpty())
            vpnOnly.set(fp, fp in restored.vpnOnly)
            auditEvents?.set(fp, fp in restored.auditEvents)
            // Checked like a typed one: an address the Go core refuses is dropped.
            kubeServers.set(fp, restored.kubeServers[fp]?.let { runCatching { Ichorgo.normalizeKubeServer(it) }.getOrNull() }.orEmpty())
            kubeAccess.set(fp, restored.kubeAccess[fp])
            clusterRepos.set(fp, restored.repos[fp])
        }
        configs.relink()
        (wakeOnLan.targets.value.keys - restored.wakeOnLan.keys)
            .filter { it.substringBefore('|') in fingerprints }
            .forEach { wakeOnLan.set(it.substringBefore('|'), it.substringAfter('|'), null) }
        restored.wakeOnLan.forEach { (key, target) ->
            wakeOnLan.set(key.substringBefore('|'), key.substringAfter('|'), target)
        }
        if (settings.privacyMask != null) {
            setPrivacyMask(PrivacyMask(settings.privacyMask, settings.privacyMaskWords.orEmpty()))
        }

        settings.themeMode?.let { ui.setThemeMode(ThemeMode.parse(it.uppercase())) }
        settings.liveClusterStats?.let(ui::setLiveClusterStats)
        settings.remoteAppIcons?.let(ui::setRemoteAppIcons)
        // Alerts need this device's notification permission; without it they stay off.
        settings.monitorAlerts?.let { monitor.setAlertsEnabled(it && notificationsAllowed()) }
        settings.monitorIntervalMinutes?.takeIf { it in MonitorStore.INTERVALS }?.let(monitor::setIntervalMinutes)
        settings.allowScreenshots?.let(ui::setAllowScreenshots)
        // Warn first: it moves critical above it, then critical lands where the backup had it.
        settings.storageWarnPercent?.let(monitor::setStorageWarnPercent)
        settings.storageCriticalPercent?.let(monitor::setStorageCriticalPercent)
        // settings.appLockMode is never applied: the lock's secret is device-bound. With a real
        // cluster restored and no lock here, the app shows the lock onboarding (lockRequired).
        restoreExtras(payload, fingerprints)
        // Checked by the core as an import is: one this app no longer reads is reported, not stored.
        val pluginsBack = restoredPlugins(payload)?.let { plugins?.restore(it) }
        val language = settings.language?.let(AppLocale::normalize)
        val languageChanged = language != null && language != ui.language.value
        if (languageChanged) ui.setLanguage(language)

        onRestored()
        return RestoreOutcome(languageChanged, language ?: ui.language.value, pluginsBack)
    }

    /**
     * Pins, Git hosts and AI settings (format 3), the proposals (format 4), each only when the
     * backup has it; secrets through their stores. The plugins (format 5) are restored by [restore].
     */
    private fun restoreExtras(payload: BackupPayload, fingerprints: List<String>) {
        restoredPins(payload.pins, fingerprints)?.forEach { (fp, list) -> pins.replace(fp, list) }
        restoredProposals(payload, fingerprints)?.forEach { (fp, list) -> proposals?.replace(fp, list) }
        val hosts = restoredGitHosts(payload.gitHosts)
        val tokens = restoredGitTokens(payload.secrets, hosts)
        // No token in the backup: "" keeps the one this device has for the host.
        hosts.forEach { gitHosts.set(it.host, it.kind, tokens[it.host].orEmpty(), it.aliases) }
        restoredAi(payload.ai, ai.providerIds, ai.settings.value, payload.secrets)?.let(ai::update)
        restoredAiKeys(payload.secrets, ai.providerIds).forEach { (provider, key) -> ai.setApiKey(provider, key) }
    }

    /** This device's API keys and Git tokens, for a backup the user opted in for. */
    private suspend fun secrets(): BackupSecrets? = backupSecrets(
        aiKeys = ai.providerIds.associateWith { ai.apiKey(it) },
        gitHostTokens = gitHosts.hosts.value.associate { it.host to gitHosts.token(it.host) },
    )
}

/**
 * Whether a picked backup says at once that it opens with an age key on a computer: an age file
 * ([isAge]) that [openWithoutPassphrase] (Go DecryptBackup with "") fails to open with
 * backup-needs-identity. Any other outcome (a passphrase file: wrong passphrase) asks the passphrase.
 */
internal inline fun needsAgeIdentity(isAge: Boolean, openWithoutPassphrase: () -> Unit): Boolean {
    if (!isAge) return false
    return try {
        openWithoutPassphrase()
        false
    } catch (e: Exception) {
        e.message.orEmpty().startsWith(Ichorgo.BackupErrNeedsIdentity)
    }
}

/** A backup that does not open with the passphrase given. */
class BackupPassphraseException : LocalizedException(UiText.Res(R.string.backup_err_wrong_passphrase))

private inline fun <T> backupCall(block: () -> T): T = try {
    block()
} catch (e: Exception) {
    throw backupException(e.message.orEmpty()) ?: e
}

/** The localized error for a Go core backup error [message] (prefixed with its code), if it has one. */
internal fun backupException(message: String): LocalizedException? = when {
    message.startsWith(Ichorgo.BackupErrWrongPassphrase) -> BackupPassphraseException()
    message.startsWith(Ichorgo.BackupErrPassphraseShort) ->
        LocalizedException(UiText.Res(R.string.backup_err_passphrase_short, Ichorgo.BackupMinPassphrase.toInt()))
    message.startsWith(Ichorgo.BackupErrNotBackup) -> LocalizedException(UiText.Res(R.string.backup_err_not_backup))
    message.startsWith(Ichorgo.BackupErrUnsupported) -> LocalizedException(UiText.Res(R.string.backup_err_unsupported))
    message.startsWith(Ichorgo.BackupErrInvalidContent) -> LocalizedException(UiText.Res(R.string.backup_err_invalid))
    // An age file for public keys only: the phone holds no age key to open it with.
    message.startsWith(Ichorgo.BackupErrNeedsIdentity) -> LocalizedException(UiText.Res(R.string.backup_err_needs_identity))
    else -> null
}

/** This device's fingerprint lineage for a backup, null when no CA was rotated. Best effort. */
private fun backupFingerprintLineage(): Map<String, String>? = runCatching {
    TalosJson.decodeFromString(LINEAGE, Ichorgo.fingerprintLineageExport())
}.getOrNull()?.takeIf { it.isNotEmpty() }

/** Merges a backup's fingerprint lineage into this device's. Best effort: at worst a rotated cluster's options are not matched. */
private fun restoreFingerprintLineage(lineage: Map<String, String>?) {
    if (lineage.isNullOrEmpty()) return
    runCatching { Ichorgo.fingerprintLineageImport(TalosJson.encodeToString(LINEAGE, lineage)) }
}

private val LINEAGE = MapSerializer(String.serializer(), String.serializer())
