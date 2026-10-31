package name.levis.ichor.security

import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.TalosApp

/**
 * Nothing (not even the encrypted config) is loaded before the first unlock. Later relocks
 * draw over [content] so its state survives. [onLockedBack]: what back does on the lock screen
 * (default: leave the task as it is, see [LockScreen]).
 */
@Composable
fun AppLockGate(app: TalosApp, onWiped: () -> Unit, onLockedBack: (() -> Unit)? = null, content: @Composable () -> Unit) {
    val locked by app.appLock.locked.collectAsStateWithLifecycle()
    val everUnlocked by app.appLock.everUnlocked.collectAsStateWithLifecycle()
    val securityKeys by app.appLock.securityKeys.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    // A panic app had the config deleted while this activity was alive.
    LaunchedEffect(Unit) { app.appLock.configDeleted.collect { onWiped() } }

    Box {
        if (everUnlocked) content()
        if (locked) {
            LockScreen(
                onUnlocked = { app.appLock.unlock() },
                onWipe = {
                    scope.launch {
                        app.configRepository.clear()
                        // Sealed with the lost keys too: unreadable for good.
                        app.kubeAuthStore.clear()
                        app.appLock.setEnabled(false)
                        onWiped()
                    }
                },
                securityKeys = securityKeys,
                onBack = onLockedBack,
            )
        }
        // The "tap your security key" dialog, over the lock screen or whatever asked for a check.
        SecurityKeyPromptHost(app)
    }
}
