package name.levis.ichor.security

import android.view.WindowManager
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.TalosApp

/**
 * Blocks screenshots and the recents thumbnail while [secure], whatever the "allow
 * screenshots" setting says (secrets or credentials on screen).
 */
@Composable
fun SecureWhile(secure: Boolean) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val window = context.findFragmentActivity()?.window ?: return
    DisposableEffect(secure) {
        if (secure) window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        onDispose {
            // MainActivity keeps the flag on when the app lock requires it.
            val appWide = app.appLock.enabled.value && !app.uiPreferences.allowScreenshots.value
            if (secure && !appWide) window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
        }
    }
}
