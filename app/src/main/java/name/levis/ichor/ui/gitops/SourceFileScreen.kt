package name.levis.ichor.ui.gitops

import android.widget.Toast
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.max
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.forgeFileUrl
import name.levis.ichor.model.isYamlPath
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.kubebrowser.YamlColors
import name.levis.ichor.ui.kubebrowser.highlightYamlLine
import name.levis.ichor.ui.settings.openUrl

/**
 * One file of an app's source, read only: monospace, selectable, line by line (a large values
 * file scrolls smoothly), long lines scrolled sideways; Copy and Open in browser. A file over
 * 1 MiB comes without its content: a note sends to the browser.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SourceFileScreen(target: SourceTarget, path: String, onBack: () -> Unit, onAddToken: (() -> Unit)?) {
    val context = LocalContext.current
    val talos = context.applicationContext as TalosApp
    val vm: SourceFileViewModel = viewModel(factory = factory { SourceFileViewModel(talos.gitOpsRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) { vm.load(target, path, config?.activeContext to generation) }
    // Back from Settings with a token: read again.
    val missingHost by rememberUpdatedState(state.missingHost)
    LifecycleResumeEffect(Unit) {
        if (missingHost != null) vm.refresh()
        onPauseOrDispose { }
    }
    val view = (state.file as? UiState.Loaded)?.data

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(path.substringAfterLast('/'), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        val sha = view?.let { it.file.sha.ifEmpty { it.source.revision } }.orEmpty()
                        Text(
                            if (sha.isEmpty()) path else "$path @ ${shortRevision(sha)}",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (view != null && !view.file.truncated) {
                        val copied = stringResource(R.string.gitops_source_copied)
                        TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.gitops_source_copy), onClick = {
                            copyToClipboard(context, path, view.file.content)
                            Toast.makeText(context, copied, Toast.LENGTH_SHORT).show()
                        })
                    }
                    val url = view?.let { forgeFileUrl(it.source.webURL, it.file.sha.ifEmpty { it.source.ref }, path) }.orEmpty()
                    if (url.isNotEmpty()) {
                        TooltipIconButton(Icons.AutoMirrored.Outlined.OpenInNew, stringResource(R.string.gitops_source_open_browser), onClick = { openUrl(context, url) })
                    }
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::refresh)
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        val host = state.missingHost
        if (host != null) GitHostMissingBox(host, "", onAddToken, modifier) else when (val s = state.file) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> if (s.data.file.truncated) {
                val url = forgeFileUrl(s.data.source.webURL, s.data.file.sha.ifEmpty { s.data.source.ref }, path)
                EmptyState(Icons.AutoMirrored.Outlined.InsertDriveFile, stringResource(R.string.gitops_source_truncated), modifier) {
                    if (url.isNotEmpty()) OpenInBrowserButton { openUrl(context, url) }
                }
            } else {
                SourceLines(s.data.file.content, yaml = isYamlPath(path), modifier)
            }
        }
    }
}

/** Past this, a line is cut on screen (Copy still copies it whole): the column stays measurable. */
private const val MAX_LINE_CHARS = 1500

/** [text] line by line in a lazy list, monospace and selectable, scrolled sideways as one block. */
@Composable
private fun SourceLines(text: String, yaml: Boolean, modifier: Modifier = Modifier) {
    val colors = YamlColors(
        key = MaterialTheme.colorScheme.primary,
        comment = MaterialTheme.colorScheme.onSurfaceVariant,
        dash = MaterialTheme.colorScheme.tertiary,
    )
    val style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace)
    val lines = remember(text) {
        text.removeSuffix("\n").split('\n').map { it.replace("\t", "    ").take(MAX_LINE_CHARS) }
    }
    val longest = remember(lines) { lines.maxOfOrNull { it.length } ?: 0 }
    val measurer = rememberTextMeasurer()
    val charWidth = remember(style) { measurer.measure("0", style).size.width }
    val density = LocalDensity.current
    val contentWidth = with(density) { (charWidth * longest).toDp() } + 32.dp
    BoxWithConstraints(modifier.fillMaxSize()) {
        val width = max(contentWidth, maxWidth)
        SelectionContainer {
            LazyColumn(
                Modifier.horizontalScroll(rememberScrollState()).width(width).fillMaxHeight(),
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            ) {
                items(lines) { line ->
                    Text(
                        if (yaml) remember(line, colors) { highlightYamlLine(line, colors) } else AnnotatedString(line),
                        style = style,
                        softWrap = false,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}
