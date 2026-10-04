package name.levis.ichor.security

import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Full-screen lock. Prompts automatically; if the device lost its screen lock the user can
 * only wipe the stored config, so removing the screen lock never bypasses the app lock.
 */
@Composable
fun LockScreen(onUnlocked: () -> Unit, onWipe: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<String?>(null) }
    val available = remember { canAuthenticate(context) }

    fun prompt() {
        val activity = context.findFragmentActivity() ?: return
        scope.launch {
            when (val result = authenticate(activity, context.getString(R.string.lock_prompt))) {
                AuthResult.Success -> onUnlocked()
                is AuthResult.Failure -> error = result.message
            }
        }
    }

    LaunchedEffect(Unit) { if (available) prompt() }
    // Back must not reach the (hidden) screens underneath.
    BackHandler { context.findFragmentActivity()?.moveTaskToBack(true) }

    Surface(Modifier.fillMaxSize()) {
        Column(
            Modifier.fillMaxSize().safeDrawingPadding().padding(32.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(Icons.Outlined.Lock, contentDescription = null, modifier = Modifier.size(48.dp))
            Spacer(Modifier.height(16.dp))
            if (available) {
                Text(stringResource(R.string.lock_locked), style = MaterialTheme.typography.titleMedium, modifier = Modifier.semantics { heading() })
                error?.let {
                    Spacer(Modifier.height(8.dp))
                    Text(
                        it,
                        color = MaterialTheme.colorScheme.error,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    )
                }
                Spacer(Modifier.height(16.dp))
                Button(onClick = ::prompt) { Text(stringResource(R.string.lock_unlock)) }
            } else {
                Text(
                    stringResource(R.string.lock_no_auth),
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(16.dp))
                OutlinedButton(onClick = onWipe) { Text(stringResource(R.string.lock_delete_config)) }
            }
        }
    }
}
