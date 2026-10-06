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
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
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
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.SecureWhile
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/** The two ways the config is shown: field by field, or as its YAML text. */
private enum class ConfigView(@StringRes val label: Int) {
    FIELDS(R.string.machine_config_view_fields),
    YAML(R.string.machine_config_view_yaml),
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MachineConfigScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: MachineConfigViewModel = viewModel(key = "machineconfig-$node", factory = factory { MachineConfigViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val revealed by vm.revealed.collectAsStateWithLifecycle()
    val editor by vm.editor.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var query by rememberSaveable { mutableStateOf("") }
    var view by rememberSaveable { mutableStateOf(ConfigView.FIELDS) }
    var confirmingDiscard by rememberSaveable { mutableStateOf(false) }
    var confirmingTry by rememberSaveable { mutableStateOf<Int?>(null) }

    SecureWhile(revealed)

    LaunchedEffect(vm) {
        vm.messages.collect { snackbar.showSnackbar(it.resolve(context), withDismissAction = true, duration = SnackbarDuration.Long) }
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

    fun back() {
        when {
            run is ConfigTryState.Running -> Unit // the try is followed to its end
            run != null -> vm.finishTry()
            review != null -> vm.closeReview()
            editor.dirty -> confirmingDiscard = true
            editor.editing -> vm.discard()
            else -> onBack()
        }
    }
    BackHandler(enabled = editor.editing) { back() }

    val loaded = (state as? UiState.Loaded)?.data
    val shown = remember(loaded, query) { loaded?.let { matchingLines(it, query) } }
    val browsing = run == null && review == null

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(
                            stringResource(
                                when {
                                    run != null -> R.string.machine_config_try_title
                                    review != null -> R.string.machine_config_review
                                    else -> R.string.machine_config_title
                                },
                            ),
                        )
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { if (run !is ConfigTryState.Running) BackButton(::back) },
                actions = {
                    if (browsing && !editor.editing) {
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
            run != null -> ConfigTryContent(run, onKeep = vm::keep, onRevert = vm::revertNow, onDone = vm::finishTry, modifier = body)
            review != null -> ConfigReviewContent(review, onRetry = vm::review, onTry = { confirmingTry = it }, modifier = body)
            else -> Column(body) {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (editor.editing) {
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
                    ViewPicker(view) { view = it }
                    // The YAML editor is the text itself: there is nothing to filter while typing in it.
                    if (!(editor.editing && view == ConfigView.YAML)) {
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
                when (val s = state) {
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                    is UiState.Loaded -> {
                        if (s.refreshing || editor.busy) LinearProgressIndicator(Modifier.fillMaxWidth())
                        val tree = editor.tree
                        val draft = editor.draft
                        when {
                            view == ConfigView.YAML && draft != null -> ConfigYamlEditor(draft, tree?.error, vm::setDraft)
                            view == ConfigView.YAML -> YamlView(shown.orEmpty(), query)
                            tree == null -> LoadingBox()
                            tree.error != null -> SyntaxErrorCard(tree.error, Modifier.padding(16.dp))
                            else -> ConfigTreeView(tree, query, editing = editor.editing, enabled = !editor.busy, onEdit = vm::apply)
                        }
                    }
                }
            }
        }
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
private fun ViewPicker(selected: ConfigView, onSelect: (ConfigView) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        ConfigView.entries.forEachIndexed { i, v ->
            SegmentedButton(
                selected = v == selected,
                onClick = { onSelect(v) },
                shape = SegmentedButtonDefaults.itemShape(i, ConfigView.entries.size),
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

private fun copy(context: Context, text: String, sensitive: Boolean) =
    copyToClipboard(context, context.getString(R.string.machine_config_title), text, sensitive)
