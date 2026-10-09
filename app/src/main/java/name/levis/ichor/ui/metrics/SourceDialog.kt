package name.levis.ichor.ui.metrics

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.PromSource
import name.levis.ichor.ui.components.MutedText

/** What the dialog says after Test or Save. */
private sealed interface SourceOutcome {
    data object Busy : SourceOutcome
    data object Reachable : SourceOutcome
    data class Failed(val message: String) : SourceOutcome
}

/**
 * Picks where queries go: a query API found in the cluster, a Service typed in (both through
 * the Kubernetes API), or a URL with its credentials. Go checks it before it is saved. Also
 * picks the Alertmanager ([title], [noneFound] and [reachable] then say so).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SourceDialog(
    current: PromSource?,
    discovered: List<PromSource>?,
    discovering: Boolean,
    discoveryError: String?,
    onDiscover: () -> Unit,
    onTest: suspend (PromSource) -> String?,
    onSave: suspend (PromSource) -> String?,
    onDismiss: () -> Unit,
    title: String = stringResource(R.string.metrics_source),
    /** What the search says when it found nothing. */
    noneFound: String = stringResource(R.string.metrics_none_found),
    /** What Test says when the source answers. */
    reachable: String = stringResource(R.string.metrics_reachable),
) {
    var source by remember { mutableStateOf(current ?: discovered?.firstOrNull() ?: PromSource()) }
    var portText by remember { mutableStateOf(source.port.takeIf { it > 0 }?.toString().orEmpty()) }
    var outcome by remember { mutableStateOf<SourceOutcome?>(null) }
    val scope = rememberCoroutineScope()
    val edited = if (source.mode == PromSource.MODE_PROXY) source.copy(port = portText.toIntOrNull() ?: 0) else source

    fun run(action: suspend (PromSource) -> String?, then: () -> Unit = {}) {
        outcome = SourceOutcome.Busy
        scope.launch {
            val error = action(edited)
            outcome = error?.let { SourceOutcome.Failed(it) } ?: SourceOutcome.Reachable
            if (error == null) then()
        }
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                val modes = listOf(PromSource.MODE_PROXY to R.string.metrics_mode_cluster, PromSource.MODE_URL to R.string.metrics_mode_url)
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    modes.forEachIndexed { i, (mode, label) ->
                        SegmentedButton(
                            selected = source.mode == mode,
                            onClick = { source = source.forMode(mode); outcome = null },
                            shape = SegmentedButtonDefaults.itemShape(i, modes.size),
                            icon = {},
                        ) { Text(stringResource(label)) }
                    }
                }
                if (source.mode == PromSource.MODE_PROXY) {
                    Discovered(discovered, discovering, discoveryError, noneFound, edited, onDiscover) { found ->
                        source = found.copy(tenant = source.tenant)
                        portText = found.port.toString()
                        outcome = null
                    }
                    MutedText(stringResource(R.string.metrics_proxy_hint))
                    Field(source.namespace, R.string.metrics_namespace) { source = source.copy(namespace = it) }
                    Field(source.service, R.string.metrics_service) { source = source.copy(service = it) }
                    Field(portText, R.string.metrics_port, KeyboardType.Number) { portText = it.filter(Char::isDigit).take(5) }
                    Field(source.pathPrefix, R.string.metrics_path_prefix) { source = source.copy(pathPrefix = it) }
                } else {
                    Field(source.url, R.string.metrics_url, KeyboardType.Uri) { source = source.copy(url = it) }
                    AuthFields(source) { source = it }
                }
                Field(source.tenant, R.string.metrics_tenant) { source = source.copy(tenant = it) }
                when (val o = outcome) {
                    SourceOutcome.Busy -> LinearProgressIndicator(Modifier.fillMaxWidth())
                    SourceOutcome.Reachable -> Text(reachable, color = MaterialTheme.colorScheme.primary)
                    is SourceOutcome.Failed -> Text(o.message, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                    null -> Unit
                }
            }
        },
        confirmButton = {
            Row {
                TextButton(enabled = outcome != SourceOutcome.Busy, onClick = { run(onTest) }) { Text(stringResource(R.string.metrics_test)) }
                TextButton(enabled = outcome != SourceOutcome.Busy, onClick = { run(onSave, onDismiss) }) { Text(stringResource(R.string.metrics_save)) }
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun Discovered(
    discovered: List<PromSource>?,
    discovering: Boolean,
    error: String?,
    noneFound: String,
    selected: PromSource,
    onDiscover: () -> Unit,
    onPick: (PromSource) -> Unit,
) {
    when {
        discovering -> LinearProgressIndicator(Modifier.fillMaxWidth())
        error != null -> Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
        discovered?.isEmpty() == true -> MutedText(noneFound)
    }
    discovered.orEmpty().forEach { found ->
        val chosen = found.namespace == selected.namespace && found.service == selected.service &&
            found.port == selected.port && found.pathPrefix == selected.pathPrefix
        Row(
            Modifier.fillMaxWidth().selectable(selected = chosen, onClick = { onPick(found) }),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            RadioButton(selected = chosen, onClick = null)
            Column(Modifier.weight(1f)) {
                Text(found.label, style = MaterialTheme.typography.bodyMedium)
                if (found.kind.isNotEmpty()) MutedText(found.kind)
            }
        }
    }
    TextButton(onClick = onDiscover, enabled = !discovering) { Text(stringResource(R.string.metrics_search_again)) }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AuthFields(source: PromSource, onChange: (PromSource) -> Unit) {
    val kinds = listOf(
        PromSource.AUTH_NONE to R.string.metrics_auth_none,
        PromSource.AUTH_BEARER to R.string.metrics_auth_bearer,
        PromSource.AUTH_BASIC to R.string.metrics_auth_basic,
    )
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        kinds.forEachIndexed { i, (kind, label) ->
            SegmentedButton(
                selected = source.auth == kind,
                onClick = { onChange(source.copy(auth = kind)) },
                shape = SegmentedButtonDefaults.itemShape(i, kinds.size),
                icon = {},
            ) { Text(stringResource(label)) }
        }
    }
    if (source.auth == PromSource.AUTH_BASIC) Field(source.username, R.string.metrics_username) { onChange(source.copy(username = it)) }
    if (source.auth != PromSource.AUTH_NONE) {
        OutlinedTextField(
            source.secret, { onChange(source.copy(secret = it)) },
            label = { Text(stringResource(if (source.auth == PromSource.AUTH_BEARER) R.string.metrics_token else R.string.metrics_password)) },
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            singleLine = true, modifier = Modifier.fillMaxWidth(),
        )
        MutedText(stringResource(R.string.metrics_https_only))
    }
    OutlinedTextField(
        source.ca, { onChange(source.copy(ca = it)) },
        label = { Text(stringResource(R.string.metrics_ca)) },
        minLines = 2, maxLines = 6, modifier = Modifier.fillMaxWidth(),
    )
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(R.string.metrics_skip_verify), Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
        Switch(checked = source.insecureSkipVerify, onCheckedChange = { onChange(source.copy(insecureSkipVerify = it)) })
    }
}

/** [this] in [mode], without the other mode's fields (Go refuses credentials for the proxy). */
private fun PromSource.forMode(mode: String): PromSource = when (mode) {
    PromSource.MODE_PROXY -> copy(mode = mode, url = "", auth = PromSource.AUTH_NONE, username = "", secret = "", ca = "", insecureSkipVerify = false)
    else -> copy(mode = mode, namespace = "", service = "", port = 0, pathPrefix = "", kind = "")
}

@Composable
private fun Field(value: String, label: Int, type: KeyboardType = KeyboardType.Text, onChange: (String) -> Unit) {
    OutlinedTextField(
        value, onChange,
        label = { Text(stringResource(label)) },
        keyboardOptions = KeyboardOptions(keyboardType = type),
        singleLine = true, modifier = Modifier.fillMaxWidth(),
    )
}
