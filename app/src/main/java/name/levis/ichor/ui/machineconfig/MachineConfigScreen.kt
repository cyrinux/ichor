package name.levis.ichor.ui.machineconfig

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Commit
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.DriftFix
import name.levis.ichor.model.MultiConfigNodeState
import name.levis.ichor.model.configRole
import name.levis.ichor.model.PatchPrefill
import name.levis.ichor.model.SnippetPrefill
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigApplyState
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.SecureWhile
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.AllowLandscape
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/** The ways the config is shown: field by field, or as its YAML text; and the node's META keys. */
private enum class ConfigView(@StringRes val label: Int) {
    FIELDS(R.string.machine_config_view_fields),
    YAML(R.string.machine_config_view_yaml),
    META(R.string.machine_config_view_meta),
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MachineConfigScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    /** A drift difference from Insights: the screen opens a draft that undoes it, then the review. */
    fix: DriftFix? = null,
    /** A snippet from another screen: the screen opens a draft and the picker on it, prefilled. */
    snippet: SnippetPrefill? = null,
    /** A repository's Talos patch file: the screen opens a draft with it applied, then the review. */
    patch: PatchPrefill? = null,
    /** A file of the cluster repository, at a line (the Git comparison's files). */
    onRepoFile: (path: String, line: Int) -> Unit = { _, _ -> },
    /** The editor of a file of the cluster repository, a quick edit applied (the write-back's review). */
    onRepoEdit: (path: String, quick: String) -> Unit = { _, _ -> },
    vm: MachineConfigViewModel = viewModel(
        key = "machineconfig-$node",
        factory = factory { MachineConfigViewModel(app.talosRepository, app.machineConfigRepository, node, hostname, app.configTryManager, app.configMultiManager) },
    ),
    metaVm: MetaKeysViewModel = viewModel(key = "meta-$node", factory = factory { MetaKeysViewModel(app.talosRepository, node) }),
) {
    AllowLandscape()
    val state by vm.state.collectAsStateWithLifecycle()
    val revealed by vm.revealed.collectAsStateWithLifecycle()
    val editor by vm.editor.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var fixApplied by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(state is UiState.Loaded) {
        if (fix != null && !fixApplied && state is UiState.Loaded) {
            fixApplied = true
            vm.applyDriftFix(fix)
        }
    }
    var patchApplied by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(state is UiState.Loaded) {
        if (patch != null && !patchApplied && state is UiState.Loaded) {
            patchApplied = true
            vm.applyPatch(patch)
        }
    }
    val snippets by vm.snippets.collectAsStateWithLifecycle()
    var pickingSnippet by rememberSaveable { mutableStateOf(false) }
    // The prefill holds for the first opening of the picker only.
    var prefillUsed by rememberSaveable { mutableStateOf(false) }
    var snippetOpened by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(state is UiState.Loaded) {
        if (snippet != null && !snippetOpened && state is UiState.Loaded) {
            snippetOpened = true
            if (!editor.editing) vm.startEditing()
            vm.loadSnippets()
            pickingSnippet = true
        }
    }

    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var query by rememberSaveable { mutableStateOf("") }
    var view by rememberSaveable { mutableStateOf(ConfigView.FIELDS) }
    var confirmingDiscard by rememberSaveable { mutableStateOf(false) }
    // Kept here so the tree stays as it was opened across the review, the YAML view and a rotation.
    var expanded by rememberSaveable(stateSaver = listSaver(save = { it.toList() }, restore = { it.toSet() })) { mutableStateOf(emptySet<String>()) }
    var confirmingTry by rememberSaveable { mutableStateOf<Int?>(null) }
    var confirmingApply by rememberSaveable { mutableStateOf<ConfigApplyMode?>(null) }
    var confirmingMulti by rememberSaveable { mutableStateOf<ConfigApplyMode?>(null) }
    var comparingGit by rememberSaveable { mutableStateOf(false) }
    // The change written back to the repository: the config it starts from, the one it gives, and the node's file it goes to.
    var proposing by remember { mutableStateOf<ProposeTarget?>(null) }
    val hasRepo = remember { app.gitOpsRepository.activeClusterRepo() != null }

    SecureWhile(revealed)

    LaunchedEffect(vm) {
        // The latest refusal replaces the one on screen instead of waiting behind it.
        vm.messages.collectLatest { snackbar.showSnackbar(it.resolve(context), withDismissAction = true, duration = SnackbarDuration.Long) }
    }

    // With the app lock on, revealing secrets or changing the node needs a fresh fingerprint/PIN.
    fun authenticated(@StringRes title: Int, action: () -> Unit) {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            action()
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(title, hostname))) {
                AuthResult.Success -> action()
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    fun toggleReveal(on: Boolean) {
        if (on) authenticated(R.string.machine_config_auth) { vm.reveal(true) } else vm.reveal(false)
    }

    val run = editor.run
    val review = editor.review
    val applying = editor.apply

    val multi = editor.multi

    fun back() {
        when {
            multi?.run?.finished == false -> onBack() // a multi-node run goes on in its notification
            multi?.run != null -> vm.finishMulti()
            multi != null -> vm.closeMulti()
            run is ConfigTryState.Running -> onBack() // the try goes on in its notification
            run != null -> vm.finishTry()
            applying is ConfigApplyState.Running -> Unit // so is an apply
            applying != null -> vm.finishApply()
            review != null -> vm.closeReview()
            editor.dirty -> confirmingDiscard = true
            editor.editing -> vm.discard()
            else -> onBack()
        }
    }
    BackHandler(enabled = editor.editing) { back() }

    val loaded = (state as? UiState.Loaded)?.data
    val shown = remember(loaded, query) { loaded?.let { matchingLines(it, query) } }
    val browsing = run == null && applying == null && review == null && multi == null
    val ownRole = remember(loaded) { loaded?.let(::configRole).orEmpty() }
    val proposeEdit = editor.draft?.takeIf { hasRepo }?.let { draft -> { proposing = ProposeTarget(editor.base, draft, hostname, ownRole) } }
    // After a multi-node run: the same edits (this node's base and draft) into each node's own file.
    val proposeForNode = editor.draft?.takeIf { hasRepo }?.let { draft ->
        { n: MultiConfigNodeState ->
            val role = (multi?.candidates as? UiState.Loaded)?.data?.firstOrNull { it.node == n.node }?.role
            proposing = ProposeTarget(editor.base, draft, n.hostname, role ?: ownRole)
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(
                            stringResource(
                                when {
                                    multi?.run != null -> multi.run.mode.label
                                    multi?.preview != null -> R.string.machine_config_multi_title
                                    run != null -> R.string.machine_config_try_title
                                    applying != null -> applying.mode.label
                                    review != null -> R.string.machine_config_review
                                    else -> R.string.machine_config_title
                                },
                            ),
                        )
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                        // The repository file the draft comes from, while it is reviewed and tried.
                        if (patch != null && (editor.editing || run != null || applying != null)) {
                            Text(
                                patch.from,
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                },
                navigationIcon = {
                    val running = applying is ConfigApplyState.Running
                    if (!running) BackButton(::back)
                },
                actions = {
                    if (browsing && !editor.editing && view != ConfigView.META) {
                        TooltipIconButton(
                            Icons.Outlined.ContentCopy,
                            stringResource(R.string.machine_config_copy),
                            enabled = shown != null,
                            onClick = {
                                shown?.let {
                                    copy(context, it.joinToString("\n"), sensitive = revealed)
                                    scope.launch { snackbar.showSnackbar(context.getString(R.string.machine_config_copied)) }
                                }
                            },
                        )
                        TooltipIconButton(Icons.Outlined.Edit, stringResource(R.string.machine_config_edit), enabled = loaded != null, onClick = vm::startEditing)
                        TooltipIconButton(Icons.Outlined.Commit, stringResource(R.string.talos_git_action), enabled = loaded != null, onClick = {
                            if (app.gitOpsRepository.activeClusterRepo() != null) {
                                comparingGit = true
                            } else {
                                scope.launch { snackbar.showSnackbar(context.getString(R.string.repo_none), withDismissAction = true) }
                            }
                        })
                    }
                    if (browsing && editor.editing) {
                        TooltipIconButton(Icons.Outlined.AutoAwesome, stringResource(R.string.machine_config_snippets), enabled = !editor.busy, onClick = {
                            vm.loadSnippets()
                            pickingSnippet = true
                        })
                    }
                },
            )
        },
        bottomBar = {
            if (browsing && editor.editing) {
                Row(
                    Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 16.dp, vertical = 8.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    OutlinedButton(onClick = ::back, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.machine_config_discard)) }
                    Button(onClick = vm::review, enabled = editor.canReview, modifier = Modifier.weight(1f)) {
                        Text(stringResource(R.string.machine_config_review))
                    }
                }
            }
        },
    ) { padding ->
        val body = Modifier.padding(padding).fillMaxSize()
        when {
            multi?.run != null -> MultiApplyContent(multi.run, onDone = vm::finishMulti, modifier = body, onProposeInGit = proposeForNode)
            multi?.preview != null -> MultiNodeReviewContent(
                multi.preview,
                onRetry = vm::previewMulti,
                onApply = { confirmingMulti = it },
                modifier = body,
            )
            run != null -> ConfigTryContent(run, onKeep = vm::keep, onRevert = vm::revertNow, onDone = vm::finishTry, modifier = body, onProposeInGit = proposeEdit)
            applying != null -> ConfigApplyContent(applying, onDone = vm::finishApply, modifier = body, onProposeInGit = proposeEdit)
            review != null -> ConfigReviewContent(
                review,
                onRetry = vm::review,
                onTry = { confirmingTry = it },
                onApply = { confirmingApply = it },
                modifier = body,
                onAlsoApply = vm::startMulti.takeIf { editor.canReplay },
            )
            else -> Column(body) {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (view == ConfigView.META) {
                        // The META keys are not the config: nothing to reveal or search.
                    } else if (editor.editing) {
                        InfoNotice(stringResource(R.string.machine_config_editing_note))
                    } else {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(stringResource(R.string.machine_config_reveal), style = MaterialTheme.typography.titleSmall)
                                MutedText(stringResource(R.string.machine_config_reveal_desc))
                            }
                            Switch(checked = revealed, onCheckedChange = ::toggleReveal)
                        }
                        if (revealed) RevealedBanner()
                    }
                    ViewPicker(view, withMeta = !editor.editing) { view = it }
                    // The YAML editor is the text itself: there is nothing to filter while typing in it.
                    if (view != ConfigView.META && !(editor.editing && view == ConfigView.YAML)) {
                        OutlinedTextField(
                            value = query,
                            onValueChange = { query = it },
                            placeholder = { Text(stringResource(R.string.machine_config_search)) },
                            leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                            supportingText = shown?.takeIf { query.isNotBlank() && view == ConfigView.YAML }?.let {
                                { Text(pluralStringResource(R.plurals.machine_config_matches, it.size, it.size)) }
                            },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
                if (view == ConfigView.META) {
                    MetaKeysPane(
                        metaVm,
                        hostname,
                        authenticated = ::authenticated,
                        onMessage = { message -> scope.launch { snackbar.showSnackbar(message.resolve(context), withDismissAction = true) } },
                    )
                } else when (val s = state) {
                    UiState.Loading -> LoadingBox(style = SkeletonStyle.TEXT)
                    is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                    is UiState.Loaded -> {
                        if (s.refreshing || editor.busy) LinearProgressIndicator(Modifier.fillMaxWidth())
                        val tree = editor.tree
                        val draft = editor.draft
                        when {
                            view == ConfigView.YAML && draft != null -> ConfigYamlEditor(draft, tree?.error, vm::setDraft)
                            view == ConfigView.YAML -> YamlView(shown.orEmpty(), query)
                            tree == null || editor.treeStale -> LoadingBox()
                            tree.error != null -> SyntaxErrorCard(tree.error, Modifier.padding(16.dp))
                            else -> ConfigTreeView(
                                tree,
                                query,
                                editing = editor.editing,
                                enabled = !editor.busy,
                                expanded = expanded,
                                onExpanded = { expanded = it },
                                onEdit = vm::apply,
                            )
                        }
                    }
                }
            }
        }
    }

    if (comparingGit) {
        GitCompareSheet(
            app.gitOpsRepository,
            node = node,
            hostname = hostname,
            role = remember(loaded) { loaded?.let(::configRole).orEmpty() },
            onOpenFile = { path, line ->
                comparingGit = false
                onRepoFile(path, line)
            },
            onEditDraft = { draft ->
                comparingGit = false
                vm.editFrom(draft)
            },
            // The node's live values into Git: from Git's version to the node's.
            onProposeLive = { gitVersion ->
                comparingGit = false
                loaded?.let { proposing = ProposeTarget(gitVersion, it, hostname, ownRole) }
            },
            onDismiss = { comparingGit = false },
        )
    }

    proposing?.let { target ->
        ProposeInGitSheet(
            app.gitOpsRepository,
            base = target.base,
            draft = target.draft,
            hostname = target.hostname,
            role = target.role,
            onReview = { path, quick ->
                proposing = null
                onRepoEdit(path, quick)
            },
            onDismiss = { proposing = null },
        )
    }

    if (pickingSnippet) {
        SnippetPickerDialog(
            snippets = snippets,
            onRetry = vm::loadSnippets,
            onApply = { id, params ->
                pickingSnippet = false
                prefillUsed = true
                vm.applySnippet(id, params)
            },
            onDismiss = {
                pickingSnippet = false
                prefillUsed = true
            },
            prefill = snippet.takeUnless { prefillUsed },
        )
    }

    if (confirmingDiscard) {
        ConfirmDialog(
            title = stringResource(R.string.machine_config_discard_title),
            text = stringResource(R.string.machine_config_discard_text),
            confirm = stringResource(R.string.machine_config_discard),
            onConfirm = {
                confirmingDiscard = false
                vm.discard()
            },
            onDismiss = { confirmingDiscard = false },
            destructive = true,
        )
    }

    confirmingApply?.let { mode ->
        val start = {
            confirmingApply = null
            authenticated(R.string.machine_config_apply_auth) { vm.startApply(mode) }
        }
        if (mode == ConfigApplyMode.REBOOT) {
            // Never a surprise reboot: the hostname is typed, as for a reboot.
            HostnameConfirmDialog(
                title = stringResource(R.string.machine_config_apply_reboot_title, hostname),
                hostname = hostname,
                confirmLabel = stringResource(R.string.machine_config_apply_reboot),
                onConfirm = start,
                onDismiss = { confirmingApply = null },
                emphasized = true,
            ) {
                Text(stringResource(R.string.machine_config_apply_reboot_text), style = MaterialTheme.typography.bodyMedium)
            }
        } else {
            ConfirmDialog(
                title = stringResource(R.string.machine_config_apply_confirm_title, hostname),
                text = stringResource(
                    if (mode == ConfigApplyMode.STAGED) R.string.machine_config_apply_confirm_staged else R.string.machine_config_apply_confirm_auto,
                ),
                confirm = stringResource(mode.label),
                onConfirm = start,
                onDismiss = { confirmingApply = null },
                confirmColor = LocalStatusColors.current.warn,
            )
        }
    }

    if (multi != null && multi.picking) {
        MultiNodePickerDialog(
            candidates = multi.candidates,
            selected = multi.selected,
            onToggle = vm::toggleMultiNode,
            onRetry = vm::startMulti,
            onPreview = vm::previewMulti,
            onDismiss = vm::closeMulti,
        )
    }

    confirmingMulti?.let { mode ->
        val preview = (multi?.preview as? UiState.Loaded)?.data
        val names = preview?.changing.orEmpty().map { it.name }
        // One node: its hostname is typed; several: the cluster's name, once for them all.
        val typed = names.singleOrNull() ?: multi?.cluster?.ifEmpty { null } ?: hostname
        HostnameConfirmDialog(
            title = stringResource(R.string.machine_config_multi_confirm_title),
            hostname = typed,
            confirmLabel = stringResource(mode.label),
            onConfirm = {
                confirmingMulti = null
                authenticated(R.string.machine_config_apply_auth) { vm.startMultiApply(mode) }
            },
            onDismiss = { confirmingMulti = null },
            emphasized = mode == ConfigApplyMode.REBOOT,
        ) {
            Text(
                stringResource(
                    when (mode) {
                        ConfigApplyMode.REBOOT -> R.string.machine_config_multi_confirm_reboot
                        ConfigApplyMode.STAGED -> R.string.machine_config_apply_confirm_staged
                        ConfigApplyMode.AUTO -> R.string.machine_config_apply_confirm_auto
                    },
                ),
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(names.joinToString(", "), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        }
    }

    confirmingTry?.let { timeout ->
        ConfirmDialog(
            title = stringResource(R.string.machine_config_try_confirm_title, hostname),
            text = stringResource(R.string.machine_config_try_confirm_text, minutesText(timeout)),
            confirm = stringResource(R.string.machine_config_try),
            onConfirm = {
                confirmingTry = null
                authenticated(R.string.machine_config_try_auth) { vm.startTry(timeout) }
            },
            onDismiss = { confirmingTry = null },
            confirmColor = LocalStatusColors.current.warn,
        )
    }
}

@Composable
private fun ViewPicker(selected: ConfigView, withMeta: Boolean, onSelect: (ConfigView) -> Unit) {
    // While editing, META is out of the way: it is not part of the draft.
    val views = ConfigView.entries.filter { withMeta || it != ConfigView.META }
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        views.forEachIndexed { i, v ->
            SegmentedButton(
                selected = v == selected,
                onClick = { onSelect(v) },
                shape = SegmentedButtonDefaults.itemShape(i, views.size),
                icon = {},
            ) { Text(stringResource(v.label)) }
        }
    }
}

@Composable
private fun RevealedBanner() {
    val bad = LocalStatusColors.current.bad
    Card(colors = CardDefaults.cardColors(containerColor = bad.copy(alpha = 0.15f)), modifier = Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.machine_config_revealed_warning),
            color = bad,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(12.dp),
        )
    }
}

/** A change written back to the repository: from [base] to [draft], into the file of the node [hostname] of [role]. */
private data class ProposeTarget(val base: String, val draft: String, val hostname: String, val role: String)

private fun copy(context: Context, text: String, sensitive: Boolean) =
    copyToClipboard(context, context.getString(R.string.machine_config_title), text, sensitive)

