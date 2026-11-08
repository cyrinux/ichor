package name.levis.ichor.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.model.PluginIndex
import name.levis.ichor.model.PluginIndexItem
import name.levis.ichor.model.PluginUserItem
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.LoadFailureRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
import name.levis.ichor.ui.userMessage

/**
 * Settings → Plugins → Browse the catalog: the plugin files of the index the preferences name ("" for
 * the project's). Import downloads one, refused unless it has the digest the index published, has the
 * core check it, and opens the same consent sheet as any other import: only its Enable stores it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PluginIndexScreen(plugins: PluginRepository, preferences: UiPreferences, onBack: () -> Unit) {
    val indexUrl by preferences.pluginIndexUrl.collectAsStateWithLifecycle()
    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(stringResource(R.string.plugin_index_browse)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            PluginIndexContent(plugins, indexUrl.trim())
        }
    }
}

@Composable
private fun PluginIndexContent(plugins: PluginRepository, indexUrl: String) {
    val scope = rememberCoroutineScope()
    var index by remember { mutableStateOf<PluginIndex?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var importingId by remember { mutableStateOf<String?>(null) }
    var consent by remember { mutableStateOf<Pair<String, PluginUserItem>?>(null) }

    LaunchedEffect(indexUrl, reload) {
        error = try {
            index = plugins.index(indexUrl)
            null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            e.userMessage()
        }
    }

    fun import(item: PluginIndexItem) {
        importingId = item.id
        error = null
        scope.launch {
            try {
                val yaml = plugins.downloadFromIndex(item.url, item.sha256)
                consent = yaml to plugins.check(yaml)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.userMessage()
            }
            importingId = null
        }
    }

    MutedText(stringResource(R.string.plugin_index_note))
    error?.let { LoadFailureRow(it, onRetry = { reload++ }) }

    val current = index
    when {
        current != null -> {
            MutedText(stringResource(R.string.plugin_index_source, current.url))
            if (current.plugins.isEmpty()) MutedText(stringResource(R.string.plugin_index_empty))
            current.plugins.forEach { item ->
                IndexRow(item, importing = importingId == item.id, enabled = importingId == null, onImport = { import(item) })
            }
        }
        error == null -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            MutedText(stringResource(R.string.plugin_index_loading))
        }
    }

    consent?.let { (yaml, item) ->
        PluginConsentSheet(plugins, yaml, item, onDismiss = {
            consent = null
            reload++
        })
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun IndexRow(item: PluginIndexItem, importing: Boolean, enabled: Boolean, onImport: () -> Unit) {
    val uriHandler = LocalUriHandler.current
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                AppIconTile(item.iconApp, size = 40.dp)
                Column(Modifier.weight(1f)) {
                    Text(item.title, style = MaterialTheme.typography.bodyLarge)
                    if (item.description.isNotEmpty()) MutedText(item.description, maxLines = 1)
                }
                when {
                    item.builtin -> Badge(stringResource(R.string.plugins_builtin))
                    item.installed -> Badge(stringResource(R.string.plugin_index_installed))
                    importing -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    else -> IchorOutlinedButton(onClick = onImport, enabled = enabled) {
                        Text(stringResource(R.string.plugin_index_import))
                    }
                }
            }
            if (item.resources.isNotEmpty()) {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    item.resources.forEach { Badge(it) }
                }
            }
            if (item.maintainer.isNotEmpty()) MutedText(stringResource(R.string.plugin_index_maintainer, item.maintainer))
            if (item.website.isNotEmpty()) {
                IchorTextButton(onClick = { uriHandler.openUri(item.website) }) { Text(item.website) }
            }
        }
    }
}

@Composable
private fun Badge(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        modifier = Modifier
            .background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(8.dp))
            .padding(horizontal = 8.dp, vertical = 4.dp),
    )
}
