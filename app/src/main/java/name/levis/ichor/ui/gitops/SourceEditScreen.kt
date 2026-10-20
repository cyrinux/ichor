package name.levis.ichor.ui.gitops

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.DiffChange
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.GitOpsEditCheck
import name.levis.ichor.model.ProposeBlock
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.diff.DiffLines
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.kubebrowser.ErrorCard
import name.levis.ichor.ui.kubebrowser.KubeYamlEditor
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Edits one file of an app's source ([path] from the repository root) and opens a pull request
 * with it: the YAML editor (schema help for a Kubernetes manifest), Review (the text diff),
 * Continue (the pull request sheet), then the request's link. Back with unsaved changes asks
 * before discarding them. A [quick] edit (from the file's Quick edit) is applied to the file as
 * read and opens on its review, the pull request titled after it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SourceEditScreen(target: SourceTarget, path: String, onBack: () -> Unit, onAddToken: (() -> Unit)?, quick: QuickEdit? = null) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: SourceEditViewModel = viewModel(factory = factory { SourceEditViewModel(talos.gitOpsRepository, talos.kubeBrowser) })
    val state by vm.state.collectAsStateWithLifecycle()
    val help by vm.help.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) { vm.load(target, path, config?.activeContext to generation, quick) }
    // Back from Settings with a token: read again.
    val block by rememberUpdatedState(state.block)
    LifecycleResumeEffect(Unit) {
        if (block != null) vm.refresh()
        onPauseOrDispose { }
    }
    val edit = (state.edit as? UiState.Loaded)?.data
    var confirmingDiscard by remember { mutableStateOf(false) }

    fun back() {
        when {
            edit == null || edit.result != null -> onBack()
            edit.review != null -> vm.backToEditor()
            edit.changed -> confirmingDiscard = true
            else -> onBack()
        }
    }
    BackHandler(enabled = edit != null) { back() }
    if (confirmingDiscard) {
        ConfirmDialog(
            title = stringResource(R.string.gitops_edit_discard_title),
            text = stringResource(R.string.gitops_edit_discard_text),
            confirm = stringResource(R.string.gitops_edit_discard),
            onConfirm = {
                confirmingDiscard = false
                onBack()
            },
            onDismiss = { confirmingDiscard = false },
            destructive = true,
        )
    }
    val form = edit?.form
    if (edit != null && form != null && edit.formOpen) {
        ProposeSheet(
            form = form,
            tracked = edit.source.tracked,
            reloading = edit.reloading,
            onChange = vm::changeForm,
            onPropose = vm::propose,
            onReload = vm::reload,
            onDismiss = vm::closeForm,
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(path.substringAfterLast('/'), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            path,
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                navigationIcon = { BackButton { back() } },
                actions = {
                    if (edit != null && edit.review == null && edit.result == null) {
                        TextButton(onClick = vm::review) { Text(stringResource(R.string.gitops_edit_review)) }
                    }
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        when (val blocked = state.block) {
            is ProposeBlock.NoToken -> GitHostMissingBox(blocked.host, "", onAddToken, modifier)
            ProposeBlock.NotEditable -> EmptyState(Icons.AutoMirrored.Outlined.InsertDriveFile, stringResource(R.string.gitops_propose_not_editable), modifier)
            null -> when (val s = state.edit) {
                UiState.Loading -> LoadingBox(modifier)
                is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
                is UiState.Loaded -> {
                    val e = s.data
                    when {
                        e.result != null -> ProposeResult(e.result, target.tool, onDone = onBack, modifier = modifier)
                        e.review != null -> EditReview(
                            e.review,
                            onBack = vm::backToEditor,
                            onContinue = vm::openForm,
                            onRetry = vm::review,
                            modifier = modifier,
                            notice = e.notice,
                            check = e.check,
                            onRetryCheck = vm::check,
                        )
                        else -> Column(modifier) {
                            // A quick edit that did not apply to the branch's file says why.
                            e.notice?.let { notice ->
                                MutedText(
                                    notice.asString(),
                                    Modifier.padding(horizontal = 16.dp, vertical = 8.dp).semantics { liveRegion = LiveRegionMode.Polite },
                                )
                            }
                            KubeYamlEditor(
                                draft = e.draft,
                                kind = e.manifest?.kind.orEmpty(),
                                help = help,
                                onChange = vm::changeDraft,
                                onOpenHelp = vm::openHelp,
                                onRetryHelp = vm::retryHelp,
                                onCloseHelp = vm::closeHelp,
                                modifier = Modifier.weight(1f),
                                schemaHelp = e.manifest != null,
                            )
                        }
                    }
                }
            }
        }
    }
}

/** What the draft changes, as a unified diff; Continue opens the pull request sheet. */
@Composable
private fun EditReview(
    review: UiState<FluxDiff>,
    onBack: () -> Unit,
    onContinue: () -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    notice: UiText? = null,
    check: UiState<GitOpsEditCheck>? = null,
    onRetryCheck: () -> Unit = {},
) {
    when (review) {
        UiState.Loading -> Column(modifier.fillMaxSize(), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
            CircularProgressIndicator()
        }
        is UiState.Failed -> Column(modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            ErrorCard(review.message.asString())
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = onBack) { Text(stringResource(R.string.kb_back_to_editor)) }
                TextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
        }
        is UiState.Loaded -> {
            val file = review.data.resources.firstOrNull()
            if (file == null || file.change != DiffChange.CHANGED) {
                Column(modifier.fillMaxSize()) {
                    InfoBox(stringResource(R.string.gitops_edit_no_change), Modifier.weight(1f))
                    OutlinedButton(onClick = onBack, modifier = Modifier.padding(16.dp).navigationBarsPadding()) { Text(stringResource(R.string.kb_back_to_editor)) }
                }
                return
            }
            Column(modifier.fillMaxSize().imePadding()) {
                Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(16.dp)) {
                    notice?.let { MutedText(it.asString(), Modifier.padding(bottom = 8.dp)) }
                    DiffLines(file.lines)
                    if (file.truncated) MutedText(stringResource(R.string.diff_truncated), Modifier.padding(top = 4.dp))
                    check?.let { EditCheckSection(it, onRetry = onRetryCheck, modifier = Modifier.padding(top = 16.dp)) }
                }
                Row(
                    Modifier.padding(16.dp).navigationBarsPadding(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    OutlinedButton(onClick = onBack) { Text(stringResource(R.string.kb_back_to_editor)) }
                    Button(onClick = onContinue) { Text(stringResource(R.string.gitops_edit_continue)) }
                    // The pull request can still be opened: the person decides.
                    if ((check as? UiState.Loaded)?.data?.fails == true) {
                        Text(stringResource(R.string.gitops_check_fails), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.labelLarge)
                    }
                }
            }
        }
    }
}
