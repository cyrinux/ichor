package name.levis.ichor.ui.kubebrowser

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SheetValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.HelmRollbackChange
import name.levis.ichor.model.HelmRollbackPlan
import name.levis.ichor.model.canRun
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** The rollback sheet of one target [revision]: its plan once read, and the last error. */
data class HelmRollbackState(
    val revision: Int,
    val plan: HelmRollbackPlan? = null,
    val loading: Boolean = true,
    val running: Boolean = false,
    val error: UiText? = null,
)

/** How a rollback ended, for a toast: [error] null when it succeeded. */
data class HelmRollbackResult(val revision: Int, val error: UiText? = null)

/**
 * Plans and runs the rollback of one release in its view model's [scope], so neither dies
 * with a rotation; [onFinished] reloads the release once a rollback was attempted.
 */
class HelmRollbackController(
    private val browser: KubeBrowserRepository,
    private val namespace: String,
    private val name: String,
    private val scope: CoroutineScope,
    private val onFinished: () -> Unit,
) {
    private val _state = MutableStateFlow<HelmRollbackState?>(null)
    val state: StateFlow<HelmRollbackState?> = _state.asStateFlow()
    private val _results = Channel<HelmRollbackResult>(Channel.BUFFERED)
    val results: Flow<HelmRollbackResult> = _results.receiveAsFlow()
    private var planJob: Job? = null

    fun open(revision: Int) {
        if (_state.value?.running == true) return
        _state.value = HelmRollbackState(revision)
        loadPlan()
    }

    fun loadPlan() {
        val current = _state.value ?: return
        if (current.running) return
        planJob?.cancel()
        _state.value = current.copy(loading = true, error = null)
        planJob = scope.launch {
            val next = try {
                current.copy(plan = browser.helmRollbackPlan(namespace, name, current.revision), loading = false, error = null)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                current.copy(loading = false, error = e.uiText())
            }
            _state.update { if (it?.revision == current.revision && !it.running) next else it }
        }
    }

    /** Closes the sheet; a running rollback goes on and reports through [results]. */
    fun dismiss() {
        planJob?.cancel()
        _state.value = null
    }

    fun run() {
        val current = _state.value ?: return
        val plan = current.plan ?: return
        if (current.running || current.loading || !plan.canRun) return
        val revision = plan.to.takeIf { it > 0 } ?: current.revision
        _state.value = current.copy(running = true, error = null)
        scope.launch {
            val error = try {
                browser.helmRollback(namespace, name, revision)
                null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                e.uiText()
            }
            val shown = _state.value?.takeIf { it.revision == current.revision }
            if (error == null) {
                if (shown != null) _state.value = null
                _results.send(HelmRollbackResult(revision))
            } else if (shown != null) {
                _state.value = shown.copy(running = false, error = error)
            } else {
                _results.send(HelmRollbackResult(revision, error))
            }
            onFinished()
        }
    }
}

/** The sheet of [controller] while one is open, and a toast for each rollback that ended. */
@Composable
fun HelmRollbackHost(controller: HelmRollbackController, state: HelmRollbackState?) {
    val context = LocalContext.current
    LaunchedEffect(controller) {
        controller.results.collect { r ->
            val text = r.error?.resolve(context) ?: context.getString(R.string.helm_rollback_done, r.revision.toString())
            Toast.makeText(context, text, if (r.error != null) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }
    if (state != null) HelmRollbackSheet(state, onRetry = controller::loadPlan, onConfirm = controller::run, onDismiss = controller::dismiss)
}

/** What rolling back to [HelmRollbackState.revision] would change, checked by dry runs, and the confirm. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun HelmRollbackSheet(state: HelmRollbackState, onRetry: () -> Unit, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val running by rememberUpdatedState(state.running)
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true, confirmValueChange = { it != SheetValue.Hidden || !running })
    val plan = state.plan
    ModalBottomSheet(onDismissRequest = { if (!running) onDismiss() }, sheetState = sheetState) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            val target = plan?.to?.takeIf { it > 0 } ?: state.revision
            Text(stringResource(R.string.helm_rollback_title, target.toString()), style = MaterialTheme.typography.titleMedium)
            if (state.loading) {
                MutedText(stringResource(R.string.helm_rollback_checking))
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
            if (plan != null) PlanBody(plan)
            state.error?.let { InlineError(it.asString()) }
            if (plan == null && state.error != null && !state.loading) {
                OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
            if (state.running) LinearProgressIndicator(Modifier.fillMaxWidth())
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = onDismiss, enabled = !state.running) { Text(stringResource(R.string.common_cancel)) }
                Button(onClick = onConfirm, enabled = plan?.canRun == true && !state.running && !state.loading) {
                    if (state.running) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                    Text(stringResource(R.string.helm_rollback_title, target.toString()), modifier = Modifier.padding(start = if (state.running) 8.dp else 0.dp))
                }
            }
        }
    }
}

@Composable
private fun PlanBody(plan: HelmRollbackPlan) {
    val colors = LocalStatusColors.current
    InfoRow(stringResource(R.string.helm_rollback_now), side(plan.from, plan.fromChart, plan.fromAppVersion), mono = true)
    InfoRow(stringResource(R.string.helm_rollback_after), side(plan.to, plan.toChart, plan.toAppVersion), mono = true)
    if (plan.fluxOwner.isNotEmpty()) {
        Text(stringResource(R.string.helm_rollback_flux, plan.fluxOwner), style = MaterialTheme.typography.bodySmall, color = colors.warn)
    }
    if (plan.blockers.isNotEmpty()) {
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(stringResource(R.string.helm_rollback_blocked), style = MaterialTheme.typography.labelLarge, color = colors.bad)
            plan.blockers.forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = colors.bad) }
        }
    }
    SectionTitle(stringResource(R.string.helm_rollback_changes))
    if (plan.changes.isEmpty()) MutedText(stringResource(R.string.helm_rollback_none))
    plan.changes.forEach { ChangeRow(it) }
    if (plan.unchanged > 0) MutedText(stringResource(R.string.helm_rollback_unchanged, plan.unchanged.toString()))
    if (plan.changes.any { it.action == "keep" }) MutedText(stringResource(R.string.helm_rollback_keep_hint))
    if (plan.canRun) MutedText(stringResource(R.string.helm_rollback_checked))
    MutedText(stringResource(R.string.helm_rollback_how))
}

@Composable
private fun side(revision: Int, chart: String, appVersion: String): String =
    listOfNotNull("#$revision $chart", appVersion.takeIf { it.isNotEmpty() }?.let { "(${stringResource(R.string.kb_helm_app_version, it)})" }).joinToString(" ")

@Composable
private fun ChangeRow(c: HelmRollbackChange) {
    val colors = LocalStatusColors.current
    val (label, color) = when (c.action) {
        "create" -> stringResource(R.string.helm_rollback_create) to colors.ok
        "update" -> stringResource(R.string.helm_rollback_update) to colors.warn
        "delete" -> stringResource(R.string.helm_rollback_delete) to colors.bad
        "keep" -> stringResource(R.string.helm_rollback_keep) to colors.muted
        else -> c.action to colors.muted
    }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TagBadge(label, color)
            Text(
                listOf(c.kind, listOf(c.namespace, c.name).filter { it.isNotEmpty() }.joinToString("/")).filter { it.isNotEmpty() }.joinToString(" "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
        if (c.error.isNotEmpty()) Text(c.error, style = MaterialTheme.typography.bodySmall, color = colors.bad)
    }
}
