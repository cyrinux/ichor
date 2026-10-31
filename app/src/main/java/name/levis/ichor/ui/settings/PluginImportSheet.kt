package name.levis.ichor.ui.settings

import android.content.Context
import android.net.Uri
import androidx.annotation.StringRes
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.model.PLUGIN_MAX_BYTES
import name.levis.ichor.model.PluginConsent
import name.levis.ichor.model.PluginUserItem
import name.levis.ichor.model.consent
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.importconfig.QrScanner
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage
import name.levis.ichor.util.readBounded

/** Where the plugin file comes from. */
private enum class PluginSource(@StringRes val label: Int) {
    PASTE(R.string.plugins_source_paste),
    FILE(R.string.plugins_source_file),
    URL(R.string.plugins_source_url),
    SCAN(R.string.plugins_source_scan),
}

/**
 * Imports a plugin file: pasted, picked with the document picker or downloaded from an https
 * URL; Check has the core validate it, storing nothing. The consent step then shows what it
 * reads (a Secret shown in clear in red) and can change; only Enable imports it, on. Cancel or
 * closing the sheet leaves the stored plugins as they were.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun PluginImportSheet(plugins: PluginRepository, onDismiss: () -> Unit) {
    // The file checked and what it showed: nothing is stored until Enable.
    var checked by remember { mutableStateOf<Pair<String, PluginUserItem>?>(null) }
    var enabling by remember { mutableStateOf(false) }

    ModalBottomSheet(onDismissRequest = { if (!enabling) onDismiss() }) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val current = checked
            if (current == null) {
                ImportStep(plugins, onChecked = { yaml, item -> checked = yaml to item })
            } else {
                ConsentContent(plugins, current.first, current.second, enabling, { enabling = it }, onDismiss)
            }
        }
    }
}

/**
 * The consent step alone, for a file already downloaded and checked (the catalog): [item] is
 * what [PluginRepository.check] said of [yaml]. Only Enable stores it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun PluginConsentSheet(plugins: PluginRepository, yaml: String, item: PluginUserItem, onDismiss: () -> Unit) {
    var enabling by remember { mutableStateOf(false) }
    ModalBottomSheet(onDismissRequest = { if (!enabling) onDismiss() }) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            ConsentContent(plugins, yaml, item, enabling, { enabling = it }, onDismiss)
        }
    }
}

/** [ConsentStep] with its Enable: stores [yaml] and closes, or says why not. */
@Composable
private fun ConsentContent(
    plugins: PluginRepository,
    yaml: String,
    item: PluginUserItem,
    enabling: Boolean,
    onEnabling: (Boolean) -> Unit,
    onDismiss: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var failure by remember { mutableStateOf<String?>(null) }
    ConsentStep(
        item.consent(),
        enabling = enabling,
        failure = failure,
        onEnable = {
            onEnabling(true)
            failure = null
            scope.launch {
                try {
                    plugins.import(yaml)
                    onDismiss()
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    failure = e.userMessage()
                }
                onEnabling(false)
            }
        },
        onCancel = onDismiss,
    )
}

@Composable
private fun ImportStep(plugins: PluginRepository, onChecked: (yaml: String, item: PluginUserItem) -> Unit) {
    val context = LocalContext.current
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    var source by remember { mutableStateOf(PluginSource.PASTE) }
    var yaml by remember { mutableStateOf("") }
    var url by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // A new key starts a new scan: the scanner stops once it delivered a code.
    var scanKey by remember { mutableIntStateOf(0) }

    fun run(block: suspend () -> Unit) {
        busy = true
        error = null
        scope.launch {
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.userMessage()
            }
            busy = false
        }
    }

    fun check(text: String) = run { onChecked(text, plugins.check(text)) }

    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri != null) {
            readPluginFile(context, uri).fold(
                onSuccess = {
                    yaml = it
                    check(it)
                },
                onFailure = { error = if (it is IllegalArgumentException) context.getString(R.string.plugins_file_too_large) else it.userMessage() },
            )
        }
    }

    Text(stringResource(R.string.plugins_import), style = MaterialTheme.typography.titleLarge)
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        PluginSource.entries.forEachIndexed { index, s ->
            SegmentedButton(
                selected = s == source,
                onClick = {
                    source = s
                    error = null
                },
                shape = SegmentedButtonDefaults.itemShape(index, PluginSource.entries.size),
            ) { Text(stringResource(s.label)) }
        }
    }
    when (source) {
        PluginSource.PASTE -> {
            OutlinedTextField(
                value = yaml,
                onValueChange = { if (it.length <= PLUGIN_MAX_BYTES) yaml = it },
                label = { Text(stringResource(R.string.plugins_field_yaml)) },
                textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                minLines = 6,
                modifier = Modifier.fillMaxWidth().heightIn(max = 320.dp),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                IchorOutlinedButton(onClick = {
                    scope.launch {
                        clipboard.getClipEntry()?.clipData?.takeIf { it.itemCount > 0 }
                            ?.getItemAt(0)?.coerceToText(context)?.toString()?.let { yaml = it }
                    }
                }) { Text(stringResource(R.string.plugins_paste_clipboard)) }
                IchorButton(onClick = { check(yaml) }, enabled = yaml.isNotBlank() && !busy) { Text(stringResource(R.string.plugins_check)) }
            }
        }
        PluginSource.FILE ->
            IchorButton(onClick = { picker.launch(arrayOf("*/*")) }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.plugins_pick_file))
            }
        PluginSource.URL -> {
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text(stringResource(R.string.plugins_field_url)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
            IchorButton(
                onClick = {
                    run {
                        val text = plugins.download(url.trim())
                        yaml = text
                        onChecked(text, plugins.check(text))
                    }
                },
                enabled = url.isNotBlank() && !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.plugins_download)) }
        }
        PluginSource.SCAN -> {
            // A plugin link is downloaded, a plugin file (one code, or several from
            // `just qr-parts`) taken as is; either way it is checked, then consented to.
            key(scanKey) {
                Box(Modifier.fillMaxWidth().height(300.dp).clip(MaterialTheme.shapes.medium)) {
                    QrScanner(
                        onScanned = { text ->
                            run {
                                val file = plugins.scanned(text)
                                yaml = file
                                onChecked(file, plugins.check(file))
                            }
                        },
                        modifier = Modifier.fillMaxSize(),
                    )
                }
            }
            MutedText(stringResource(R.string.plugins_scan_hint))
            if (error != null && !busy) {
                IchorOutlinedButton(
                    onClick = {
                        error = null
                        scanKey++
                    },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.plugins_scan_again)) }
            }
        }
    }
    if (busy) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            MutedText(stringResource(R.string.plugins_working))
        }
    }
    error?.let {
        Column(Modifier.semantics { liveRegion = LiveRegionMode.Polite }) {
            Text(stringResource(R.string.plugins_import_failed), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
            // The core's problems are "plugin.yaml:<line>: …" lines: kept as they are.
            Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace))
        }
    }
}

@Composable
private fun ConsentStep(consent: PluginConsent, enabling: Boolean, failure: String?, onEnable: () -> Unit, onCancel: () -> Unit) {
    Text(stringResource(R.string.plugins_consent_title, consent.name), style = MaterialTheme.typography.titleLarge)
    if (consent.website.isNotEmpty()) MutedText(consent.website)
    if (consent.replaces) Text(stringResource(R.string.plugins_replaces), color = LocalStatusColors.current.warn)
    Text(stringResource(R.string.plugins_consent_reads), style = MaterialTheme.typography.titleSmall)
    consent.resources.forEach { r ->
        Column {
            Text(r.name, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
            if (r.secret) {
                Text(
                    stringResource(if (r.revealed) R.string.plugins_secret_revealed else R.string.plugins_secret_masked),
                    color = if (r.revealed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
    Text(stringResource(R.string.plugins_consent_changes), style = MaterialTheme.typography.titleSmall)
    if (consent.readOnly) MutedText(stringResource(R.string.plugins_consent_read_only))
    consent.actions.forEach { a ->
        Column {
            Text(a.label, style = MaterialTheme.typography.bodyMedium)
            MutedText(listOf(a.verb, a.needs).filter { it.isNotEmpty() }.joinToString(" · "))
        }
    }
    MutedText(stringResource(R.string.plugins_consent_note))
    failure?.let {
        Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite })
    }
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
        IchorTextButton(onClick = onCancel, enabled = !enabling) { Text(stringResource(R.string.common_cancel)) }
        IchorButton(onClick = onEnable, enabled = !enabling) { Text(stringResource(R.string.plugins_enable)) }
    }
}

/** The picked file's text, at most [PLUGIN_MAX_BYTES] (more is an IllegalArgumentException). */
private fun readPluginFile(context: Context, uri: Uri): Result<String> = runCatching {
    context.contentResolver.openInputStream(uri)?.use { readBounded(it, PLUGIN_MAX_BYTES).decodeToString() }
        ?: error("cannot open the file")
}
