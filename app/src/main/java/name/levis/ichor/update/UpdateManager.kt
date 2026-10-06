package name.levis.ichor.update

import name.levis.ichor.R
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.content.pm.PackageInstaller
import android.os.Build
import name.levis.ichor.BuildConfig
import name.levis.ichor.data.ChangelogRepository
import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

sealed interface UpdateState {
    data object Idle : UpdateState
    data object Checking : UpdateState
    data object UpToDate : UpdateState
    data class Available(val info: UpdateInfo) : UpdateState
    data class Downloading(val info: UpdateInfo, val progress: Float) : UpdateState
    data class NeedsInstallPermission(val info: UpdateInfo) : UpdateState
    data object Installing : UpdateState
    data class Failed(val message: UiText, val info: UpdateInfo? = null) : UpdateState
}

/**
 * Self-update from GitHub releases ([BuildConfig.UPDATE_REPO]). The APK is verified before
 * installing: SHA-256 against the digest GitHub publishes, and the signing certificate must
 * be exactly the installed app's. Android's installer still asks the user to confirm.
 */
class UpdateManager(
    private val context: Context,
    private val prefs: SharedPreferences,
    private val changelog: ChangelogRepository,
) {
    private val _state = MutableStateFlow<UpdateState>(UpdateState.Idle)
    val state: StateFlow<UpdateState> = _state.asStateFlow()

    private val _autoCheck = MutableStateFlow(prefs.getBoolean(KEY_AUTO, !BuildConfig.DEBUG))
    val autoCheck: StateFlow<Boolean> = _autoCheck.asStateFlow()

    /** Debug builds are signed with a per-machine debug key, so release APKs cannot replace them. */
    val canInstall: Boolean get() = !BuildConfig.DEBUG

    fun setAutoCheck(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_AUTO, enabled).apply()
        _autoCheck.value = enabled
    }

    /** At most once a day, silently; the overview shows a banner when an update exists. */
    fun maybeAutoCheck(scope: CoroutineScope) {
        val last = prefs.getLong(KEY_LAST_CHECK, 0)
        if (!_autoCheck.value || System.currentTimeMillis() - last < DAY_MS) return
        scope.launch { check() }
    }

    suspend fun check() {
        _state.value = UpdateState.Checking
        val info = try {
            val release = withContext(Dispatchers.IO) { fetchLatestRelease() }
            prefs.edit().putLong(KEY_LAST_CHECK, System.currentTimeMillis()).apply()
            release?.let { selectUpdate(it, BuildConfig.VERSION_NAME, Build.SUPPORTED_ABIS.toList()) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.value = UpdateState.Failed(UiText.Res(R.string.update_check_failed, e.uiText()))
            return
        }
        if (info == null) {
            _state.value = UpdateState.UpToDate
            return
        }
        // Offer the update at once; what it brings is added when the changelog published
        // with the release arrives (best effort, at most 10 s).
        _state.value = UpdateState.Available(info)
        val changes = changelog.publishedSinceThisBuild()
        if (changes.isNotEmpty()) {
            _state.update { if (it is UpdateState.Available && it.info == info) UpdateState.Available(info.copy(changes = changes)) else it }
        }
    }

    suspend fun downloadAndInstall(info: UpdateInfo) {
        if (!context.packageManager.canRequestPackageInstalls()) {
            _state.value = UpdateState.NeedsInstallPermission(info)
            return
        }
        try {
            val apk = withContext(Dispatchers.IO) { download(info) }
            withContext(Dispatchers.IO) { verify(apk, info) }
            _state.value = UpdateState.Installing
            withContext(Dispatchers.IO) { install(apk) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.value = UpdateState.Failed(e.uiText(), info)
        }
    }

    /** Called by [InstallResultReceiver] when the installer reports a failure. */
    fun onInstallFailed(message: UiText) {
        _state.value = UpdateState.Failed(UiText.Res(R.string.update_install_failed, message))
    }

    private fun fetchLatestRelease(): GitHubRelease? {
        val connection = open("https://api.github.com/repos/${BuildConfig.UPDATE_REPO}/releases/latest")
        connection.setRequestProperty("Accept", "application/vnd.github+json")
        try {
            return when (val code = connection.responseCode) {
                HttpURLConnection.HTTP_OK -> connection.inputStream.use {
                    TalosJson.decodeFromString(GitHubRelease.serializer(), it.readBytes().decodeToString())
                }
                HttpURLConnection.HTTP_NOT_FOUND -> null // no release published yet
                else -> throw LocalizedException(UiText.Res(R.string.update_err_http, code))
            }
        } finally {
            connection.disconnect()
        }
    }

    private fun download(info: UpdateInfo): File {
        val dir = File(context.cacheDir, "updates").apply { deleteRecursively(); mkdirs() }
        val target = File(dir, "update.apk")
        val connection = open(info.apkUrl)
        try {
            if (connection.responseCode != HttpURLConnection.HTTP_OK) {
                throw LocalizedException(UiText.Res(R.string.update_err_download_http, connection.responseCode))
            }
            val total = connection.contentLengthLong.takeIf { it > 0 } ?: info.apkSize
            connection.inputStream.use { input ->
                target.outputStream().use { output ->
                    val buffer = ByteArray(64 * 1024)
                    var done = 0L
                    while (true) {
                        val n = input.read(buffer)
                        if (n < 0) break
                        output.write(buffer, 0, n)
                        done += n
                        if (done > MAX_APK_BYTES) throw LocalizedException(UiText.Res(R.string.update_err_too_large))
                        if (total > 0) _state.value = UpdateState.Downloading(info, (done.toFloat() / total).coerceIn(0f, 1f))
                    }
                }
            }
        } finally {
            connection.disconnect()
        }
        return target
    }

    private fun verify(apk: File, info: UpdateInfo) {
        info.sha256?.let { expected ->
            val actual = apk.inputStream().use { input ->
                val digest = MessageDigest.getInstance("SHA-256")
                val buffer = ByteArray(64 * 1024)
                while (true) {
                    val n = input.read(buffer)
                    if (n < 0) break
                    digest.update(buffer, 0, n)
                }
                digest.digest().joinToString("") { "%02x".format(it) }
            }
            if (actual != expected) {
                apk.delete()
                throw LocalizedException(UiText.Res(R.string.update_err_checksum))
            }
        }

        val pm = context.packageManager
        val candidate = pm.getPackageArchiveInfo(apk.path, signatureFlags)
            ?: throw LocalizedException(UiText.Res(R.string.update_err_not_apk))
        if (candidate.packageName != context.packageName) {
            throw LocalizedException(UiText.Res(R.string.update_err_other_app, candidate.packageName))
        }
        val installed = pm.getPackageInfo(context.packageName, signatureFlags)
        if (!sameSigners(installed.signerDigests(), candidate.signerDigests())) {
            apk.delete()
            throw LocalizedException(UiText.Res(R.string.update_err_signature))
        }
    }

    private fun install(apk: File) {
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(context.packageName)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
            }
        }
        val sessionId = installer.createSession(params)
        installer.openSession(sessionId).use { session ->
            apk.inputStream().use { input ->
                session.openWrite("update.apk", 0, apk.length()).use { output ->
                    input.copyTo(output)
                    session.fsync(output)
                }
            }
            val callback = PendingIntent.getBroadcast(
                context,
                sessionId,
                Intent(context, InstallResultReceiver::class.java).setPackage(context.packageName),
                // The installer fills in status extras, so the intent must be mutable.
                PendingIntent.FLAG_UPDATE_CURRENT or
                    (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0),
            )
            session.commit(callback.intentSender)
        }
    }

    private fun open(url: String): HttpURLConnection =
        (URL(url).openConnection() as HttpURLConnection).apply {
            connectTimeout = 15_000
            readTimeout = 30_000
            instanceFollowRedirects = true // release assets redirect to GitHub's object storage
            setRequestProperty("User-Agent", "ichor/${BuildConfig.VERSION_NAME}")
        }

    private companion object {
        const val KEY_AUTO = "auto_check"
        const val KEY_LAST_CHECK = "last_check"
        const val DAY_MS = 24 * 60 * 60 * 1000L
        const val MAX_APK_BYTES = 200L * 1024 * 1024
    }
}
