package name.levis.ichor

import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.WindowManager
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.Surface
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.core.content.IntentCompat
import androidx.fragment.app.FragmentActivity
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.security.LockOnboarding
import name.levis.ichor.security.LockScreen
import name.levis.ichor.security.lockRequired
import name.levis.ichor.data.ConfigUnreadableException
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.seedOf
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.Navigation
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.debug.DebugShellService
import name.levis.ichor.ui.importconfig.ConfigUnreadableScreen
import name.levis.ichor.ui.debug.LiveShell
import name.levis.ichor.ui.debug.shellKey
import name.levis.ichor.ui.theme.TalosTheme
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch

// FragmentActivity (still a ComponentActivity) is required by BiometricPrompt.
class MainActivity : FragmentActivity() {
    /** A screen to open from a notification tap, consumed once by Navigation. */
    private val deepLink = MutableStateFlow<DeepLink?>(null)

    /** The fingerprint of a cluster to show, from a launcher shortcut; consumed once by Navigation. */
    private val openCluster = MutableStateFlow<String?>(null)

    /** A backup file opened from another app (a file manager), consumed once read. */
    private val backupFile = MutableStateFlow<Uri?>(null)

    /** A debug shell to go back to, from its notification; consumed once by Navigation. */
    private val openShell = MutableStateFlow<LiveShell?>(null)

    /** A share link (its URL, checked by Navigation once the config is loaded), consumed once. */
    private val openTarget = MutableStateFlow<String?>(null)

    // Below API 33, the in-app language is applied here (API 33+ uses LocaleManager).
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        // Edge-to-edge from the first frame (and what Play's check looks for): system-theme
        // bars until the composition below restyles them for the app theme.
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT),
        )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            // No grey scrim behind 3-button navigation: true black stays black.
            window.isNavigationBarContrastEnforced = false
        }
        super.onCreate(savedInstanceState)
        val app = application as TalosApp

        // With the lock on, keep cluster data out of the recents thumbnail and screenshots,
        // unless the user explicitly allowed screenshots.
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.CREATED) {
                combine(app.appLock.enabled, app.uiPreferences.allowScreenshots) { lock, allow -> lock && !allow }
                    .collect { secure ->
                    if (secure) window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
                    else window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
                }
            }
        }

        // Reopened from Recents, the task's first intent comes again: already handled then.
        val fromHistory = intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0
        if (savedInstanceState == null && !fromHistory) {
            deepLink.value = intent.deepLink()
            openCluster.value = intent.clusterFingerprint()
            backupFile.value = intent.backupFile()
            openShell.value = intent.debugShell()
            openTarget.value = intent.shareLink()
        }
        if (BuildConfig.SELF_UPDATE) app.updateManager.maybeAutoCheck(lifecycleScope)
        else app.storeUpdater.attach(this)
        if (BuildConfig.FEATURE_FUNDING) {
            lifecycleScope.launch { lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { app.featureStore.finishPurchases() } }
        }
        if ((BuildConfig.DONATIONS || BuildConfig.FEATURE_FUNDING) && savedInstanceState == null) app.supportPrompt.onLaunch()

        setContent {
            val themeMode by app.uiPreferences.themeMode.collectAsStateWithLifecycle()
            val dark = themeMode.isDark(isSystemInDarkTheme())
            // System bars follow the app theme (not just the system one): transparent over
            // the app background, with icons contrasting with it.
            DisposableEffect(dark) {
                val style = if (dark) {
                    SystemBarStyle.dark(Color.TRANSPARENT)
                } else {
                    SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                }
                enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
                onDispose {}
            }
            // The palette follows the cluster on screen (its main color, see ClusterColors).
            val stored by app.configRepository.config.collectAsStateWithLifecycle()
            val clusterColors by app.clusterColors.colors.collectAsStateWithLifecycle()
            TalosTheme(themeMode, seed = clusterColors.seedOf(stored?.activeSummary)) {
                Surface {
                    LockGate(app, LaunchTargets(deepLink, openCluster, backupFile, openShell, openTarget), onWiped = ::recreate)
                }
            }
        }
    }

    // A notification, shortcut or file opened while the activity is kept (otherwise onCreate reads the intent).
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        intent.deepLink()?.let { deepLink.value = it }
        intent.clusterFingerprint()?.let { openCluster.value = it }
        intent.backupFile()?.let { backupFile.value = it }
        intent.debugShell()?.let { openShell.value = it }
        intent.shareLink()?.let { openTarget.value = it }
    }

    companion object {
        const val EXTRA_OPEN = "name.levis.ichor.OPEN"

        /** The fingerprint of the cluster a launcher shortcut opens. */
        const val EXTRA_CLUSTER = "name.levis.ichor.CLUSTER"

        /** The debug shell a notification opens: its cluster (context), node and hostname, or its pod. */
        const val EXTRA_SHELL_CONTEXT = "name.levis.ichor.SHELL_CONTEXT"
        const val EXTRA_SHELL_NODE = "name.levis.ichor.SHELL_NODE"
        const val EXTRA_SHELL_HOST = "name.levis.ichor.SHELL_HOST"
        const val EXTRA_SHELL_NAMESPACE = "name.levis.ichor.SHELL_NAMESPACE"
        const val EXTRA_SHELL_POD = "name.levis.ichor.SHELL_POD"
        const val EXTRA_SHELL_CONTAINER = "name.levis.ichor.SHELL_CONTAINER"
    }
}

/** What the intent that launched the app asks to show: a screen, a cluster, a backup to restore. */
private class LaunchTargets(
    val deepLink: MutableStateFlow<DeepLink?>,
    val cluster: MutableStateFlow<String?>,
    val backupFile: MutableStateFlow<Uri?>,
    val shell: MutableStateFlow<LiveShell?>,
    val shareLink: MutableStateFlow<String?>,
)

private fun Intent.debugShell(): LiveShell? {
    if (action != DebugShellService.ACTION_OPEN) return null
    val key = shellKey() ?: return null
    return LiveShell(key, getStringExtra(MainActivity.EXTRA_SHELL_HOST)?.takeIf { it.isNotBlank() } ?: key.node.ifEmpty { key.pod })
}

/** The URL of a share link (ichor://open, or the website page forwarding to it), not yet checked. */
private fun Intent.shareLink(): String? {
    val uri = data ?: return null
    val ours = uri.scheme == "ichor" && uri.host == "open" ||
        uri.scheme == "https" && uri.host == "cyrinux.github.io" && uri.path.orEmpty().startsWith("/ichor/open")
    return uri.toString().takeIf { action == Intent.ACTION_VIEW && ours }
}

private fun Intent.clusterFingerprint(): String? = getStringExtra(MainActivity.EXTRA_CLUSTER)?.takeIf { it.isNotBlank() }

/**
 * A file opened with the app or shared to it (see the manifest's intent filters); its content
 * tells a backup from a config to import. Not file://: another app could point it at this
 * app's private files.
 */
private fun Intent.backupFile(): Uri? {
    val uri = when (action) {
        Intent.ACTION_VIEW -> data
        Intent.ACTION_SEND -> IntentCompat.getParcelableExtra(this, Intent.EXTRA_STREAM, Uri::class.java)
        else -> null
    }
    return uri?.takeIf { it.scheme == "content" }
}

private fun Intent.deepLink(): DeepLink? {
    val uri = data
    if (action == Intent.ACTION_VIEW && uri?.scheme == "ichor" && uri.host == "demo" &&
        uri.port == -1 && uri.userInfo == null && uri.path.orEmpty() in listOf("", "/") &&
        uri.query == null && uri.fragment == null
    ) return DeepLink.DEMO
    return getStringExtra(MainActivity.EXTRA_OPEN)?.let { name -> DeepLink.entries.firstOrNull { it.name == name } }
}

/**
 * Nothing (not even the encrypted config) is loaded before the first unlock. Later relocks
 * draw over the app so navigation state survives.
 */
@Composable
private fun LockGate(app: TalosApp, targets: LaunchTargets, onWiped: () -> Unit) {
    val locked by app.appLock.locked.collectAsStateWithLifecycle()
    val everUnlocked by app.appLock.everUnlocked.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    Box {
        if (everUnlocked) Root(app, targets)
        if (locked) {
            LockScreen(
                onUnlocked = { app.appLock.unlock() },
                onWipe = {
                    scope.launch {
                        app.configRepository.clear()
                        app.appLock.setEnabled(false)
                        onWiped()
                    }
                },
            )
        }
    }
}

/**
 * The stored config once loaded. A real cluster stored without the app lock gets the lock
 * onboarding first: right after the first import, or on updating from an optional lock.
 */
@Composable
private fun Root(app: TalosApp, targets: LaunchTargets) {
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val lockEnabled by app.appLock.enabled.collectAsStateWithLifecycle()
    var load by remember { mutableStateOf<ConfigLoad>(ConfigLoad.Loading) }
    var attempt by remember { mutableIntStateOf(0) }
    LaunchedEffect(attempt) {
        load = ConfigLoad.Loading
        load = try {
            app.configRepository.load()
            ConfigLoad.Done
        } catch (e: ConfigUnreadableException) {
            ConfigLoad.Unreadable(e.cause?.let { "${it.javaClass.simpleName}: ${it.message}" }.orEmpty())
        }
    }

    when (val state = load) {
        ConfigLoad.Loading -> LoadingBox()
        // Not the import screen: the config is still stored, reading it failed.
        is ConfigLoad.Unreadable -> ConfigUnreadableScreen(
            reason = state.reason,
            onRetry = { attempt++ },
            onImport = { load = ConfigLoad.Done },
        )
        ConfigLoad.Done -> Loaded(app, targets, config, lockEnabled)
    }
}

/** Where reading the stored config is at. Done covers "none stored", which starts on the import screen. */
private sealed interface ConfigLoad {
    data object Loading : ConfigLoad
    data object Done : ConfigLoad
    class Unreadable(val reason: String) : ConfigLoad
}

@Composable
private fun Loaded(app: TalosApp, targets: LaunchTargets, config: StoredConfig?, lockEnabled: Boolean) {
    val link by targets.deepLink.collectAsStateWithLifecycle()
    val cluster by targets.cluster.collectAsStateWithLifecycle()
    val backup by targets.backupFile.collectAsStateWithLifecycle()
    val shell by targets.shell.collectAsStateWithLifecycle()
    val shareLink by targets.shareLink.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    when {
        !lockEnabled && lockRequired(config?.summary?.contexts.orEmpty()) -> LockOnboarding(
            // The import screen may have left before starting the monitoring sync.
            onEnabled = {
                app.appLock.setEnabled(true)
                app.launchSync(runNow = true)
            },
            onDeleteConfig = {
                scope.launch {
                    app.configRepository.clear()
                    app.launchSync(runNow = true)
                }
            },
        )
        else -> {
            // Read when the navigation (re)starts, e.g. on the overview after the onboarding.
            val startWithImport = remember { app.configRepository.config.value == null }
            Navigation(
                app,
                startWithImport = startWithImport,
                deepLink = link,
                onDeepLinkHandled = { targets.deepLink.value = null },
                openCluster = cluster,
                onClusterOpened = { targets.cluster.value = null },
                incomingBackup = backup,
                onIncomingBackupRead = { targets.backupFile.value = null },
                openShell = shell,
                onShellOpened = { targets.shell.value = null },
                openLink = shareLink,
                onLinkOpened = { targets.shareLink.value = null },
            )
        }
    }
}
