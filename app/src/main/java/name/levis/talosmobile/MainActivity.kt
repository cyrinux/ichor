package name.levis.talosmobile

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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.fragment.app.FragmentActivity
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import name.levis.talosmobile.i18n.AppLocale
import name.levis.talosmobile.security.LockScreen
import name.levis.talosmobile.ui.DeepLink
import name.levis.talosmobile.ui.Navigation
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.theme.TalosTheme
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch

// FragmentActivity (still a ComponentActivity) is required by BiometricPrompt.
class MainActivity : FragmentActivity() {
    /** A screen to open from a notification tap, consumed once by Navigation. */
    private val deepLink = MutableStateFlow<DeepLink?>(null)

    // Below API 33, the in-app language is applied here (API 33+ uses LocaleManager).
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
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

        if (savedInstanceState == null) deepLink.value = intent.deepLink()
        app.updateManager.maybeAutoCheck(lifecycleScope)
        if (savedInstanceState == null) app.supportPrompt.onLaunch()

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
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    // No grey scrim behind 3-button navigation: true black stays black.
                    window.isNavigationBarContrastEnforced = false
                }
                onDispose {}
            }
            TalosTheme(themeMode) {
                Surface {
                    LockGate(app, deepLink, onWiped = ::recreate)
                }
            }
        }
    }

    // A notification tapped while the activity is kept (otherwise onCreate reads the intent).
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        intent.deepLink()?.let { deepLink.value = it }
    }

    companion object {
        const val EXTRA_OPEN = "name.levis.talosmobile.OPEN"
    }
}

private fun Intent.deepLink(): DeepLink? =
    getStringExtra(MainActivity.EXTRA_OPEN)?.let { name -> DeepLink.entries.firstOrNull { it.name == name } }

/**
 * Nothing (not even the encrypted config) is loaded before the first unlock. Later relocks
 * draw over the app so navigation state survives.
 */
@Composable
private fun LockGate(app: TalosApp, deepLink: MutableStateFlow<DeepLink?>, onWiped: () -> Unit) {
    val locked by app.appLock.locked.collectAsStateWithLifecycle()
    var everUnlocked by rememberSaveable { mutableStateOf(!locked) }
    val scope = rememberCoroutineScope()

    Box {
        if (everUnlocked) Root(app, deepLink)
        if (locked) {
            LockScreen(
                onUnlocked = {
                    everUnlocked = true
                    app.appLock.unlock()
                },
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
private fun Root(app: TalosApp, deepLink: MutableStateFlow<DeepLink?>) {
    val link by deepLink.collectAsStateWithLifecycle()
    // null = still loading the stored config; then whether one exists.
    var hasConfig by remember { mutableStateOf<Boolean?>(null) }
    LaunchedEffect(Unit) { hasConfig = app.configRepository.load() != null }

    when (val ready = hasConfig) {
        null -> LoadingBox()
        else -> Navigation(app, startWithImport = !ready, deepLink = link, onDeepLinkHandled = { deepLink.value = null })
    }
}
