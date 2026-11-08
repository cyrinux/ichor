package name.levis.ichor.security

import android.content.Context
import android.content.pm.PackageManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

private const val DELETE_TOKEN = "DELETE"

/** A panic app's name, or its package when it cannot be seen (uninstalled since). */
fun panicAppLabel(context: Context, packageName: String): String = try {
    val packages = context.packageManager
    packages.getApplicationLabel(packages.getApplicationInfo(packageName, 0)).toString()
} catch (_: PackageManager.NameNotFoundException) {
    packageName
}

/**
 * What [triggerPackage]'s panic trigger does. Deleting the config takes the app lock, a
 * fingerprint/PIN check and a typed word. Replacing a connection takes the check too: whoever
 * holds the unlocked phone must not be able to downgrade it, or hand the trigger to another app.
 */
@Composable
fun PanicConfigDialog(
    appLock: AppLock,
    triggerPackage: String,
    onConnected: (PanicResponse) -> Unit,
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val lockEnabled by appLock.enabled.collectAsStateWithLifecycle()
    val connected by appLock.panicTriggerPackage.collectAsStateWithLifecycle()
    val configured by appLock.panicResponse.collectAsStateWithLifecycle()
    val canAuth = remember { canAuthenticate(context) }
    val canDelete = lockEnabled && canAuth
    var choice by remember { mutableStateOf(if (connected == triggerPackage && canDelete) configured else PanicResponse.LOCK) }
    var typing by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val label = remember(triggerPackage) { panicAppLabel(context, triggerPackage) }
    val title = stringResource(R.string.panic_config_title)

    fun proceed() {
        if (choice == PanicResponse.DELETE_CONFIG) typing = true else onConnected(PanicResponse.LOCK)
    }

    fun confirm() {
        val delete = choice == PanicResponse.DELETE_CONFIG
        val activity = context.findFragmentActivity()
        if (!canAuth || !(delete || connected != null) || activity == null) {
            // A delete never proceeds without a check.
            if (!delete) proceed()
            return
        }
        scope.launch {
            when (val result = authenticate(activity, title)) {
                AuthResult.Success -> proceed()
                is AuthResult.Failure -> error = result.message
            }
        }
    }

    if (typing) {
        HostnameConfirmDialog(
            title = stringResource(R.string.panic_delete_confirm_title),
            hostname = DELETE_TOKEN,
            confirmLabel = stringResource(R.string.panic_confirm),
            onConfirm = { onConnected(PanicResponse.DELETE_CONFIG) },
            onDismiss = { typing = false },
        ) {
            Text(stringResource(R.string.panic_response_delete_desc))
        }
        return
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.panic_config_body, label))
                Column(Modifier.selectableGroup()) {
                    PanicChoice(
                        selected = choice == PanicResponse.LOCK,
                        enabled = true,
                        title = stringResource(R.string.panic_response_lock),
                        description = stringResource(R.string.panic_response_lock_desc),
                        onClick = { choice = PanicResponse.LOCK },
                    )
                    PanicChoice(
                        selected = choice == PanicResponse.DELETE_CONFIG,
                        enabled = canDelete,
                        title = stringResource(R.string.panic_response_delete),
                        description = stringResource(if (canDelete) R.string.panic_response_delete_desc else R.string.panic_delete_needs_lock),
                        onClick = { choice = PanicResponse.DELETE_CONFIG },
                    )
                }
                error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
            }
        },
        confirmButton = { TextButton(onClick = ::confirm) { Text(stringResource(R.string.panic_confirm)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun PanicChoice(selected: Boolean, enabled: Boolean, title: String, description: String, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .selectable(selected = selected, enabled = enabled, role = Role.RadioButton, onClick = onClick)
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = null, enabled = enabled)
        Column(Modifier.padding(start = 12.dp)) {
            Text(title, style = MaterialTheme.typography.bodyMedium)
            MutedText(description)
        }
    }
}
