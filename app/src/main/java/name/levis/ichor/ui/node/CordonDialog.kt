package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Row
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.ui.components.ConfirmDialog

/**
 * Confirms a cordon or an uncordon of [hostname]. [cordoned] null: the app does not know the
 * node's state yet, so both are offered.
 */
@Composable
fun CordonDialog(hostname: String, cordoned: Boolean?, onConfirm: (on: Boolean) -> Unit, onDismiss: () -> Unit) {
    if (cordoned != null) {
        val on = !cordoned
        ConfirmDialog(
            title = stringResource(if (on) R.string.cordon_confirm_title else R.string.uncordon_confirm_title, hostname),
            text = stringResource(if (on) R.string.cordon_confirm_body else R.string.uncordon_confirm_body, hostname),
            confirm = stringResource(if (on) R.string.node_menu_cordon else R.string.node_menu_uncordon),
            onConfirm = { onConfirm(on) },
            onDismiss = onDismiss,
        )
        return
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.cordon_unknown_title, hostname)) },
        text = { Text(stringResource(R.string.cordon_unknown_body, hostname)) },
        confirmButton = { TextButton(onClick = { onConfirm(true) }) { Text(stringResource(R.string.node_menu_cordon)) } },
        dismissButton = {
            Row {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                TextButton(onClick = { onConfirm(false) }) { Text(stringResource(R.string.node_menu_uncordon)) }
            }
        },
    )
}
