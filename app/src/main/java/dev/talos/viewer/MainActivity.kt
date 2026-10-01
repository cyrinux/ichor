package dev.talos.viewer

import android.os.Bundle
import android.view.WindowManager
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
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
import dev.talos.viewer.security.LockScreen
import dev.talos.viewer.ui.Navigation
import dev.talos.viewer.ui.components.LoadingBox
import dev.talos.viewer.ui.theme.TalosTheme
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch

// FragmentActivity (still a ComponentActivity) is required by BiometricPrompt.
class MainActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
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

        setContent {
            val themeMode by app.uiPreferences.themeMode.collectAsStateWithLifecycle()
            TalosTheme(themeMode) {
                Surface {
                    LockGate(app, onWiped = ::recreate)
                }
            }
        }
    }
}

/**
 * Nothing (not even the encrypted config) is loaded before the first unlock. Later relocks
 * draw over the app so navigation state survives.
 */
@Composable
private fun LockGate(app: TalosApp, onWiped: () -> Unit) {
    val locked by app.appLock.locked.collectAsStateWithLifecycle()
    var everUnlocked by rememberSaveable { mutableStateOf(!locked) }
    val scope = rememberCoroutineScope()

    Box {
        if (everUnlocked) Root(app)
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
private fun Root(app: TalosApp) {
    // null = still loading the stored config; then whether one exists.
    var hasConfig by remember { mutableStateOf<Boolean?>(null) }
    LaunchedEffect(Unit) { hasConfig = app.configRepository.load() != null }

    when (val ready = hasConfig) {
        null -> LoadingBox()
        else -> Navigation(app, startWithImport = !ready)
    }
}
