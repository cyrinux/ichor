package name.levis.ichor.ui.importconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The stored config could not be read (see ConfigRepository.load): trying again usually
 * works; importing again is the way out when its key is gone for good.
 */
@Composable
fun ConfigUnreadableScreen(reason: String, onRetry: () -> Unit, onImport: () -> Unit) {
    Column(
        Modifier.fillMaxSize().safeDrawingPadding().padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            Icons.Outlined.ErrorOutline,
            contentDescription = null,
            tint = LocalStatusColors.current.bad,
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(stringResource(R.string.config_unreadable_title), textAlign = TextAlign.Center, style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        Text(stringResource(R.string.config_unreadable_body), textAlign = TextAlign.Center, style = MaterialTheme.typography.bodyMedium)
        if (reason.isNotBlank()) {
            Spacer(Modifier.height(8.dp))
            Text(
                reason,
                textAlign = TextAlign.Center,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Spacer(Modifier.height(16.dp))
        Button(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
        TextButton(onClick = onImport) { Text(stringResource(R.string.config_unreadable_import)) }
    }
}
