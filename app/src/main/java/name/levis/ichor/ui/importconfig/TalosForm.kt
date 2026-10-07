package name.levis.ichor.ui.importconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.TalosForm
import name.levis.ichor.model.TalosFormMode
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A talosconfig context entered by hand: a cluster reached directly (endpoints, nodes, the CA
 * and client certificate, typed or read from files) or through Omni. [onSubmit] builds it and
 * opens the usual preview.
 */
@Composable
internal fun TalosFormSource(form: TalosForm, onForm: (TalosForm) -> Unit, onSubmit: (TalosForm) -> Unit) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        ModeToggle(form.mode) { onForm(form.copy(mode = it)) }
        LineField(form.name, { onForm(form.copy(name = it)) }, stringResource(R.string.import_form_name))
        when (form.mode) {
            TalosFormMode.DIRECT -> DirectFields(form, onForm)
            TalosFormMode.OMNI -> OmniFields(form, onForm)
        }
        Button(onClick = { onSubmit(form) }, enabled = form.canSubmit, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.import_form_continue))
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ModeToggle(mode: TalosFormMode, onMode: (TalosFormMode) -> Unit) {
    val modes = TalosFormMode.entries
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        modes.forEachIndexed { i, m ->
            SegmentedButton(
                selected = m == mode,
                onClick = { onMode(m) },
                shape = SegmentedButtonDefaults.itemShape(i, modes.size),
            ) {
                Text(stringResource(if (m == TalosFormMode.DIRECT) R.string.import_form_mode_talos else R.string.import_form_mode_omni))
            }
        }
    }
}

@Composable
private fun DirectFields(form: TalosForm, onForm: (TalosForm) -> Unit) {
    ListField(form.endpoints, { onForm(form.copy(endpoints = it)) }, stringResource(R.string.import_form_endpoints), stringResource(R.string.import_form_endpoints_hint))
    ListField(form.nodes, { onForm(form.copy(nodes = it)) }, stringResource(R.string.kube_field_optional, stringResource(R.string.import_form_nodes)), stringResource(R.string.import_form_nodes_hint))
    PemField(form.ca, { onForm(form.copy(ca = it)) }, stringResource(R.string.import_form_ca))
    PemField(form.crt, { onForm(form.copy(crt = it)) }, stringResource(R.string.import_form_crt))
    PemField(form.key, { onForm(form.copy(key = it)) }, stringResource(R.string.import_form_key))
    MutedText(stringResource(R.string.import_form_pem_hint))
}

@Composable
private fun OmniFields(form: TalosForm, onForm: (TalosForm) -> Unit) {
    LineField(form.omniUrl, { onForm(form.copy(omniUrl = it)) }, stringResource(R.string.import_form_omni_url), KeyboardType.Uri, placeholder = "https://acme.omni.example.com")
    LineField(form.cluster, { onForm(form.copy(cluster = it)) }, stringResource(R.string.import_omni_cluster))
    LineField(
        form.identity,
        { onForm(form.copy(identity = it)) },
        stringResource(R.string.import_omni_identity),
        KeyboardType.Email,
        supporting = stringResource(R.string.import_form_omni_identity_hint),
    )
}

@Composable
private fun LineField(
    value: String,
    onValue: (String) -> Unit,
    label: String,
    keyboard: KeyboardType = KeyboardType.Text,
    placeholder: String? = null,
    supporting: String? = null,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValue,
        label = { Text(label) },
        placeholder = placeholder?.let { { Text(it) } },
        supportingText = supporting?.let { { Text(it) } },
        singleLine = true,
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = keyboard),
        modifier = Modifier.fillMaxWidth(),
    )
}

/** Addresses, one per line. */
@Composable
private fun ListField(value: String, onValue: (String) -> Unit, label: String, supporting: String) {
    OutlinedTextField(
        value = value,
        onValueChange = onValue,
        label = { Text(label) },
        supportingText = { Text(supporting) },
        minLines = 2,
        maxLines = 6,
        textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Uri),
        modifier = Modifier.fillMaxWidth(),
    )
}

/** A PEM block (or its base64), pasted or read from a file. */
@Composable
private fun PemField(value: String, onValue: (String) -> Unit, label: String) {
    var error by remember { mutableStateOf<String?>(null) }
    val pickFile = rememberConfigFilePicker(onYaml = { error = null; onValue(it) }, onError = { error = it })
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        OutlinedTextField(
            value = value,
            onValueChange = { if (it.length <= MAX_CONFIG_BYTES) onValue(it) },
            label = { Text(label) },
            minLines = 3,
            maxLines = 6,
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedButton(onClick = pickFile) {
            Icon(Icons.Outlined.FileOpen, contentDescription = null, modifier = Modifier.padding(end = 8.dp))
            Text(stringResource(R.string.import_form_pick_file))
        }
        error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
    }
}
