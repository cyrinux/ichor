package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
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
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.NodeTaintEdit
import name.levis.ichor.model.UNSCHEDULABLE_TAINT
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory

/** What the editor's dialog is open on: a taint or a label, new ([key] or [taint] null) or existing. */
private sealed interface MetaEditing {
    data class Taint(val taint: NodeTaintEdit?) : MetaEditing
    data class Label(val key: String?, val value: String) : MetaEditing
}

/**
 * The taints and labels of [node] (its Talos address, or its Kubernetes name on a kubeconfig
 * cluster; [hostname] is what the typed confirmation asks for). Rows are edited in a draft; Review
 * shows the dry-run plan, Apply confirms (typed hostname for a taint change, then the app lock).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeMetaScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: NodeMetaViewModel = viewModel(key = "node-meta-$node", factory = factory { NodeMetaViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val review by vm.review.collectAsStateWithLifecycle()
    val outcome by vm.outcome.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var editing by remember { mutableStateOf<MetaEditing?>(null) }
    var confirming by remember { mutableStateOf(false) }

    LaunchedEffect(outcome) {
        val done = outcome ?: return@LaunchedEffect
        vm.outcomeShown()
        val text = done.message?.resolve(context) ?: context.getString(R.string.node_meta_applied, hostname)
        // In the screen's scope: consuming the outcome restarts this effect, which would cancel it.
        scope.launch { snackbar.showSnackbar(text, withDismissAction = true, duration = SnackbarDuration.Long) }
    }

    // With the app lock on, a taint change needs a fresh fingerprint/PIN, like the power actions.
    fun applyConfirmed(taints: Boolean) {
        confirming = false
        val app = context.applicationContext as TalosApp
        val activity = context.findFragmentActivity()
        if (!taints || !app.appLock.enabled.value || activity == null) {
            vm.apply()
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.node_meta_confirm_taints, hostname))) {
                AuthResult.Success -> vm.apply()
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.node_meta_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if ((state as? UiState.Loaded)?.refreshing == true) {
                        CircularProgressIndicator(Modifier.size(20.dp).padding(end = 4.dp), strokeWidth = 2.dp)
                    }
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.node_meta_reload), onClick = vm::load)
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, onRetry = vm::load, modifier = Modifier.pageContent(padding))
            is UiState.Loaded -> NodeMetaEditor(
                draft = s.data,
                planning = review is NodeMetaReview.Planning,
                modifier = Modifier.pageContent(padding).fillMaxSize(),
                onShowProtected = vm::showProtected,
                onEdit = { editing = it },
                onRemoveTaint = vm::removeTaint,
                onRemoveLabel = vm::removeLabel,
                onReview = vm::review,
            )
        }
    }

    when (val e = editing) {
        is MetaEditing.Taint -> TaintEditDialog(
            initial = e.taint,
            onSave = {
                vm.putTaint(e.taint, it)
                editing = null
            },
            onDismiss = { editing = null },
        )
        is MetaEditing.Label -> LabelEditDialog(
            initialKey = e.key,
            initialValue = e.value,
            onSave = { key, value ->
                vm.putLabel(e.key, key, value)
                editing = null
            },
            onDismiss = { editing = null },
        )
        null -> Unit
    }

    when (val r = review) {
        is NodeMetaReview.Ready -> NodeMetaPlanSheet(
            plan = r.plan,
            hostname = hostname,
            applying = r.applying,
            onApply = { confirming = true },
            onReload = {
                vm.dismissReview()
                vm.load()
            },
            onDismiss = vm::dismissReview,
        )
        is NodeMetaReview.Failed -> LaunchedEffect(r) {
            val text = r.message.resolve(context)
            vm.dismissReview()
            // Dismissing leaves this branch and cancels the effect: the snackbar runs in the screen's scope.
            scope.launch { snackbar.showSnackbar(text, withDismissAction = true, duration = SnackbarDuration.Long) }
        }
        else -> Unit
    }

    val ready = review as? NodeMetaReview.Ready
    if (confirming && ready != null) {
        val plan = ready.plan
        if (plan.hasTaintChanges) {
            HostnameConfirmDialog(
                title = stringResource(R.string.node_meta_confirm_taints, hostname),
                hostname = hostname,
                confirmLabel = stringResource(R.string.node_meta_apply),
                onConfirm = { applyConfirmed(taints = true) },
                onDismiss = { confirming = false },
                emphasized = plan.evicts.isNotEmpty(),
                enabled = plan.canApply,
            ) {
                if (plan.evicts.isNotEmpty()) {
                    Text(stringResource(R.string.node_meta_evicts), style = MaterialTheme.typography.bodyMedium)
                    plan.evicts.forEach { Text("${it.namespace}/${it.name}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
                }
            }
        } else {
            ConfirmDialog(
                title = stringResource(R.string.node_meta_confirm_labels, hostname),
                text = plan.summaryLines(),
                confirm = stringResource(R.string.node_meta_apply),
                onConfirm = { applyConfirmed(taints = false) },
                onDismiss = { confirming = false },
            )
        }
    }
}

@Composable
private fun NodeMetaEditor(
    draft: NodeMetaDraft,
    planning: Boolean,
    modifier: Modifier,
    onShowProtected: (Boolean) -> Unit,
    onEdit: (MetaEditing) -> Unit,
    onRemoveTaint: (NodeTaintEdit) -> Unit,
    onRemoveLabel: (String) -> Unit,
    onReview: () -> Unit,
) {
    Column(modifier) {
        LazyColumn(
            Modifier.weight(1f).fillMaxWidth(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            item(key = "protected") {
                ToggleRow(
                    title = stringResource(R.string.node_meta_show_protected),
                    checked = draft.showProtected,
                    onChange = onShowProtected,
                    description = stringResource(R.string.node_meta_protected_hint),
                )
            }
            taintRows(draft, onEdit, onRemoveTaint)
            labelRows(draft, onEdit, onRemoveLabel)
        }
        Button(
            onClick = onReview,
            enabled = draft.changed && !planning,
            modifier = Modifier.fillMaxWidth().padding(16.dp),
        ) {
            if (planning) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            else Text(stringResource(R.string.node_meta_review))
        }
    }
}

private fun LazyListScope.taintRows(draft: NodeMetaDraft, onEdit: (MetaEditing) -> Unit, onRemove: (NodeTaintEdit) -> Unit) {
    item(key = "taints") { SectionTitle(stringResource(R.string.node_meta_taints)) }
    val taints = draft.taints.sortedWith(compareBy({ it.key }, { it.effect }))
    if (taints.isEmpty()) item(key = "no-taints") { EmptyText(stringResource(R.string.node_meta_no_taints)) }
    items(taints, key = { "taint-${it.key}-${it.effect}" }) { taint ->
        // The cordon's taint belongs to Cordon / Uncordon, even with protected ones shown.
        val locked = taint.key == UNSCHEDULABLE_TAINT || (draft.original.taintProtected(taint.key) && !draft.showProtected)
        MetaRow(
            text = if (taint.value.isEmpty()) taint.key else "${taint.key}=${taint.value}",
            effect = taint.effect,
            locked = locked,
            onEdit = { onEdit(MetaEditing.Taint(taint)) },
            onRemove = { onRemove(taint) },
        )
    }
    item(key = "add-taint") {
        AddRow(stringResource(R.string.node_meta_add_taint)) { onEdit(MetaEditing.Taint(null)) }
    }
}

private fun LazyListScope.labelRows(draft: NodeMetaDraft, onEdit: (MetaEditing) -> Unit, onRemove: (String) -> Unit) {
    item(key = "labels") { SectionTitle(stringResource(R.string.node_meta_labels)) }
    val labels = draft.labels.entries.sortedBy { it.key }.map { it.key to it.value }
    if (labels.isEmpty()) item(key = "no-labels") { EmptyText(stringResource(R.string.node_meta_no_labels)) }
    items(labels, key = { "label-${it.first}" }) { (key, value) ->
        MetaRow(
            text = "$key=$value",
            effect = null,
            locked = draft.original.labelProtected(key) && !draft.showProtected,
            onEdit = { onEdit(MetaEditing.Label(key, value)) },
            onRemove = { onRemove(key) },
        )
    }
    item(key = "add-label") {
        AddRow(stringResource(R.string.node_meta_add_label)) { onEdit(MetaEditing.Label(null, "")) }
    }
}

/** A taint or label: its text, the taint's [effect], then Edit and Remove, or a lock when [locked]. */
@Composable
private fun MetaRow(text: String, effect: String?, locked: Boolean, onEdit: () -> Unit, onRemove: () -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Column(Modifier.weight(1f).padding(vertical = 6.dp)) {
            Text(
                text,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
            )
            // Kubernetes' own effect names, never translated.
            if (effect != null) Tag(effect, mono = true)
        }
        if (locked) {
            Icon(
                Icons.Outlined.Lock,
                contentDescription = stringResource(R.string.node_meta_protected_hint),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(12.dp).size(20.dp),
            )
        } else {
            TooltipIconButton(Icons.Outlined.Edit, stringResource(R.string.node_meta_edit), onClick = onEdit)
            TooltipIconButton(Icons.Outlined.Delete, stringResource(R.string.node_meta_remove), onClick = onRemove)
        }
    }
}

@Composable
private fun AddRow(label: String, onClick: () -> Unit) {
    TextButton(onClick = onClick) {
        Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
        Text(label, Modifier.padding(start = 8.dp))
    }
}
