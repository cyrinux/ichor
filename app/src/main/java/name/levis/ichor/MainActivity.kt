package name.levis.ichor

import android.content.Context
import android.content.Intent
import android.graphics.Color
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
import name.levis.ichor.security.LockScreen
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
        }
        if (BuildConfig.SELF_UPDATE) app.updateManager.maybeAutoCheck(lifecycleScope)
        if (BuildConfig.DONATIONS && savedInstanceState == null) app.supportPrompt.onLaunch()

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
                    LockGate(app, LaunchTargets(deepLink, openCluster), onWiped = ::recreate)
                }
            }
        }
    }

    // A notification or shortcut tapped while the activity is kept (otherwise onCreate reads the intent).
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        intent.deepLink()?.let { deepLink.value = it }
        intent.clusterFingerprint()?.let { openCluster.value = it }
    }

    companion object {
        const val EXTRA_OPEN = "name.levis.ichor.OPEN"

        /** The fingerprint of the cluster a launcher shortcut opens. */
        const val EXTRA_CLUSTER = "name.levis.ichor.CLUSTER"
    }
}

/** What the intent that launched the app asks to show: a screen, a cluster. */
private class LaunchTargets(val deepLink: MutableStateFlow<DeepLink?>, val cluster: MutableStateFlow<String?>)

private fun Intent.clusterFingerprint(): String? = getStringExtra(MainActivity.EXTRA_CLUSTER)?.takeIf { it.isNotBlank() }

private fun Intent.deepLink(): DeepLink? =
    getStringExtra(MainActivity.EXTRA_OPEN)?.let { name -> DeepLink.entries.firstOrNull { it.name == name } }

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

@Composable
private fun Root(app: TalosApp, targets: LaunchTargets) {
    val link by targets.deepLink.collectAsStateWithLifecycle()
    val cluster by targets.cluster.collectAsStateWithLifecycle()
    // null = still loading the stored config; then whether one exists.
    var hasConfig by remember { mutableStateOf<Boolean?>(null) }
    LaunchedEffect(Unit) { hasConfig = app.configRepository.load() != null }

    when (val ready = hasConfig) {
        null -> LoadingBox()
        else -> Navigation(
            app,
            startWithImport = !ready,
            deepLink = link,
            onDeepLinkHandled = { targets.deepLink.value = null },
            openCluster = cluster,
            onClusterOpened = { targets.cluster.value = null },
        )
    }
}
