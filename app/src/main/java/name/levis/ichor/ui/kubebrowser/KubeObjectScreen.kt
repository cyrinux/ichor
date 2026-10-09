package name.levis.ichor.ui.kubebrowser

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeObjectAction
import name.levis.ichor.model.KubeObjectBar
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubePermission
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.SecureWhile
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ActionBarActions
import name.levis.ichor.ui.components.ActionBarEditor
import name.levis.ichor.ui.components.AppTab
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoBox
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.diff.DiffLines
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.machineconfig.ConfigYamlEditor

/**
 * One object of any kind ([ref]): first its summary (health, conditions, owners up the chain
 * through [onOwner] or [onHelmRelease], events, metadata), then its YAML, with copy and share,
 * a Secret's values behind the app lock, and, when the kind can be updated, an editor whose
 * change is reviewed as a diff (the API server's dry run) before it is saved. A pod also opens
 * a port-forward ([onPortForward]). Its top bar is arranged like the overview's: the actions
 * shown as icons or kept in its menu are the user's choice.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeObjectScreen(
    ref: KubeObjectRef,
    onBack: () -> Unit,
    onPortForward: (() -> Unit)?,
    onOwner: (KubeObjectRef) -> Unit,
    onHelmRelease: (namespace: String, name: String) -> Unit,
    vm: KubeObjectViewModel = viewModel(
        key = "kube-object-${ref.group}/${ref.resource}/${ref.namespace}/${ref.name}",
        factory = factory { KubeObjectViewModel(app.kubeBrowser, ref) },
    ),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val state by vm.yaml.collectAsStateWithLifecycle()
    val summary by vm.summary.collectAsStateWithLifecycle()
    val revealed by vm.revealed.collectAsStateWithLifecycle()
    val edit by vm.edit.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) {
        if (state == UiState.Loading) vm.refresh()
        if (summary == UiState.Loading) vm.refreshSummary()
    }
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var confirmingDiscard by remember { mutableStateOf(false) }
    val yaml = (state as? UiState.Loaded)?.data

    SecureWhile(revealed)
    LaunchedEffect(vm) {
        vm.messages.collectLatest { snackbar.showSnackbar(it.resolve(context), withDismissAction = true, duration = SnackbarDuration.Long) }
    }

    // With the app lock on, showing a Secret's values needs a fresh fingerprint/PIN.
    fun toggleReveal(on: Boolean) {
        val activity = context.findFragmentActivity()
        if (!on || !app.appLock.enabled.value || activity == null) return vm.reveal(on)
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.kb_reveal_auth, ref.name))) {
                AuthResult.Success -> vm.reveal(true)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    val bar by app.uiPreferences.kubeObjectBar.collectAsStateWithLifecycle()
    var customizing by rememberSaveable { mutableStateOf(false) }
    BackHandler(enabled = customizing) { customizing = false }

    val current = edit
    BackHandler(enabled = current != null) {
        when {
            current?.review != null -> vm.backToEditor()
            current?.changed == true -> confirmingDiscard = true
            else -> vm.cancelEdit()
        }
    }
    if (confirmingDiscard) {
        ConfirmDialog(
            title = stringResource(R.string.kb_discard_title),
            text = stringResource(R.string.kb_discard_text),
            confirm = stringResource(R.string.kb_discard_confirm),
            onConfirm = {
                confirmingDiscard = false
                vm.cancelEdit()
            },
            onDismiss = { confirmingDiscard = false },
            destructive = true,
        )
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            if (customizing) TopAppBar(
                title = { Text(stringResource(R.string.bar_edit_title)) },
                actions = { TextButton(onClick = { customizing = false }) { Text(stringResource(R.string.overview_edit_done)) } },
            ) else TopAppBar(
                title = {
                    Column {
                        Text(ref.name, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            listOf(ref.kind, ref.namespace).filter { it.isNotEmpty() }.joinToString("  ·  "),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                        )
                    }
                },
                navigationIcon = { BackButton(if (current == null) onBack else ({ if (current.changed) confirmingDiscard = true else vm.cancelEdit() })) },
                actions = {
                    when {
                        current == null -> ViewActions(
                            ref,
                            yaml,
                            revealed,
                            bar,
                            onPortForward,
                            onEdit = vm::startEdit,
                            onRefresh = vm::refreshAll,
                            onCustomize = { customizing = true },
                        )
                        current.review == null -> TextButton(onClick = vm::review) { Text(stringResource(R.string.kb_review)) }
                    }
                },
            )
        },
    ) { padding ->
        if (customizing) {
            ActionBarEditor(bar, kubeObjectActionLook, app.uiPreferences::setKubeObjectBar, Modifier.padding(padding))
            return@Scaffold
        }
        // Asked while editing: an update the credentials cannot run cannot be saved.
        val saveDenial = if (current != null) rememberKubeCanDenial("update", ref.group, ref.resource, ref.namespace, ref.name) else null
        Column(Modifier.padding(padding).fillMaxSize()) {
            when {
                current?.review != null -> ReviewContent(current, saveDenial, onBack = vm::backToEditor, onSave = vm::save, onRetry = vm::review)
                current != null -> {
                    MutedText(stringResource(R.string.kb_edit_hint), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                    KubeDenialNote(saveDenial, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                    ConfigYamlEditor(current.draft, null, vm::changeDraft)
                }
                else -> {
                    PrimaryTabRow(selectedTabIndex = tab) {
                        AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.kb_tab_summary)) })
                        AppTab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.kb_tab_yaml)) })
                    }
                    if (tab == 0) {
                        ObjectSummaryContent(
                            summary,
                            onRetry = vm::refreshSummary,
                            onOwner = { owner ->
                                val target = owner.toRef()
                                when {
                                    owner.isHelmRelease -> onHelmRelease(owner.namespace, owner.name)
                                    target != null -> onOwner(target)
                                }
                            },
                            modifier = Modifier.weight(1f),
                        )
                    } else {
                        if (ref.isSecret) SecretReveal(revealed, ::toggleReveal)
                        when (val s = state) {
                            UiState.Loading -> LoadingBox(style = SkeletonStyle.TEXT)
                            is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                            is UiState.Loaded -> YamlLines(s.data, Modifier.weight(1f))
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ViewActions(
    ref: KubeObjectRef,
    yaml: String?,
    revealed: Boolean,
    bar: KubeObjectBar,
    onPortForward: (() -> Unit)?,
    onEdit: () -> Unit,
    onRefresh: () -> Unit,
    onCustomize: () -> Unit,
) {
    val context = LocalContext.current
    ActionBarActions(
        bar = bar,
        look = kubeObjectActionLook,
        onClick = { action ->
            when (action) {
                KubeObjectAction.EDIT -> onEdit()
                KubeObjectAction.REFRESH -> onRefresh()
                KubeObjectAction.COPY -> copyWithToast(context, ref.name, yaml.orEmpty(), sensitive = ref.isSecret && revealed)
                KubeObjectAction.SHARE -> shareText(context, yaml.orEmpty(), context.getString(R.string.kb_share))
                KubeObjectAction.PORT_FORWARD -> onPortForward?.invoke()
            }
        },
        customizeLabel = stringResource(R.string.bar_edit_title),
        onCustomize = onCustomize,
        offered = { action ->
            when (action) {
                KubeObjectAction.EDIT -> ref.editable
                KubeObjectAction.PORT_FORWARD -> onPortForward != null && ref.isPod
                else -> true
            }
        },
        // Nothing to copy, share or edit before the YAML is read.
        enabled = { action -> action == KubeObjectAction.REFRESH || action == KubeObjectAction.PORT_FORWARD || yaml != null },
    )
}

/** The switch showing a Secret's values, and the warning while they are shown. */
@Composable
private fun SecretReveal(revealed: Boolean, onToggle: (Boolean) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        ToggleRow(stringResource(R.string.kb_reveal), revealed, onToggle, stringResource(R.string.kb_reveal_desc))
        if (revealed) ErrorCard(stringResource(R.string.kb_revealed_warning))
    }
}

/** What saving would change, from the API server's dry run, and the save itself (disabled when [saveDenial] refuses it). */
@Composable
private fun ReviewContent(edit: ObjectEdit, saveDenial: KubePermission?, onBack: () -> Unit, onSave: () -> Unit, onRetry: () -> Unit) {
    when (val review = edit.review) {
        null, UiState.Loading -> Column(Modifier.fillMaxSize(), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
            CircularProgressIndicator()
            MutedText(stringResource(R.string.kb_review_loading), Modifier.padding(top = 12.dp))
        }
        is UiState.Failed -> Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            ErrorCard(review.message.asString())
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = onBack) { Text(stringResource(R.string.kb_back_to_editor)) }
                TextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
        }
        is UiState.Loaded -> {
            if (!review.data.changed) {
                Column(Modifier.fillMaxSize()) {
                    InfoBox(stringResource(R.string.kb_no_changes), Modifier.weight(1f))
                    OutlinedButton(onClick = onBack, modifier = Modifier.padding(16.dp).navigationBarsPadding()) { Text(stringResource(R.string.kb_back_to_editor)) }
                }
                return
            }
            Column(Modifier.fillMaxSize().imePadding()) {
                Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(16.dp)) {
                    DiffLines(review.data.lines)
                }
                Column(Modifier.padding(16.dp).navigationBarsPadding(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    edit.saveError?.let { ErrorCard(it.asString()) }
                    KubeDenialNote(saveDenial)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        OutlinedButton(onClick = onBack, enabled = !edit.saving) { Text(stringResource(R.string.kb_back_to_editor)) }
                        Button(onClick = onSave, enabled = !edit.saving && saveDenial == null) { Text(stringResource(R.string.kb_save)) }
                        if (edit.saving) CircularProgressIndicator(Modifier.padding(start = 8.dp))
                    }
                }
            }
        }
    }
}
