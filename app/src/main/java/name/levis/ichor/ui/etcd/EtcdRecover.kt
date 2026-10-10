package name.levis.ichor.ui.etcd

import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.text.KeyboardOptions
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.EtcdRecoverEvent
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.EtcdRecoverPhase
import name.levis.ichor.model.EtcdRecoverProgress
import name.levis.ichor.model.SnapshotInfo
import name.levis.ichor.model.SnapshotOpener
import name.levis.ichor.model.etcdRecoverTimeline
import name.levis.ichor.model.nodeHostnames
import name.levis.ichor.model.recoverCandidates
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.upgrade.TimelineRow
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatBytes
import java.io.File
import java.util.UUID

/** Talos' disaster recovery guide, linked from the confirmation. */
private const val DISASTER_RECOVERY_URL = "https://www.talos.dev/latest/advanced/disaster-recovery/"

/** A snapshot picked for a recovery: copied into the app cache (Go needs a path), as picked. */
data class PickedSnapshot(val path: String, val name: String, val info: SnapshotInfo)

data class EtcdRecoverState(
    /** The picked file is being copied and inspected. */
    val loading: Boolean = false,
    val picked: PickedSnapshot? = null,
    val running: Boolean = false,
    val events: List<EtcdRecoverProgress> = emptyList(),
    val finished: Boolean = false,
    val error: String? = null,
) {
    val idle: Boolean get() = !loading && picked == null && !running && !finished
}

/**
 * etcd recovery from a snapshot file. The picked file is copied as it is into the app cache
 * (an encrypted one stays encrypted: Go decrypts it while uploading) and deleted afterwards.
 */
class EtcdRecoverViewModel(private val talos: TalosRepository, private val app: TalosApp) : ViewModel() {
    private val _state = MutableStateFlow(EtcdRecoverState())
    val state: StateFlow<EtcdRecoverState> = _state.asStateFlow()
    private var job: Job? = null
    private val dir get() = File(app.cacheDir, RECOVER_DIR)

    /** Copies the document at [uri] into the cache and reads what it needs. */
    fun pick(uri: Uri) {
        if (!_state.value.idle) return
        _state.value = EtcdRecoverState(loading = true)
        viewModelScope.launch {
            val file = File(dir, "${UUID.randomUUID()}.snapshot")
            try {
                val name = withContext(Dispatchers.IO) {
                    dir.deleteRecursively()
                    dir.mkdirs()
                    val resolver = app.contentResolver
                    resolver.openInputStream(uri)?.use { input -> file.outputStream().use { input.copyTo(it) } }
                        ?: error(app.getString(R.string.etcd_recover_open_failed))
                    displayName(uri)
                }
                val info = talos.snapshotInspect(file.path)
                _state.value = EtcdRecoverState(picked = PickedSnapshot(file.path, name, info))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                withContext(NonCancellable + Dispatchers.IO) { file.delete() }
                _state.value = EtcdRecoverState(finished = true, error = e.uiText().resolve(app))
            }
        }
    }

    private fun displayName(uri: Uri): String =
        app.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
            if (c.moveToFirst()) c.getString(0) else null
        } ?: uri.lastPathSegment.orEmpty()

    /** Recovers on [node]; [identity] and [passphrase] are passed to Go and never kept. */
    fun start(node: String, identity: String, passphrase: String, skipHashCheck: Boolean) {
        val picked = _state.value.picked ?: return
        if (_state.value.running) return
        _state.update { it.copy(running = true) }
        job = viewModelScope.launch {
            try {
                talos.etcdRecover(node, picked.path, identity, passphrase, skipHashCheck).collect { event ->
                    when (event) {
                        is EtcdRecoverEvent.Progress -> _state.update { it.copy(events = it.events + event.progress) }
                        is EtcdRecoverEvent.Done -> _state.update { it.copy(error = event.error) }
                    }
                }
            } catch (e: CancellationException) {
                _state.update { it.copy(error = it.error ?: app.getString(R.string.etcd_recover_stopped)) }
                throw e
            } catch (e: Throwable) {
                _state.update { it.copy(error = it.error ?: e.uiText().resolve(app)) }
            } finally {
                _state.update { it.copy(running = false, finished = true) }
                withContext(NonCancellable + Dispatchers.IO) { File(picked.path).delete() }
            }
        }
    }

    fun cancel() {
        job?.cancel()
    }

    /** Forgets a picked file or a finished run, and deletes the cache copy. */
    fun dismiss() {
        if (_state.value.running) return
        _state.value = EtcdRecoverState()
        viewModelScope.launch(NonCancellable + Dispatchers.IO) { dir.deleteRecursively() }
    }

    override fun onCleared() {
        if (!_state.value.running) dir.deleteRecursively()
    }

    private companion object {
        const val RECOVER_DIR = "etcd-recover"
    }
}

/** What the recovery dialog chose, kept in memory only until the run starts. */
private data class RecoverChoice(val node: String, val hostname: String, val identity: String, val passphrase: String, val skipHashCheck: Boolean)

/**
 * The "Recover from snapshot…" flow: the file picker, then what opens the file and the node,
 * then the confirmation (typed cluster name and the acknowledgement), the app lock, the run.
 * Returns the function starting it; call it at screen level.
 */
@Composable
fun rememberEtcdRecoverFlow(vm: EtcdRecoverViewModel, clusterName: String): (EtcdOverview) -> Unit {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    val state by vm.state.collectAsStateWithLifecycle()
    var etcd by remember { mutableStateOf<EtcdOverview?>(null) }
    var choice by remember { mutableStateOf<RecoverChoice?>(null) }

    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri != null) vm.pick(uri) else etcd = null
    }

    fun authenticated(action: () -> Unit) {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            action()
            return
        }
        scope.launch {
            if (authenticate(activity, context.getString(R.string.etcd_recover_auth)) is AuthResult.Success) action()
        }
    }

    val picked = state.picked
    val overview = etcd
    if (picked != null && overview != null && choice == null && !state.running) {
        EtcdRecoverDialog(
            picked = picked,
            nodes = overview.recoverCandidates,
            hostnames = overview.nodeHostnames(),
            onNext = { choice = it },
            onDismiss = {
                etcd = null
                vm.dismiss()
            },
        )
    }
    choice?.let { c ->
        var understood by remember(c) { mutableStateOf(false) }
        HostnameConfirmDialog(
            title = stringResource(R.string.etcd_recover_confirm_title, c.hostname),
            hostname = clusterName,
            confirmLabel = stringResource(R.string.etcd_recover_confirm),
            onConfirm = {
                choice = null
                etcd = null
                authenticated { vm.start(c.node, c.identity, c.passphrase, c.skipHashCheck) }
            },
            onDismiss = {
                choice = null
                etcd = null
                vm.dismiss()
            },
            emphasized = true,
            enabled = understood,
        ) {
            Text(stringResource(R.string.etcd_recover_confirm_body, c.hostname), style = MaterialTheme.typography.bodyMedium)
            RecoverAcknowledgment(understood) { understood = it }
        }
    }

    return { overview ->
        etcd = overview
        picker.launch(arrayOf("*/*"))
    }
}

@Composable
private fun RecoverAcknowledgment(checked: Boolean, onChange: (Boolean) -> Unit) {
    val uri = LocalUriHandler.current
    Row(
        Modifier.fillMaxWidth().toggleable(value = checked, role = Role.Checkbox, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = null)
        Text(stringResource(R.string.etcd_recover_ack), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(start = 8.dp))
    }
    TextButton(onClick = { uri.openUri(DISASTER_RECOVERY_URL) }) { Text(stringResource(R.string.etcd_recover_docs)) }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun EtcdRecoverDialog(
    picked: PickedSnapshot,
    nodes: List<String>,
    hostnames: Map<String, String>,
    onNext: (RecoverChoice) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    val opener = picked.info.opener
    var secret by remember { mutableStateOf("") }
    var node by remember { mutableStateOf(nodes.singleOrNull()) }
    var skipHash by remember { mutableStateOf(false) }
    val ready = node != null && opener != SnapshotOpener.UNSUPPORTED && (opener == SnapshotOpener.NONE || secret.isNotBlank())

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.etcd_recover_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("${picked.name} · ${formatBytes(picked.info.size)}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                when (opener) {
                    SnapshotOpener.NONE -> Text(stringResource(R.string.etcd_recover_clear), style = MaterialTheme.typography.bodyMedium)
                    SnapshotOpener.PASSPHRASE -> OutlinedTextField(
                        value = secret,
                        onValueChange = { secret = it },
                        label = { Text(stringResource(R.string.etcd_recover_passphrase)) },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SnapshotOpener.SECRET_KEY -> {
                        Text(stringResource(R.string.etcd_recover_needs_key), style = MaterialTheme.typography.bodyMedium)
                        OutlinedTextField(
                            value = secret,
                            onValueChange = { secret = it },
                            label = { Text(stringResource(R.string.etcd_recover_secret_key)) },
                            visualTransformation = PasswordVisualTransformation(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    SnapshotOpener.UNSUPPORTED -> Text(stringResource(R.string.etcd_recover_unsupported), color = colors.bad, style = MaterialTheme.typography.bodyMedium)
                }
                Text(stringResource(R.string.etcd_recover_node), style = MaterialTheme.typography.labelMedium)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    nodes.forEach { n ->
                        FilterChip(selected = node == n, onClick = { node = n }, label = { Text(hostnames[n] ?: n) })
                    }
                }
                if (opener == SnapshotOpener.NONE) {
                    ToggleRow(
                        title = stringResource(R.string.etcd_recover_skip_hash),
                        description = stringResource(R.string.etcd_recover_skip_hash_desc),
                        checked = skipHash,
                        onChange = { skipHash = it },
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    val n = node ?: return@TextButton
                    val key = if (opener == SnapshotOpener.SECRET_KEY) secret else ""
                    val pass = if (opener == SnapshotOpener.PASSPHRASE) secret else ""
                    onNext(RecoverChoice(n, hostnames[n] ?: n, key, pass, skipHash && opener == SnapshotOpener.NONE))
                },
                enabled = ready,
            ) { Text(stringResource(R.string.etcd_recover_next)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

private val EtcdRecoverPhase.label: Int
    get() = when (this) {
        EtcdRecoverPhase.DECRYPTING -> R.string.etcd_recover_step_decrypting
        EtcdRecoverPhase.UPLOADING -> R.string.etcd_recover_step_uploading
        EtcdRecoverPhase.BOOTSTRAPPING -> R.string.etcd_recover_step_bootstrapping
        EtcdRecoverPhase.WAITING -> R.string.etcd_recover_step_waiting
    }

/** The recovery's steps with their latest message and the result, with Cancel or OK. */
@Composable
fun EtcdRecoverPanel(state: EtcdRecoverState, onCancel: () -> Unit, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val failed = state.error != null
    val encrypted = state.picked?.info?.encrypted == true
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.etcd_recover_title), style = MaterialTheme.typography.titleSmall)
            if (state.loading) Text(stringResource(R.string.etcd_recover_reading), style = MaterialTheme.typography.bodyMedium)
            if (state.running || state.events.isNotEmpty()) {
                etcdRecoverTimeline(state.events, state.finished, failed, encrypted).forEach { (phase, status) ->
                    val own = state.events.filter { it.phase == phase.wire }
                    TimelineRow(stringResource(phase.label), status, own.firstOrNull()?.at ?: 0, own.lastOrNull()?.message.orEmpty())
                }
                state.events.lastOrNull { it.phase == EtcdRecoverPhase.UPLOADING.wire && it.total > 0 }?.let {
                    Text(
                        stringResource(R.string.etcd_recover_uploaded, formatBytes(it.bytes), formatBytes(it.total)),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            when {
                state.running -> TextButton(onClick = onCancel) { Text(stringResource(R.string.common_cancel)) }
                state.loading || state.picked != null && !state.finished -> {}
                failed -> {
                    Text(state.error.orEmpty(), color = colors.bad, style = MaterialTheme.typography.bodySmall)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
                state.finished -> {
                    Text(stringResource(R.string.etcd_recover_done), color = colors.ok)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
            }
        }
    }
}
