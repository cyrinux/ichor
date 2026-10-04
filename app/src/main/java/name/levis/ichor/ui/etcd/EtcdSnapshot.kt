package name.levis.ichor.ui.etcd

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.SnapshotEvent
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.VersionNotice
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.openTruncating
import name.levis.ichor.ui.components.text
import name.levis.ichor.model.nodeHostnames
import name.levis.ichor.model.snapshotCandidates
import name.levis.ichor.model.snapshotFileName
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatBytes
import java.io.File
import java.util.UUID

/** The member a snapshot is taken from; [dbSize] (0 if unknown) sizes the progress bar. */
data class SnapshotTarget(val node: String, val hostname: String, val dbSize: Long)

sealed interface SnapshotState {
    data object Idle : SnapshotState
    data class Running(val hostname: String, val bytes: Long, val total: Long) : SnapshotState
    data class Saving(val hostname: String) : SnapshotState
    data class Done(val hostname: String, val size: Long, val sha256: String) : SnapshotState
    data class Failed(val message: UiText) : SnapshotState
}

val SnapshotState.busy: Boolean get() = this is SnapshotState.Running || this is SnapshotState.Saving

/**
 * Downloads an etcd snapshot into the app cache (Go needs a real path), then copies it to the
 * document the user picked. The cache copy is deleted in every case; leaving the screen
 * clears this view model, which cancels the download.
 */
class EtcdSnapshotViewModel(private val talos: TalosRepository, private val app: TalosApp) : ViewModel() {
    private val _state = MutableStateFlow<SnapshotState>(SnapshotState.Idle)
    val state: StateFlow<SnapshotState> = _state.asStateFlow()
    private var job: Job? = null

    /** Member chosen before the file picker opened (kept across activity recreation). */
    var pending: SnapshotTarget? = null

    fun start(target: SnapshotTarget, uri: Uri) {
        if (_state.value.busy) return
        job = viewModelScope.launch {
            val dir = File(app.cacheDir, SNAPSHOT_DIR)
            val temp = File(dir, "${UUID.randomUUID()}.snapshot")
            var saved = false
            try {
                withContext(Dispatchers.IO) {
                    dir.deleteRecursively() // leftovers of a killed process
                    dir.mkdirs()
                }
                _state.value = SnapshotState.Running(target.hostname, 0, target.dbSize)
                var result: SnapshotEvent? = null
                talos.etcdSnapshot(target.node, temp.path).collect { event ->
                    if (event is SnapshotEvent.Progress) {
                        _state.value = SnapshotState.Running(target.hostname, event.bytes, target.dbSize)
                    } else {
                        result = event
                    }
                }
                _state.value = when (val r = result) {
                    is SnapshotEvent.Done -> {
                        _state.value = SnapshotState.Saving(target.hostname)
                        withContext(Dispatchers.IO) { copyToDocument(File(r.path), uri) }
                        saved = true
                        SnapshotState.Done(target.hostname, r.size, r.sha256)
                    }
                    is SnapshotEvent.Failed -> SnapshotState.Failed(UiText.Raw(r.message))
                    else -> SnapshotState.Failed(UiText.Res(R.string.etcd_snapshot_interrupted))
                }
            } catch (e: CancellationException) {
                _state.value = SnapshotState.Idle
                throw e
            } catch (e: Throwable) {
                _state.value = SnapshotState.Failed(e.uiText())
            } finally {
                withContext(NonCancellable + Dispatchers.IO) {
                    temp.delete()
                    File(temp.path + ".part").delete()
                    // An empty or partial file must not pass for a backup.
                    if (!saved) runCatching { DocumentsContract.deleteDocument(app.contentResolver, uri) }
                }
            }
        }
    }

    fun cancel() {
        job?.cancel()
    }

    fun fail(message: UiText) {
        if (!_state.value.busy) _state.value = SnapshotState.Failed(message)
    }

    fun dismiss() {
        if (!_state.value.busy) _state.value = SnapshotState.Idle
    }

    private fun copyToDocument(source: File, uri: Uri) {
        openTruncating(app, uri, R.string.etcd_snapshot_open_failed).use { stream -> source.inputStream().use { it.copyTo(stream) } }
    }

    private companion object {
        const val SNAPSHOT_DIR = "snapshots"
    }
}

/**
 * The "Save snapshot…" flow: member picker (when several are healthy), secrets warning,
 * app-lock authentication, then the system file picker. Returns the function starting it.
 * Must be called at screen level so the file picker result is never lost.
 */
@Composable
fun rememberSnapshotFlow(vm: EtcdSnapshotViewModel, contextName: String): (EtcdOverview) -> Unit {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    var choices by remember { mutableStateOf<List<SnapshotTarget>?>(null) }
    var warning by remember { mutableStateOf<SnapshotTarget?>(null) }

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
        val target = vm.pending
        vm.pending = null
        if (uri != null && target != null) vm.start(target, uri)
    }

    fun pickFile(target: SnapshotTarget) {
        vm.pending = target
        saver.launch(snapshotFileName(contextName, target.hostname))
    }

    fun accepted(target: SnapshotTarget) {
        warning = null
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            pickFile(target)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.etcd_snapshot_auth))) {
                AuthResult.Success -> pickFile(target)
                is AuthResult.Failure -> vm.fail(UiText.Raw(auth.message))
            }
        }
    }

    choices?.let { list ->
        MemberPickerDialog(
            members = list,
            onPick = {
                choices = null
                warning = it
            },
            onDismiss = { choices = null },
        )
    }
    warning?.let { target ->
        ConfirmDialog(
            title = stringResource(R.string.etcd_snapshot_warning_title),
            text = stringResource(R.string.etcd_snapshot_warning_body, target.hostname),
            confirm = stringResource(R.string.etcd_snapshot_continue),
            onConfirm = { accepted(target) },
            onDismiss = { warning = null },
        )
    }

    return { etcd ->
        val hostnames = etcd.nodeHostnames()
        val targets = snapshotCandidates(etcd.statuses).map { SnapshotTarget(it.node, hostnames[it.node] ?: it.node, it.dbSize) }
        when {
            targets.isEmpty() -> vm.fail(UiText.Res(R.string.etcd_snapshot_no_member))
            targets.size == 1 -> warning = targets.single()
            else -> choices = targets
        }
    }
}

@Composable
private fun MemberPickerDialog(members: List<SnapshotTarget>, onPick: (SnapshotTarget) -> Unit, onDismiss: () -> Unit) {
    // The first candidate is the default: a healthy follower when there is one.
    var selected by remember { mutableStateOf(members.first()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.etcd_snapshot_pick_member)) },
        text = {
            Column(Modifier.selectableGroup()) {
                members.forEach { member ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.fillMaxWidth()
                            .selectable(selected = member == selected, role = Role.RadioButton) { selected = member },
                    ) {
                        RadioButton(selected = member == selected, onClick = null)
                        Column {
                            Text(member.hostname)
                            Text(member.node, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = { onPick(selected) }) { Text(stringResource(R.string.etcd_snapshot_continue)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
fun SnapshotPanel(state: SnapshotState, onSave: () -> Unit, onCancel: () -> Unit, onDismiss: () -> Unit, notice: VersionNotice? = null) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.etcd_snapshot_title), style = MaterialTheme.typography.titleSmall)
            when (state) {
                SnapshotState.Idle -> {
                    MutedText(stringResource(R.string.etcd_snapshot_explainer))
                    OutlinedButton(onClick = onSave, enabled = notice == null, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.etcd_snapshot_save))
                    }
                    notice?.let { InfoNotice(it.text()) }
                }
                is SnapshotState.Running -> {
                    Text(
                        if (state.total > 0) {
                            stringResource(R.string.etcd_snapshot_progress_of, state.hostname, formatBytes(state.bytes), formatBytes(state.total))
                        } else {
                            stringResource(R.string.etcd_snapshot_progress, state.hostname, formatBytes(state.bytes))
                        },
                        style = MaterialTheme.typography.bodySmall,
                    )
                    if (state.total > 0) {
                        LinearProgressIndicator(
                            progress = { (state.bytes.toFloat() / state.total).coerceIn(0f, 1f) },
                            modifier = Modifier.fillMaxWidth(),
                        )
                    } else {
                        LinearProgressIndicator(Modifier.fillMaxWidth())
                    }
                    TextButton(onClick = onCancel) { Text(stringResource(R.string.common_cancel)) }
                }
                is SnapshotState.Saving -> {
                    Text(stringResource(R.string.etcd_snapshot_saving), style = MaterialTheme.typography.bodySmall)
                    LinearProgressIndicator(Modifier.fillMaxWidth())
                }
                is SnapshotState.Done -> {
                    Text(stringResource(R.string.etcd_snapshot_done, state.hostname, formatBytes(state.size)), color = colors.ok)
                    Text(stringResource(R.string.etcd_snapshot_sha256), style = MaterialTheme.typography.labelMedium)
                    SelectionContainer {
                        Text(state.sha256, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                    }
                    Row {
                        TextButton(onClick = { copySha(context, state.sha256) }) { Text(stringResource(R.string.etcd_snapshot_copy_sha)) }
                        TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                    }
                }
                is SnapshotState.Failed -> {
                    Text(state.message.asString(), color = colors.bad, style = MaterialTheme.typography.bodySmall)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
            }
        }
    }
}

private fun copySha(context: Context, sha: String) {
    context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("SHA-256", sha))
}
