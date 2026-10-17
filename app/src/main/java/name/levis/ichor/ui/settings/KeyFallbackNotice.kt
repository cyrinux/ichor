package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KeyProtection

/**
 * Settings → Security: the config key went to the TEE because the secure chip kept refusing
 * (see SecureStore.key). Shown until dismissed, or until the key is deleted or StrongBox again.
 */
@Composable
fun KeyFallbackNotice(configs: ConfigRepository) {
    val fellBack by configs.keyFallback.collectAsStateWithLifecycle()
    val config by configs.config.collectAsStateWithLifecycle()
    // Keystore calls: read off the main thread, again only when the stored config changes.
    val protection by produceState<KeyProtection?>(null, config, fellBack) {
        value = if (fellBack) withContext(Dispatchers.IO) { configs.keyProtection() } else null
    }
    if (!fellBack || protection != KeyProtection.TEE) return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.security_key_fell_back), style = MaterialTheme.typography.bodyMedium)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = configs::dismissKeyFallback) { Text(stringResource(R.string.common_ok)) }
            }
        }
    }
}
