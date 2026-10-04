package name.levis.ichor.ui.etcd

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import android.provider.DocumentsContract
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
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
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.SnapshotEvent
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.VersionNotice
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.openTruncating
import name.levis.ichor.ui.components.text
import name.levis.ichor.model.nodeHostnames
import name.levis.ichor.model.snapshotCandidates
import name.levis.ichor.model.snapshotFileName
import name.levis.ichor.model.snapshotRestoreCommands
import name.levis.ichor.model.SnapshotEncryption
import name.levis.ichor.model.SnapshotMode
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
    /** [fileName] is the saved document's name, for the restore commands. */
    data class Done(val hostname: String, val size: Long, val sha256: String, val mode: SnapshotMode, val fileName: String) : SnapshotState
    data class Failed(val message: UiText) : SnapshotState
}

/** A snapshot waiting for the file picker; [encryption] may hold a passphrase (its toString masks it). */
data class PendingSnapshot(val target: SnapshotTarget, val encryption: SnapshotEncryption, val suggestedName: String)

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

    /** Member, protection and file name chosen before the file picker opened (kept across activity recreation, in memory only). */
    var pending: PendingSnapshot? = null

    fun start(target: SnapshotTarget, encryption: SnapshotEncryption, uri: Uri, suggestedName: String) {
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
                talos.etcdSnapshot(target.node, temp.path, encryption).collect { event ->
                    if (event is SnapshotEvent.Progress) {
                        _state.value = SnapshotState.Running(target.hostname, event.bytes, target.dbSize)
                    } else {
                        result = event
                    }
                }
                _state.value = when (val r = result) {
                    is SnapshotEvent.Done -> {
                        _state.value = SnapshotState.Saving(target.hostname)
                        val name = withContext(Dispatchers.IO) {
                            copyToDocument(File(r.path), uri)
                            displayName(uri) ?: suggestedName
                        }
                        saved = true
                        SnapshotState.Done(target.hostname, r.size, r.sha256, encryption.mode, name)
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

    /** The name the user gave the document in the picker, null when the provider does not say. */
    private fun displayName(uri: Uri): String? = runCatching {
        app.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) cursor.getString(0) else null
        }
    }.getOrNull()?.takeIf { it.isNotBlank() }

    private companion object {
        const val SNAPSHOT_DIR = "snapshots"
    }
}

/**
 * The "Save snapshot…" flow: member picker (when several are healthy), protection (age public
 * keys, passphrase or none), app-lock authentication, then the system file picker. Returns
 * the function starting it. Must be called at screen level so the file picker result is never lost.
 * [fingerprint] keys the public keys remembered for the cluster.
 */
@Composable
fun rememberSnapshotFlow(vm: EtcdSnapshotViewModel, contextName: String, fingerprint: String): (EtcdOverview) -> Unit {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    var choices by remember { mutableStateOf<List<SnapshotTarget>?>(null) }
    var warning by remember { mutableStateOf<SnapshotTarget?>(null) }
    val savedKeys by app.snapshotKeys.keys.collectAsStateWithLifecycle()

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
        val pending = vm.pending
        vm.pending = null
        if (uri != null && pending != null) vm.start(pending.target, pending.encryption, uri, pending.suggestedName)
    }

    fun pickFile(target: SnapshotTarget, encryption: SnapshotEncryption) {
        val name = snapshotFileName(contextName, target.hostname, encrypted = encryption != SnapshotEncryption.None)
        vm.pending = PendingSnapshot(target, encryption, name)
        saver.launch(name)
    }

    fun accepted(target: SnapshotTarget, encryption: SnapshotEncryption) {
        warning = null
        if (encryption is SnapshotEncryption.Keys) app.snapshotKeys.set(fingerprint, encryption.recipients)
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            pickFile(target, encryption)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.etcd_snapshot_auth))) {
                AuthResult.Success -> pickFile(target, encryption)
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
        SnapshotEncryptionDialog(
            hostname = target.hostname,
            savedKeys = savedKeys[fingerprint].orEmpty(),
            checkKeys = app.talosRepository::checkSnapshotRecipients,
            onConfirm = { accepted(target, it) },
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

@OptIn(ExperimentalLayoutApi::class)
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
                    var restore by remember { mutableStateOf(false) }
                    Text(stringResource(R.string.etcd_snapshot_done, state.hostname, formatBytes(state.size)), color = colors.ok)
                    Text(
                        stringResource(
                            when (state.mode) {
                                SnapshotMode.KEYS -> R.string.etcd_snapshot_done_keys
                                SnapshotMode.PASSPHRASE -> R.string.etcd_snapshot_done_passphrase
                                SnapshotMode.NONE -> R.string.etcd_snapshot_done_clear
                            },
                        ),
                        color = if (state.mode == SnapshotMode.NONE) colors.bad else Color.Unspecified,
                        style = MaterialTheme.typography.bodySmall,
                    )
                    Text(
                        stringResource(if (state.mode == SnapshotMode.NONE) R.string.etcd_snapshot_sha256 else R.string.etcd_snapshot_sha_clear),
                        style = MaterialTheme.typography.labelMedium,
                    )
                    SelectionContainer {
                        Text(state.sha256, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                    }
                    FlowRow {
                        TextButton(onClick = { restore = true }) { Text(stringResource(R.string.etcd_snapshot_restore)) }
                        TextButton(onClick = { copySha(context, state.sha256) }) { Text(stringResource(R.string.etcd_snapshot_copy_sha)) }
                        TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                    }
                    if (restore) {
                        SnapshotRestoreDialog(snapshotRestoreCommands(state.fileName, state.mode, state.sha256)) { restore = false }
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
