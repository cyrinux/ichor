package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.AlertDialog
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.model.PluginCatalogItem
import name.levis.ichor.model.PluginUserItem
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.userMessage

// docs/plugins.md, published by the website workflow (scripts/site-reference.py).
private const val PLUGIN_DOCS_URL = "https://cyrinux.github.io/ichor/plugins/"

/**
 * Settings → Plugins: the built-in CRD plugins (read-only) and the ones the user imported, each
 * with a switch and Remove; Import opens [PluginImportSheet] (paste, file or https URL, then the
 * consent sheet). An imported plugin the core no longer reads is shown off with its reason.
 */
@Composable
fun PluginsSection(plugins: PluginRepository) {
    val scope = rememberCoroutineScope()
    val uriHandler = LocalUriHandler.current
    var builtins by remember { mutableStateOf<List<PluginCatalogItem>>(emptyList()) }
    var imported by remember { mutableStateOf<List<PluginUserItem>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var importing by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<PluginUserItem?>(null) }

    LaunchedEffect(reload) {
        error = try {
            imported = plugins.userPlugins().items
            builtins = plugins.catalog().builtins
            null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            e.userMessage()
        }
    }

    fun change(block: suspend () -> Unit) {
        scope.launch {
            try {
                block()
                error = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.userMessage()
            }
            reload++
        }
    }

    SectionTitle(stringResource(R.string.plugins_title))
    MutedText(stringResource(R.string.plugins_desc))
    error?.let { Text(stringResource(R.string.plugins_load_failed, it), color = MaterialTheme.colorScheme.error) }

    Text(stringResource(R.string.plugins_imported), style = MaterialTheme.typography.titleSmall)
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            if (imported.isEmpty()) {
                MutedText(stringResource(R.string.plugins_empty), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            }
            imported.forEachIndexed { index, item ->
                if (index > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp))
                ImportedRow(
                    item,
                    onEnabled = { on -> change { plugins.setEnabled(item.id, on) } },
                    onRemove = { removing = item },
                )
            }
        }
    }
    OutlinedButton(onClick = { importing = true }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.plugins_import))
    }
    MutedText(stringResource(R.string.plugins_explainer))
    TextButton(onClick = { uriHandler.openUri(PLUGIN_DOCS_URL) }) { Text(stringResource(R.string.plugins_docs)) }

    Text(stringResource(R.string.plugins_builtin), style = MaterialTheme.typography.titleSmall)
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            builtins.forEachIndexed { index, item ->
                if (index > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp))
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text(item.name, style = MaterialTheme.typography.bodyLarge)
                    MutedText(stringResource(R.string.plugins_reads, item.resources.joinToString(", ") { it.resource }))
                }
            }
        }
    }

    if (importing) {
        PluginImportSheet(plugins, onDismiss = {
            importing = false
            reload++
        })
    }
    removing?.let { item ->
        AlertDialog(
            onDismissRequest = { removing = null },
            title = { Text(stringResource(R.string.plugins_remove_title, item.name.ifEmpty { item.id })) },
            text = { Text(stringResource(R.string.plugins_remove_text)) },
            confirmButton = {
                TextButton(onClick = {
                    removing = null
                    change { plugins.remove(item.id) }
                }) { Text(stringResource(R.string.plugins_remove)) }
            },
            dismissButton = { TextButton(onClick = { removing = null }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

@Composable
private fun ImportedRow(item: PluginUserItem, onEnabled: (Boolean) -> Unit, onRemove: () -> Unit) {
    val enabledLabel = stringResource(R.string.plugins_enabled)
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, top = 4.dp, bottom = 4.dp, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(item.name.ifEmpty { item.id }, style = MaterialTheme.typography.bodyLarge)
            MutedText(item.id)
            if (item.usable) {
                MutedText(stringResource(R.string.plugins_reads, item.resources.joinToString(", ") { it.resource }))
            } else {
                Text(
                    stringResource(R.string.plugins_off_reason, item.error),
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
        Switch(
            checked = item.enabled,
            onCheckedChange = onEnabled,
            enabled = item.usable,
            modifier = Modifier.semantics { contentDescription = enabledLabel },
        )
        IconButton(onClick = onRemove) {
            Icon(Icons.Outlined.Delete, contentDescription = stringResource(R.string.plugins_remove))
        }
    }
}
