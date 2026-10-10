package name.levis.ichor.ui.etcd

import android.net.Uri
import android.provider.DocumentsContract
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
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
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.EtcdFixEvent
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.EtcdFixMember
import name.levis.ichor.model.EtcdFixPhase
import name.levis.ichor.model.EtcdFixProgress
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.SnapshotEncryption
import name.levis.ichor.model.defragOrder
import name.levis.ichor.model.etcdFixTimeline
import name.levis.ichor.model.nodeHostnames
import name.levis.ichor.model.snapshotCandidates
import name.levis.ichor.model.snapshotFileName
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.openTruncating
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.upgrade.TimelineRow
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatBytes
import java.io.File
import java.util.UUID

/** A snapshot to take first: from [target], protected by [encryption], saved into [uri] named [name]. */
data class FixSnapshot(val target: SnapshotTarget, val encryption: SnapshotEncryption, val uri: Uri, val name: String)

data class EtcdFixState(
    val running: Boolean = false,
    val events: List<EtcdFixProgress> = emptyList(),
    val finished: Boolean = false,
    val error: String? = null,
    /** The snapshot's document name once saved. */
    val snapshotSaved: String? = null,
) {
    val idle: Boolean get() = !running && !finished
}

/**
 * The one-tap NOSPACE fix. Go writes the snapshot into the app cache (it needs a real path);
 * it is copied to the document the user picked once the run ends, and the cache copy deleted.
 */
class EtcdFixViewModel(private val talos: TalosRepository, private val app: TalosApp) : ViewModel() {
    private val _state = MutableStateFlow(EtcdFixState())
    val state: StateFlow<EtcdFixState> = _state.asStateFlow()
    private var job: Job? = null

    /** The snapshot chosen before the file picker opened (in memory only). */
    var pending: Pair<SnapshotTarget, SnapshotEncryption>? = null

    fun start(snapshot: FixSnapshot?) {
        if (_state.value.running) return
        _state.value = EtcdFixState(running = true)
        job = viewModelScope.launch {
            val dir = File(app.cacheDir, FIX_DIR)
            val temp = File(dir, "${UUID.randomUUID()}.snapshot")
            var saved = false
            try {
                withContext(Dispatchers.IO) {
                    dir.deleteRecursively()
                    dir.mkdirs()
                }
                val dest = if (snapshot != null) temp.path else ""
                val encryption = snapshot?.encryption ?: SnapshotEncryption.None
                talos.etcdNospaceFix(snapshot?.target?.node.orEmpty(), dest, encryption).collect { event ->
                    when (event) {
                        is EtcdFixEvent.Progress -> _state.update { it.copy(events = it.events + event.progress) }
                        is EtcdFixEvent.Done -> _state.update { it.copy(error = event.error) }
                    }
                }
                if (snapshot != null && withContext(Dispatchers.IO) { temp.exists() }) {
                    withContext(Dispatchers.IO) {
                        openTruncating(app, snapshot.uri, R.string.etcd_snapshot_open_failed).use { out -> temp.inputStream().use { it.copyTo(out) } }
                    }
                    saved = true
                    _state.update { it.copy(snapshotSaved = snapshot.name) }
                }
            } catch (e: CancellationException) {
                // Stopped by Cancel (or the screen left): Go stops before its next step.
                _state.update { it.copy(error = it.error ?: app.getString(R.string.etcd_fix_stopped)) }
                throw e
            } catch (e: Throwable) {
                _state.update { it.copy(error = it.error ?: e.uiText().resolve(app)) }
            } finally {
                _state.update { it.copy(running = false, finished = true) }
                withContext(NonCancellable + Dispatchers.IO) {
                    temp.delete()
                    File(temp.path + ".part").delete()
                    // An empty or partial file must not pass for a backup.
                    if (snapshot != null && !saved) runCatching { DocumentsContract.deleteDocument(app.contentResolver, snapshot.uri) }
                }
            }
        }
    }

    /** Stops the run before its next step. */
    fun cancel() {
        job?.cancel()
    }

    fun dismiss() {
        if (!_state.value.running) _state.value = EtcdFixState()
    }

    private companion object {
        const val FIX_DIR = "nospace-fix"
    }
}

/**
 * The "Fix NOSPACE…" flow: confirmation (with "take a snapshot first", on by default), the
 * snapshot's protection, the app lock, then the file picker. Returns the function starting
 * it; call it at screen level so the file picker result is never lost.
 */
@Composable
fun rememberEtcdFixFlow(vm: EtcdFixViewModel, contextName: String, fingerprint: String): (EtcdOverview) -> Unit {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    var confirming by remember { mutableStateOf<EtcdOverview?>(null) }
    var protecting by remember { mutableStateOf<SnapshotTarget?>(null) }
    val savedKeys by app.snapshotKeys.keys.collectAsStateWithLifecycle()

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
        val pending = vm.pending
        vm.pending = null
        if (uri != null && pending != null) {
            val name = snapshotFileName(contextName, pending.first.hostname, encrypted = pending.second != SnapshotEncryption.None)
            vm.start(FixSnapshot(pending.first, pending.second, uri, name))
        }
    }

    fun authenticated(action: () -> Unit) {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            action()
            return
        }
        scope.launch {
            if (authenticate(activity, context.getString(R.string.etcd_fix_auth)) is AuthResult.Success) action()
        }
    }

    confirming?.let { etcd ->
        val hostnames = etcd.nodeHostnames()
        val target = snapshotCandidates(etcd.statuses).firstOrNull()?.let { SnapshotTarget(it.node, hostnames[it.node] ?: it.node, it.dbSize) }
        EtcdFixConfirmDialog(
            order = defragOrder(etcd.statuses).map { hostnames[it.node] ?: it.node },
            canSnapshot = target != null,
            onConfirm = { snapshot ->
                confirming = null
                if (snapshot && target != null) protecting = target else authenticated { vm.start(null) }
            },
            onDismiss = { confirming = null },
        )
    }
    protecting?.let { target ->
        SnapshotEncryptionDialog(
            hostname = target.hostname,
            savedKeys = savedKeys[fingerprint].orEmpty(),
            checkKeys = app.talosRepository::checkSnapshotRecipients,
            onConfirm = { encryption ->
                protecting = null
                if (encryption is SnapshotEncryption.Keys) app.snapshotKeys.set(fingerprint, encryption.recipients)
                authenticated {
                    vm.pending = target to encryption
                    saver.launch(snapshotFileName(contextName, target.hostname, encrypted = encryption != SnapshotEncryption.None))
                }
            },
            onDismiss = { protecting = null },
        )
    }

    return { etcd -> confirming = etcd }
}

@Composable
private fun EtcdFixConfirmDialog(order: List<String>, canSnapshot: Boolean, onConfirm: (snapshot: Boolean) -> Unit, onDismiss: () -> Unit) {
    var snapshot by remember { mutableStateOf(canSnapshot) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.etcd_fix_confirm_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.etcd_fix_confirm_body, order.joinToString(" → ")))
                if (canSnapshot) {
                    ToggleRow(
                        title = stringResource(R.string.etcd_fix_snapshot),
                        description = stringResource(R.string.etcd_fix_snapshot_desc),
                        checked = snapshot,
                        onChange = { snapshot = it },
                    )
                }
            }
        },
        confirmButton = { TextButton(onClick = { onConfirm(snapshot) }) { Text(stringResource(R.string.etcd_fix_start)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

private val EtcdFixPhase.label: Int
    get() = when (this) {
        EtcdFixPhase.SNAPSHOT -> R.string.etcd_fix_step_snapshot
        EtcdFixPhase.DEFRAG -> R.string.etcd_fix_step_defrag
        EtcdFixPhase.DISARM -> R.string.etcd_fix_step_disarm
        EtcdFixPhase.RECHECK -> R.string.etcd_fix_step_recheck
    }

private val EtcdFixMember.stateLabel: Int
    get() = when (state) {
        EtcdFixMember.STATE_RUNNING -> R.string.etcd_fix_member_running
        EtcdFixMember.STATE_DONE -> R.string.etcd_fix_member_done
        EtcdFixMember.STATE_FAILED -> R.string.etcd_fix_member_failed
        else -> R.string.etcd_fix_member_pending
    }

/** The run's steps with their latest message, the members and the result, with Cancel or OK. */
@Composable
fun EtcdFixPanel(state: EtcdFixState, onCancel: () -> Unit, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val failed = state.error != null
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.etcd_fix_title), style = MaterialTheme.typography.titleSmall)
            etcdFixTimeline(state.events, state.finished, failed).forEach { (phase, status) ->
                val own = state.events.filter { it.phase == phase.wire }
                TimelineRow(stringResource(phase.label), status, own.firstOrNull()?.at ?: 0, own.lastOrNull()?.message.orEmpty())
            }
            state.events.lastOrNull()?.members?.forEach { member ->
                val reclaimed = if (member.reclaimedBytes > 0) " · " + stringResource(R.string.etcd_fix_member_reclaimed, formatBytes(member.reclaimedBytes)) else ""
                Text(
                    "${member.hostname}: ${stringResource(member.stateLabel)}$reclaimed",
                    style = MaterialTheme.typography.bodySmall,
                    color = if (member.state == EtcdFixMember.STATE_FAILED) colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            state.snapshotSaved?.let { Text(stringResource(R.string.etcd_fix_snapshot_saved, it), style = MaterialTheme.typography.bodySmall) }
            when {
                state.running -> TextButton(onClick = onCancel) { Text(stringResource(R.string.common_cancel)) }
                failed -> {
                    Text(state.error.orEmpty(), color = colors.bad, style = MaterialTheme.typography.bodySmall)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
                else -> {
                    Text(stringResource(R.string.etcd_fix_done), color = colors.ok)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
            }
        }
    }
}
