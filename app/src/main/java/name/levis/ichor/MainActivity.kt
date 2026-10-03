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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.fragment.app.FragmentActivity
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.security.LockOnboarding
import name.levis.ichor.security.LockScreen
import name.levis.ichor.security.lockRequired
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.seedOf
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.Navigation
import name.levis.ichor.ui.components.LoadingBox
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
        }
        if (BuildConfig.SELF_UPDATE) app.updateManager.maybeAutoCheck(lifecycleScope)
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
                    LockGate(app, LaunchTargets(deepLink, openCluster, backupFile), onWiped = ::recreate)
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
    }

    companion object {
        const val EXTRA_OPEN = "name.levis.ichor.OPEN"

        /** The fingerprint of the cluster a launcher shortcut opens. */
        const val EXTRA_CLUSTER = "name.levis.ichor.CLUSTER"
    }
}

/** What the intent that launched the app asks to show: a screen, a cluster, a backup to restore. */
private class LaunchTargets(
    val deepLink: MutableStateFlow<DeepLink?>,
    val cluster: MutableStateFlow<String?>,
    val backupFile: MutableStateFlow<Uri?>,
)

private fun Intent.clusterFingerprint(): String? = getStringExtra(MainActivity.EXTRA_CLUSTER)?.takeIf { it.isNotBlank() }

/**
 * A file opened with the app (see the manifest's intent filters); its content tells whether it
 * is a backup. Not file://: another app could point it at this app's private files.
 */
private fun Intent.backupFile(): Uri? = data?.takeIf { action == Intent.ACTION_VIEW && it.scheme == "content" }

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
    val link by targets.deepLink.collectAsStateWithLifecycle()
    val cluster by targets.cluster.collectAsStateWithLifecycle()
    val backup by targets.backupFile.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val lockEnabled by app.appLock.enabled.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var loaded by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        app.configRepository.load()
        loaded = true
    }

    when {
        !loaded -> LoadingBox()
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
            )
        }
    }
}
