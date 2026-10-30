package name.levis.ichor.ui.components

import name.levis.ichor.ui.design.IchorAlertDialog
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Yes/cancel dialog; [destructive] colours the [confirm] button as an error, [confirmColor]
 * as anything else (a caution in the warn colour).
 */
@Composable
fun ConfirmDialog(
    title: String,
    text: String,
    confirm: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    destructive: Boolean = false,
    confirmColor: Color = Color.Unspecified,
) {
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Text(text) },
        confirmButton = {
            IchorTextButton(onClick = onConfirm) {
                Text(confirm, color = if (destructive) LocalStatusColors.current.bad else confirmColor)
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
