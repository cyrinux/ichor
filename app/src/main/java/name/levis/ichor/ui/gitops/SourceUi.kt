package name.levis.ichor.ui.gitops

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.GitOpsSource
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.SourceReadPath
import name.levis.ichor.model.gitHostWebUrl
import name.levis.ichor.model.sourceRowLabel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.ui.uiStateOf

/** The tool names `GitOpsSource` takes. */
const val SOURCE_TOOL_ARGO = "argocd"
const val SOURCE_TOOL_FLUX = "flux"

/**
 * The "Source" rows of an app's detail: one per Git source (an Argo CD source read only for its
 * value files is left out), "path @ short sha"; a Helm chart shows its name and version and is
 * not tappable. Read again when [revision] changes (a sync or a reconcile ran). [onOpen] opens
 * the browser on the source at that index of `GitOpsSource`'s list.
 */
@Composable
fun GitOpsSourceSection(
    tool: String,
    kind: String,
    namespace: String,
    name: String,
    revision: String,
    onOpen: ((Int) -> Unit)?,
) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val state by produceState<UiState<GitOpsSource>>(UiState.Loading, tool, kind, namespace, name, revision) {
        value = uiStateOf { talos.gitOpsRepository.source(tool, kind, namespace, name) }
    }
    when (val s = state) {
        UiState.Loading -> Unit
        is UiState.Failed -> Column {
            SectionTitle(stringResource(R.string.gitops_source_title))
            MutedText(s.message.asString())
        }
        is UiState.Loaded -> {
            val rows = s.data.sources.withIndex().filterNot { it.value.isValuesOnly }
            if (rows.isEmpty()) return
            Column {
                SectionTitle(stringResource(R.string.gitops_source_title))
                rows.forEach { (index, source) ->
                    val readable = !source.isChart && source.readPath != SourceReadPath.NONE
                    SourceRow(source, onClick = if (readable && onOpen != null) ({ onOpen(index) }) else null)
                }
                if (s.data.note.isNotEmpty()) MutedText(s.data.note)
            }
        }
    }
}

@Composable
private fun SourceRow(source: GitSource, onClick: (() -> Unit)?) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val clickable = if (onClick != null) {
        Modifier.clickable(onClickLabel = stringResource(R.string.gitops_source_title), onClick = onClick)
    } else {
        Modifier
    }
    Row(Modifier.fillMaxWidth().then(clickable).padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(if (source.isChart) Icons.Outlined.Inventory2 else Icons.Outlined.FolderOpen, contentDescription = null, tint = muted, modifier = Modifier.size(20.dp))
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            if (source.isChart) {
                Text(
                    stringResource(R.string.gitops_source_chart, source.chart.ifEmpty { source.repoURL }, source.tracked),
                    style = MaterialTheme.typography.bodyMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            } else {
                Text(sourceRowLabel(source), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            MutedText(repoLabel(source), maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!source.isChart && source.readPath == SourceReadPath.NONE) MutedText(stringResource(R.string.gitops_source_not_browsable))
        }
        if (onClick != null) Icon(Icons.Outlined.ChevronRight, contentDescription = null, tint = muted)
    }
}

/** The repository as read: its page without the scheme, else its address. */
internal fun repoLabel(source: GitSource): String = source.webURL.removePrefix("https://").ifEmpty { source.repoURL }

/**
 * Puts a forge link on the Git host whose alias it names (Settings → Git hosts), so a source
 * Argo CD or Flux reach by an in-cluster or SSH name opens on the forge's own address.
 */
@Composable
fun rememberGitWebUrl(): (String) -> String {
    val store = (LocalContext.current.applicationContext as? TalosApp)?.gitHostStore ?: return { it }
    val hosts by store.hosts.collectAsStateWithLifecycle()
    return remember(hosts) { { url: String -> gitHostWebUrl(url, hosts) } }
}

/**
 * No token is saved for [host]: says so, with a shortcut to Settings → Git hosts and, when the
 * repository has a page, "Open in browser" ([url]).
 */
@Composable
fun GitHostMissingBox(host: String, url: String, onAddToken: (() -> Unit)?, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val web = rememberGitWebUrl()
    EmptyState(Icons.Outlined.Key, stringResource(R.string.gitops_source_no_token, host), modifier) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (onAddToken != null) Button(onClick = onAddToken) { Text(stringResource(R.string.gitops_source_add_token)) }
            if (url.isNotEmpty()) OpenInBrowserButton { openUrl(context, web(url)) }
        }
    }
}

@Composable
internal fun OpenInBrowserButton(onClick: () -> Unit) {
    OutlinedButton(onClick = onClick) {
        Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = null, modifier = Modifier.size(18.dp))
        Text(stringResource(R.string.gitops_source_open_browser), modifier = Modifier.padding(start = 8.dp))
    }
}
