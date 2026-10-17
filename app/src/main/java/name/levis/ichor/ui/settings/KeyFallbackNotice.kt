package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KeyMoveResult
import name.levis.ichor.data.KeyProtection

/**
 * Settings → Security: the config key went to the TEE because the secure chip kept refusing
 * (see SecureStore.key). Shown until dismissed, or until the key is deleted or StrongBox again.
 * "Move to the secure chip" re-keys the stored configs in place (see
 * ConfigRepository.moveKeyToStrongBox) and says how it went in [snackbar].
 */
@Composable
fun KeyFallbackNotice(configs: ConfigRepository, snackbar: SnackbarHostState) {
    val fellBack by configs.keyFallback.collectAsStateWithLifecycle()
    val config by configs.config.collectAsStateWithLifecycle()
    var moving by remember { mutableStateOf(false) }
    // Keystore calls: read off the main thread, again only when the stored config changes.
    val protection by produceState<KeyProtection?>(null, config, fellBack, moving) {
        value = if (fellBack) withContext(Dispatchers.IO) { configs.keyProtection() } else null
    }
    // Before the early return: the snackbar still shows once a move hides the notice.
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    if (!fellBack || (protection != KeyProtection.TEE && !moving)) return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.security_key_fell_back), style = MaterialTheme.typography.bodyMedium)
            if (moving) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    Text(stringResource(R.string.security_key_moving), style = MaterialTheme.typography.bodySmall)
                }
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(
                    enabled = !moving,
                    onClick = click@{
                        // A second tap before the button shows disabled: one move at a time.
                        if (moving) return@click
                        moving = true
                        scope.launch {
                            // Fails only by cancellation: the move reports any error in its result.
                            val result = configs.moveKeyToStrongBox()
                            moving = false
                            snackbar.showSnackbar(context.getString(result.message))
                        }
                    },
                ) { Text(stringResource(R.string.security_key_move)) }
                TextButton(enabled = !moving, onClick = configs::dismissKeyFallback) { Text(stringResource(R.string.common_ok)) }
            }
        }
    }
}

/** The snackbar after a move of the config keys. */
private val KeyMoveResult.message: Int
    get() = when (this) {
        KeyMoveResult.MOVED -> R.string.security_key_moved
        KeyMoveResult.PARTIAL -> R.string.security_key_move_partial
        KeyMoveResult.REFUSED -> R.string.security_key_move_refused
        KeyMoveResult.SECURITY_KEY_REQUIRED -> R.string.security_key_move_needs_key
    }
