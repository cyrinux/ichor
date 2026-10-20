package name.levis.ichor.ui.gitops

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.GitEntry
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.GitTree
import name.levis.ichor.model.SourceReadPath
import name.levis.ichor.model.shortRevision
import name.levis.ichor.model.sourceBreadcrumbs
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.util.formatBytes

/**
 * The files an Argo CD or Flux app deploys, at the revision the cluster runs: a header with the
 * repository, the followed ref and the commit, a breadcrumb up to the repository root, then the
 * directory's folders and files. Opens at [startDir] (null: the source's own path). Flux reads
 * the source-controller's artifact; Argo CD reads the Git host with its saved token, and without
 * one says so with a shortcut to Settings ([onAddToken]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SourceBrowserScreen(
    target: SourceTarget,
    startDir: String?,
    onBack: () -> Unit,
    onFile: (String) -> Unit,
    onAddToken: (() -> Unit)?,
    /** A Find in repository hit: the file at that line. */
    onHit: (String, Int) -> Unit = { path, _ -> onFile(path) },
) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: SourceBrowserViewModel = viewModel(factory = factory { SourceBrowserViewModel(talos.gitOpsRepository) })
    val source by vm.source.collectAsStateWithLifecycle()
    val listing by vm.listing.collectAsStateWithLifecycle()
    val trail by vm.trail.collectAsStateWithLifecycle()
    val search by vm.search.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) { vm.load(target, startDir, config?.activeContext to generation) }
    BackHandler(enabled = trail.isNotEmpty() && !search.open) { vm.back() }
    BackHandler(enabled = search.open) { vm.closeSearch() }
    // Back from Settings with a token: read again.
    val missingHost by rememberUpdatedState(listing.missingHost)
    LifecycleResumeEffect(Unit) {
        if (missingHost != null) vm.refresh()
        onPauseOrDispose { }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        val app = target as? SourceTarget.App
                        Text(stringResource(if (app != null) R.string.gitops_source_title else R.string.repo_title), maxLines = 1)
                        val subtitle = app?.name ?: (source as? UiState.Loaded)?.data?.let { repoLabel(it) }.orEmpty()
                        Text(subtitle, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                    }
                },
                navigationIcon = {
                    BackButton {
                        when {
                            search.open -> vm.closeSearch()
                            trail.isNotEmpty() -> vm.back()
                            else -> onBack()
                        }
                    }
                },
                actions = {
                    val searchable = (source as? UiState.Loaded)?.data?.readPath?.let { it != SourceReadPath.NONE } == true
                    if (searchable && !search.open) {
                        TooltipIconButton(Icons.Outlined.Search, stringResource(R.string.source_search), onClick = vm::openSearch)
                    }
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::refresh)
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        when (val s = source) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> listing.missingHost?.let { GitHostMissingBox(it, "", onAddToken, modifier) }
                ?: ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> Column(modifier.fillMaxSize()) {
                val src = s.data
                SourceHeader(src, (listing.tree as? UiState.Loaded)?.data)
                if (src.readPath == SourceReadPath.NONE) {
                    NotBrowsable(src)
                } else if (search.open) {
                    HorizontalDivider()
                    SourceSearchPane(search, onSearch = vm::search, onClose = vm::closeSearch, onHit = onHit)
                } else {
                    Breadcrumb(listing.dir, vm::open)
                    HorizontalDivider()
                    Listing(
                        src, listing, onDir = vm::open, onFile = onFile, onAddToken = onAddToken, onRetry = vm::refresh,
                        // The cluster repository's root lists the pull requests opened from the phone on it.
                        header = if (target == SourceTarget.Repo && listing.dir.isEmpty()) {
                            { ProposalsCard(talos.gitOpsRepository, "", "", "", "", refresh = src.revision) }
                        } else {
                            null
                        },
                    )
                }
            }
        }
    }
}

/** The repository, the followed ref and the commit; a tap opens the repository's page. */
@Composable
private fun SourceHeader(src: GitSource, tree: GitTree?) {
    val context = LocalContext.current
    val web = rememberGitWebUrl()
    val sha = src.revision.ifEmpty { tree?.sha.orEmpty() }
    val open = if (src.webURL.isNotEmpty()) {
        Modifier.clickable(onClickLabel = stringResource(R.string.gitops_source_open_browser)) { openUrl(context, web(src.webURL)) }
    } else {
        Modifier
    }
    Column(Modifier.fillMaxWidth().then(open).padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(repoLabel(src), style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
        val tracked = stringResource(R.string.gitops_source_tracked, src.tracked)
        MutedText(if (sha.isEmpty()) tracked else "$tracked · ${shortRevision(sha)}", maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** "Repository root › apps › podinfo": each part opens that directory. */
@Composable
private fun Breadcrumb(dir: String, onOpen: (String) -> Unit) {
    val parts = sourceBreadcrumbs(dir)
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TextButton(onClick = { onOpen("") }, enabled = parts.isNotEmpty()) { Text(stringResource(R.string.gitops_source_root)) }
        parts.forEachIndexed { i, (label, path) ->
            Icon(Icons.Outlined.ChevronRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(16.dp))
            TextButton(onClick = { onOpen(path) }, enabled = i < parts.lastIndex) { Text(label, fontFamily = FontFamily.Monospace) }
        }
    }
}

@Composable
private fun Listing(
    src: GitSource,
    listing: SourceListing,
    onDir: (String) -> Unit,
    onFile: (String) -> Unit,
    onAddToken: (() -> Unit)?,
    onRetry: () -> Unit,
    header: (@Composable () -> Unit)? = null,
) {
    val host = listing.missingHost
    if (host != null) {
        GitHostMissingBox(host, src.webURL, onAddToken)
        return
    }
    when (val t = listing.tree) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(t.message, onRetry)
        is UiState.Loaded -> if (t.data.entries.isEmpty()) {
            EmptyState(Icons.Outlined.FolderOpen, stringResource(R.string.gitops_source_empty))
        } else {
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 4.dp)) {
                header?.let { h -> item(key = "header") { Box(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) { h() } } }
                items(t.data.entries, key = { it.path }) { entry ->
                    EntryRow(entry) { if (entry.isDir) onDir(entry.path) else onFile(entry.path) }
                }
            }
        }
    }
}

@Composable
private fun EntryRow(entry: GitEntry, onClick: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (entry.isDir) Icons.Outlined.Folder else Icons.AutoMirrored.Outlined.InsertDriveFile,
            contentDescription = null,
            tint = if (entry.isDir) MaterialTheme.colorScheme.primary else muted,
            modifier = Modifier.size(20.dp),
        )
        Text(
            entry.name,
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f).padding(start = 12.dp),
        )
        if (entry.sops) {
            Icon(
                Icons.Outlined.Lock,
                contentDescription = stringResource(R.string.sops_encrypted),
                tint = muted,
                modifier = Modifier.padding(start = 8.dp).size(16.dp),
            )
        }
        if (!entry.isDir) MutedText(formatBytes(entry.size), modifier = Modifier.padding(start = 8.dp))
    }
}

/** A source with no tree to read: why, and its page when it has one. */
@Composable
private fun NotBrowsable(src: GitSource) {
    val context = LocalContext.current
    val web = rememberGitWebUrl()
    val text = if (src.isChart) {
        stringResource(R.string.gitops_source_chart, src.chart.ifEmpty { src.repoURL }, src.tracked)
    } else {
        stringResource(R.string.gitops_source_not_browsable)
    }
    EmptyState(Icons.Outlined.FolderOpen, text) {
        if (src.webURL.isNotEmpty()) OpenInBrowserButton { openUrl(context, web(src.webURL)) }
    }
}
