package name.levis.ichor.ui.kubeauth

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FieldKind
import name.levis.ichor.model.credentialField
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.readBounded

/** A service account key is a few KB; anything far larger is not one. */
private const val MAX_KEY_FILE_BYTES = 64 * 1024

/**
 * The credentials [fields] (Go core field names) to fill in, each labelled in the app's
 * language: secrets masked, a service account key as pasted JSON or a file. [values] holds
 * what was typed, by field name.
 */
@Composable
fun CredentialFields(fields: List<String>, values: Map<String, String>, onValue: (String, String) -> Unit, enabled: Boolean = true) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        fields.forEach { name ->
            val field = credentialField(name)
            val label = field?.let {
                val text = stringResource(it.label)
                if (it.optional) stringResource(R.string.kube_field_optional, text) else text
            } ?: name
            when (field?.kind ?: FieldKind.SECRET) {
                FieldKind.TEXT -> OutlinedTextField(
                    value = values[name].orEmpty(),
                    onValueChange = { onValue(name, it) },
                    label = { Text(label) },
                    singleLine = true,
                    enabled = enabled,
                    keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Uri),
                    modifier = Modifier.fillMaxWidth(),
                )
                FieldKind.SECRET -> SecretField(values[name].orEmpty(), { onValue(name, it) }, label, enabled)
                FieldKind.JSON -> KeyFileField(values[name].orEmpty(), { onValue(name, it) }, label, enabled)
            }
        }
    }
}

@Composable
private fun SecretField(value: String, onValue: (String) -> Unit, label: String, enabled: Boolean) {
    var visible by remember { mutableStateOf(false) }
    OutlinedTextField(
        value = value,
        onValueChange = onValue,
        label = { Text(label) },
        singleLine = true,
        enabled = enabled,
        visualTransformation = if (visible) VisualTransformation.None else PasswordVisualTransformation(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
        trailingIcon = {
            IconButton(onClick = { visible = !visible }) {
                Icon(
                    if (visible) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility,
                    contentDescription = stringResource(if (visible) R.string.kube_signin_hide else R.string.kube_signin_show),
                )
            }
        },
        modifier = Modifier.fillMaxWidth(),
    )
}

/** A JSON key (GCP service account): pasted, or read from a file. */
@Composable
private fun KeyFileField(value: String, onValue: (String) -> Unit, label: String, enabled: Boolean) {
    val context = LocalContext.current
    var error by remember { mutableStateOf<String?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        runCatching {
            context.contentResolver.openInputStream(uri)?.use { readBounded(it, MAX_KEY_FILE_BYTES, "File is too large").decodeToString() }
        }.fold(
            onSuccess = { text ->
                error = if (text == null) context.getString(R.string.import_could_not_open) else null
                text?.let(onValue)
            },
            onFailure = { error = context.getString(R.string.import_could_not_open) },
        )
    }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        OutlinedTextField(
            value = value,
            onValueChange = onValue,
            label = { Text(label) },
            minLines = 4,
            maxLines = 8,
            enabled = enabled,
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedButton(onClick = { picker.launch(arrayOf("application/json", "text/plain", "*/*")) }, enabled = enabled) {
            Icon(Icons.Outlined.FileOpen, contentDescription = null)
            Text(stringResource(R.string.kube_field_from_file))
        }
        error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
    }
}

/** Opens [url] in the browser; false when there is none. */
fun openInBrowser(context: Context, url: String): Boolean = try {
    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    true
} catch (_: ActivityNotFoundException) {
    false
}

