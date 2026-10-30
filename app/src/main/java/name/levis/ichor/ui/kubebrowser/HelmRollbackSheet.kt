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
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.SheetValue
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
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
import java.util.concurrent.atomic.AtomicLong
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.updateAndGet
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.HelmRollbackFollowHandle
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.HelmRollbackChange
import name.levis.ichor.model.HelmRollbackFollowProgress
import name.levis.ichor.model.HelmRollbackOptions
import name.levis.ichor.model.HelmRollbackPhase
import name.levis.ichor.model.HelmRollbackPlan
import name.levis.ichor.model.canRun
import name.levis.ichor.model.isHelmHistoryNotPruned
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.goErrorText
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** The rollback sheet of one target [revision]: its plan once read, and the last error. */
data class HelmRollbackState(
    val revision: Int,
    val plan: HelmRollbackPlan? = null,
    val loading: Boolean = true,
    val running: Boolean = false,
    val error: UiText? = null,
    val options: HelmRollbackOptions = HelmRollbackOptions(),
    /** Set once the rollback is done and its workloads are followed; the sheet shows only that. */
    val follow: HelmRollbackFollowState? = null,
)

/**
 * How a rollback ended, for a toast: [error] null when it succeeded; [warning] what went
 * wrong after a rollback that is done (old revisions not pruned).
 */
data class HelmRollbackResult(val revision: Int, val error: UiText? = null, val warning: UiText? = null)

/**
 * Plans and runs the rollback of one release in its view model's [scope], so neither dies
 * with a rotation; [onFinished] reloads the release once a rollback was attempted. With
 * [HelmRollbackOptions.wait] the sheet then follows the release's workloads until ready.
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
    // Read and cleared from the scope's completion handler, on whatever thread ends it.
    @Volatile private var followHandle: HelmRollbackFollowHandle? = null
    private val followIds = AtomicLong()

    init {
        scope.coroutineContext[Job]?.invokeOnCompletion { stopFollow() }
    }

    fun open(revision: Int) {
        if (_state.value?.running == true) return
        stopFollow()
        _state.value = HelmRollbackState(revision)
        loadPlan()
    }

    fun loadPlan() = readPlan(debounce = false)

    private fun readPlan(debounce: Boolean) {
        val current = _state.value ?: return
        if (current.running || current.follow != null) return
        planJob?.cancel()
        _state.value = current.copy(loading = true, error = null)
        planJob = scope.launch {
            if (debounce) delay(OPTIONS_DEBOUNCE_MS)
            val next = try {
                current.copy(plan = browser.helmRollbackPlan(namespace, name, current.revision, current.options), loading = false, error = null)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // The plan read for other options must not be confirmed: retry reads it again.
                current.copy(plan = null, loading = false, error = e.uiText())
            }
            // Wait changed meanwhile does not change the dry run: the result stands, with the
            // options as they are now. Force or history max changed: a newer read is coming.
            _state.update {
                if (it?.revision == current.revision && !it.running && it.follow == null && !it.options.changesPlan(current.options)) {
                    next.copy(options = it.options)
                } else {
                    it
                }
            }
        }
    }

    /** New options; force and history-max change the dry run, so the plan is read again. */
    fun setOptions(options: HelmRollbackOptions) {
        val current = _state.value ?: return
        if (current.running || current.follow != null || options == current.options) return
        _state.value = current.copy(options = options)
        if (options.changesPlan(current.options)) readPlan(debounce = true)
    }

    /** Closes the sheet and stops following; a running rollback goes on and reports through [results]. */
    fun dismiss() {
        planJob?.cancel()
        stopFollow()
        _state.value = null
    }

    fun run() {
        val current = _state.value ?: return
        val plan = current.plan ?: return
        if (current.running || current.loading || current.follow != null || !plan.canRun) return
        val revision = plan.to.takeIf { it > 0 } ?: current.revision
        val options = current.options
        _state.value = current.copy(running = true, error = null)
        scope.launch {
            var warning: UiText? = null
            val error = try {
                browser.helmRollback(namespace, name, revision, options)
                null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // The rollback is done when only the history pruning after it was refused.
                if (isHelmHistoryNotPruned(e.message)) {
                    warning = e.uiText()
                    null
                } else {
                    e.uiText()
                }
            }
            val shown = _state.value?.takeIf { it.revision == current.revision }
            when {
                error == null && shown != null && options.wait && plan.workloads > 0 -> startFollow(shown, revision, warning)
                error == null -> {
                    if (shown != null) _state.value = null
                    _results.send(HelmRollbackResult(revision, warning = warning))
                }
                shown != null -> _state.value = shown.copy(running = false, error = error)
                else -> _results.send(HelmRollbackResult(revision, error))
            }
            onFinished()
        }
    }

    private fun startFollow(shown: HelmRollbackState, revision: Int, warning: UiText?) {
        val id = followIds.incrementAndGet()
        _state.value = shown.copy(running = false, follow = HelmRollbackFollowState(id, revision, warning = warning))
        followHandle = try {
            browser.startHelmRollbackFollow(
                namespace,
                name,
                onProgress = { json -> onFollowProgress(id, json) },
                onDone = { result, errMessage -> onFollowDone(id, result, errMessage) },
            )
        } catch (e: Throwable) {
            updateFollow(id) { it.copy(error = e.uiText()) }
            null
        }
    }

    private fun onFollowProgress(id: Long, json: String) {
        val progress = runCatching { TalosJson.decodeFromString(HelmRollbackFollowProgress.serializer(), json) }.getOrNull() ?: return
        updateFollow(id) { it.copy(progress = progress) }
    }

    private fun onFollowDone(id: Long, result: String, errMessage: String) {
        if (result.isEmpty() && errMessage.isEmpty()) return // cancelled
        val error = errMessage.takeIf { it.isNotEmpty() }?.let { UiText.Raw(goErrorText(it)) }
        val follow = updateFollow(id) { it.copy(result = result, error = error) } ?: return
        if (result == HelmRollbackPhase.SUCCEEDED && error == null) {
            _results.trySend(HelmRollbackResult(follow.revision))
        }
    }

    /** Applies [change] to follow [id] if it is still the one shown; the updated follow, or null. */
    private fun updateFollow(id: Long, change: (HelmRollbackFollowState) -> HelmRollbackFollowState): HelmRollbackFollowState? {
        val next = _state.updateAndGet { s ->
            val f = s?.follow
            if (s == null || f == null || f.id != id) s else s.copy(follow = change(f))
        }
        return next?.follow?.takeIf { it.id == id }
    }

    private fun stopFollow() {
        followHandle?.cancel()
        followHandle = null
    }

    private companion object {
        const val OPTIONS_DEBOUNCE_MS = 400L
    }
}

/** The sheet of [controller] while one is open, and a toast for each rollback that ended. */
@Composable
fun HelmRollbackHost(controller: HelmRollbackController, state: HelmRollbackState?) {
    val context = LocalContext.current
    LaunchedEffect(controller) {
        controller.results.collect { r ->
            val done = context.getString(R.string.helm_rollback_done, r.revision.toString())
            val text = r.error?.resolve(context) ?: listOfNotNull(done, r.warning?.resolve(context)).joinToString("\n")
            val long = r.error != null || r.warning != null
            Toast.makeText(context, text, if (long) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }
    if (state != null) {
        HelmRollbackSheet(
            state,
            onRetry = controller::loadPlan,
            onOptions = controller::setOptions,
            onConfirm = controller::run,
            onDismiss = controller::dismiss,
        )
    }
}

/** What rolling back to [HelmRollbackState.revision] would change, checked by dry runs, and the confirm. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun HelmRollbackSheet(
    state: HelmRollbackState,
    onRetry: () -> Unit,
    onOptions: (HelmRollbackOptions) -> Unit,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
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
            val follow = state.follow
            if (follow != null) {
                HelmRollbackFollowBody(follow, onStop = onDismiss)
            } else {
                PlanContent(state, target, onRetry, onOptions, onConfirm, onDismiss)
            }
        }
    }
}

/** The plan, the options and the confirm, until the rollback is done. */
@Composable
private fun PlanContent(
    state: HelmRollbackState,
    target: Int,
    onRetry: () -> Unit,
    onOptions: (HelmRollbackOptions) -> Unit,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val plan = state.plan
    if (state.loading) {
        MutedText(stringResource(R.string.helm_rollback_checking))
        LinearProgressIndicator(Modifier.fillMaxWidth())
    }
    if (plan != null) PlanBody(plan, state.options, optionsEnabled = !state.running, onOptions = onOptions)
    state.error?.let { InlineError(it.asString()) }
    if (plan == null && state.error != null && !state.loading) {
        IchorOutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
    }
    if (state.running) {
        // The rollback is one call: the hooks run (and are waited for) inside it.
        if (plan?.hooks?.isNotEmpty() == true) MutedText(stringResource(R.string.helm_rollback_hooks_running))
        LinearProgressIndicator(Modifier.fillMaxWidth())
    }
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) {
        IchorTextButton(onClick = onDismiss, enabled = !state.running) { Text(stringResource(R.string.common_cancel)) }
        IchorButton(onClick = onConfirm, enabled = plan?.canRun == true && !state.running && !state.loading) {
            if (state.running) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Text(stringResource(R.string.helm_rollback_title, target.toString()), modifier = Modifier.padding(start = if (state.running) 8.dp else 0.dp))
        }
    }
}

@Composable
private fun PlanBody(plan: HelmRollbackPlan, options: HelmRollbackOptions, optionsEnabled: Boolean, onOptions: (HelmRollbackOptions) -> Unit) {
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
    HelmRollbackOptionsBlock(plan, options, enabled = optionsEnabled, onChange = onOptions)
    HelmRollbackHooksSection(plan.hooks)
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
        "replace" -> stringResource(R.string.helm_rollback_replace) to colors.warn
        "delete" -> stringResource(R.string.helm_rollback_delete) to colors.bad
        "keep" -> stringResource(R.string.helm_rollback_keep) to colors.muted
        else -> c.action to colors.muted
    }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Tag(label, color)
            Text(
                listOf(c.kind, listOf(c.namespace, c.name).filter { it.isNotEmpty() }.joinToString("/")).filter { it.isNotEmpty() }.joinToString(" "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
        if (c.error.isNotEmpty()) Text(c.error, style = MaterialTheme.typography.bodySmall, color = colors.bad)
    }
}
