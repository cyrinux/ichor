package name.levis.ichor.ui.gitops

import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
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
import name.levis.ichor.model.ProposeBlock
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.QuickEditTargets
import name.levis.ichor.model.SourceReadPath
import name.levis.ichor.model.forgeFileUrl
import name.levis.ichor.model.isYamlPath
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.kubebrowser.YamlColors
import name.levis.ichor.ui.kubebrowser.highlightYamlLine
import name.levis.ichor.ui.settings.openUrl

/**
 * One file of an app's source, read only: monospace, selectable, line by line (a large values
 * file scrolls smoothly), long lines scrolled sideways; Copy, Open in browser and Edit ([onEdit],
 * a pull request through the forge: disabled with the reason when there is no token for its
 * host or no forge). A file over 1 MiB comes without its content: a note sends to the browser.
 * [line] (1-based, from a diff's "Open source file"): scrolled to and highlighted. Quick edit
 * (a workload's image tag or replicas, offered when Edit is) checks the edit on the file, then
 * [onQuickEdit] opens the editor's review with it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SourceFileScreen(
    target: SourceTarget,
    path: String,
    onBack: () -> Unit,
    onAddToken: (() -> Unit)?,
    onEdit: (() -> Unit)? = null,
    onQuickEdit: ((QuickEdit) -> Unit)? = null,
    line: Int? = null,
) {
    val context = LocalContext.current
    val talos = context.applicationContext as TalosApp
    val vm: SourceFileViewModel = viewModel(factory = factory { SourceFileViewModel(talos.gitOpsRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val quickSheet by vm.quick.sheet.collectAsStateWithLifecycle()
    val applied by vm.quick.applied.collectAsStateWithLifecycle()
    val onQuick by rememberUpdatedState(onQuickEdit)
    LaunchedEffect(applied) {
        val edit = applied ?: return@LaunchedEffect
        vm.quick.consumeApplied()
        onQuick?.invoke(edit)
    }
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) { vm.load(target, path, config?.activeContext to generation) }
    // Back from Settings with a token: read again.
    val missingHost by rememberUpdatedState(state.missingHost)
    val noToken by rememberUpdatedState(state.editBlock is ProposeBlock.NoToken)
    LifecycleResumeEffect(Unit) {
        if (missingHost != null || noToken) vm.refresh()
        onPauseOrDispose { }
    }
    val view = (state.file as? UiState.Loaded)?.data
    // Quick edits follow Edit's rules: offered only when the file can be proposed.
    val quick = state.quick?.takeIf { view != null && !view.file.truncated && onEdit != null && onQuickEdit != null && state.editBlock == null }
    val sheet = quickSheet
    if (quick != null && sheet != null) {
        when (sheet.kind) {
            QuickSheetKind.IMAGE -> QuickEditImageSheet(sheet, quick, onApply = vm.quick::applyEdit, onDismiss = vm.quick::close)
            QuickSheetKind.REPLICAS -> QuickEditReplicasSheet(sheet, quick, onApply = vm.quick::applyEdit, onDismiss = vm.quick::close)
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(path.substringAfterLast('/'), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        val sha = view?.let { if (it.source.readPath == SourceReadPath.FLUX) it.file.sha.ifEmpty { it.source.ref } else it.source.ref }.orEmpty()
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
                    if (quick != null) QuickEditMenu(quick, onOpen = { vm.quick.open(it) })
                    if (view != null && !view.file.truncated && onEdit != null) {
                        TooltipIconButton(Icons.Outlined.Edit, stringResource(R.string.gitops_edit), onClick = onEdit, enabled = state.editBlock == null)
                    }
                    if (view != null && !view.file.truncated) {
                        val copied = stringResource(R.string.gitops_source_copied)
                        TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.gitops_source_copy), onClick = {
                            copyToClipboard(context, path, view.file.content)
                            Toast.makeText(context, copied, Toast.LENGTH_SHORT).show()
                        })
                    }
                    val url = view?.let { forgeFileUrl(it.source.webURL, it.source.ref, path) }.orEmpty()
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
                val url = forgeFileUrl(s.data.source.webURL, s.data.source.ref, path)
                EmptyState(Icons.AutoMirrored.Outlined.InsertDriveFile, stringResource(R.string.gitops_source_truncated), modifier) {
                    if (url.isNotEmpty()) OpenInBrowserButton { openUrl(context, url) }
                }
            } else {
                Column(modifier) {
                    val block = state.editBlock
                    if (onEdit != null && block != null) EditBlockNote(block, onAddToken)
                    SourceLines(s.data.file.content, yaml = isYamlPath(path), line = line, modifier = Modifier.weight(1f))
                }
            }
        }
    }
}

/** The Quick edit button and its menu: Change image tag, Set replicas, each when the file offers it. */
@Composable
private fun QuickEditMenu(targets: QuickEditTargets, onOpen: (QuickSheetKind) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        TooltipIconButton(Icons.Outlined.Bolt, stringResource(R.string.gitops_quick_edit), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            if (targets.canEditImage) {
                DropdownMenuItem(text = { Text(stringResource(R.string.gitops_quick_image)) }, onClick = {
                    open = false
                    onOpen(QuickSheetKind.IMAGE)
                })
            }
            if (targets.canEditReplicas) {
                DropdownMenuItem(text = { Text(stringResource(R.string.gitops_quick_replicas)) }, onClick = {
                    open = false
                    onOpen(QuickSheetKind.REPLICAS)
                })
            }
        }
    }
}

/** Why Edit is disabled, with a shortcut to Settings → Git hosts when a token is what is missing. */
@Composable
private fun EditBlockNote(block: ProposeBlock, onAddToken: (() -> Unit)?) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val text = when (block) {
            is ProposeBlock.NoToken -> stringResource(R.string.gitops_propose_no_token, block.host)
            ProposeBlock.NotEditable -> stringResource(R.string.gitops_propose_not_editable)
        }
        MutedText(text, Modifier.weight(1f))
        if (block is ProposeBlock.NoToken && onAddToken != null) {
            TextButton(onClick = onAddToken) { Text(stringResource(R.string.gitops_source_add_token)) }
        }
    }
}

/** Past this, a line is cut on screen (Copy still copies it whole): the column stays measurable. */
private const val MAX_LINE_CHARS = 1500

/** Lines kept above the highlighted line when it is scrolled to, so its context shows. */
private const val LINES_ABOVE_TARGET = 3

/**
 * [text] line by line in a lazy list, monospace and selectable, scrolled sideways as one block;
 * [line] (1-based) scrolled to once and highlighted.
 */
@Composable
private fun SourceLines(text: String, yaml: Boolean, line: Int? = null, modifier: Modifier = Modifier) {
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
    val target = line?.takeIf { it in 1..lines.size }
    val listState = rememberLazyListState()
    LaunchedEffect(target) {
        if (target != null) listState.scrollToItem((target - 1 - LINES_ABOVE_TARGET).coerceAtLeast(0))
    }
    val highlight = MaterialTheme.colorScheme.primaryContainer
    BoxWithConstraints(modifier.fillMaxSize()) {
        val width = max(contentWidth, maxWidth)
        SelectionContainer {
            LazyColumn(
                Modifier.horizontalScroll(rememberScrollState()).width(width).fillMaxHeight(),
                state = listState,
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            ) {
                itemsIndexed(lines) { index, text ->
                    Text(
                        if (yaml) remember(text, colors) { highlightYamlLine(text, colors) } else AnnotatedString(text),
                        style = style,
                        softWrap = false,
                        maxLines = 1,
                        modifier = if (index + 1 == target) Modifier.fillMaxWidth().background(highlight) else Modifier,
                    )
                }
            }
        }
    }
}
