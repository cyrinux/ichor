package name.levis.ichor.ui.dataservices

import android.content.Context
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.GARAGE_TRANQUILITY_FULL
import name.levis.ichor.model.GarageBlockReport
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageNode
import name.levis.ichor.model.GarageRepairOutcome
import name.levis.ichor.model.GarageRepairResult
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** The block report the sheet shows, for [instance]. */
data class GarageBlocksState(
    val instance: GarageInstance,
    val report: GarageBlockReport? = null,
    /** The last load failed (the report, if any, is the previous one). */
    val error: UiText? = null,
    val loading: Boolean = true,
    val repairing: Boolean = false,
)

/** Outcome of a Garage action, shown once. */
sealed interface GarageActionResult {
    data class Tranquility(val node: String, val value: Long, val error: UiText?) : GarageActionResult
    data class Repair(val result: GarageRepairResult?, val error: UiText?) : GarageActionResult
}

/**
 * Garage maintenance run in [scope] (a ViewModel's): resync tranquility changes, the block
 * error report and its repair. [onChanged] runs after an action that changed the cluster,
 * e.g. to refresh the data services.
 */
class GarageActions(
    private val scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<GarageActionResult>(scope)
    /** Keys ([tuningKey]) of the nodes whose tranquility change is in flight. */
    val tuning: StateFlow<Set<String>> get() = actions.busy

    private val _sheet = MutableStateFlow<GarageBlocksState?>(null)
    /** The open block report, null when closed. */
    val sheet: StateFlow<GarageBlocksState?> = _sheet.asStateFlow()
    private var loadJob: Job? = null

    val results: Flow<GarageActionResult> get() = actions.results

    fun setTranquility(instance: GarageInstance, node: GarageNode, value: Long) {
        actions.launch(tuningKey(instance, node), { dataServices.garageSetTranquility(instance, node.id, value) }, {
            GarageActionResult.Tranquility(node.label, value, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }

    fun openReport(instance: GarageInstance) {
        _sheet.value = GarageBlocksState(instance)
        reload()
    }

    fun closeReport() {
        loadJob?.cancel()
        _sheet.value = null
    }

    /** Loads the open report again, keeping the previous one on screen meanwhile. */
    fun reload() {
        val instance = _sheet.value?.instance ?: return
        loadJob?.cancel()
        _sheet.update { it?.copy(loading = true, error = null) }
        loadJob = scope.launch {
            try {
                val report = dataServices.garageBlockErrors(instance)
                _sheet.update { it?.copy(report = report, loading = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _sheet.update { it?.copy(error = e.uiText(), loading = false) }
            }
        }
    }

    /** Launches the repairs for the open report's cluster, then loads the report again. */
    fun repair() {
        val current = _sheet.value ?: return
        if (current.repairing) return
        actions.launch(REPAIR_KEY + current.instance.label, {
            _sheet.update { it?.copy(repairing = true) }
            try {
                dataServices.garageRepairBlocks(current.instance)
            } finally {
                _sheet.update { it?.copy(repairing = false) }
            }
        }, { GarageActionResult.Repair(it.getOrNull(), it.exceptionOrNull()?.uiText()) }) {
            reload()
            onChanged()
        }
    }

    companion object {
        private const val REPAIR_KEY = "repair|"

        fun tuningKey(instance: GarageInstance, node: GarageNode) = "${instance.label}|${node.id}"
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun GarageResultToasts(results: Flow<GarageActionResult>) = ResultToasts(results) { context, r ->
    when (r) {
        is GarageActionResult.Tranquility -> r.error?.resolve(context)
            ?.let { context.getString(R.string.garage_tranquility_failed, r.node, it) to true }
            ?: (context.getString(R.string.garage_tranquility_done, r.node, r.value) to false)
        is GarageActionResult.Repair -> r.error?.resolve(context)
            ?.let { context.getString(R.string.garage_repair_failed, it) to true }
            ?: (r.result!!.summary(context) to r.result.errors.isNotEmpty())
    }
}

/** "Block-refs and block-rc repairs launched; 12 resyncs retried", then any error. */
fun GarageRepairResult.summary(context: Context): String {
    val parts = outcomes.map { outcome ->
        when (outcome) {
            GarageRepairOutcome.BOTH_LAUNCHED -> context.getString(R.string.garage_repair_both)
            GarageRepairOutcome.REFS_LAUNCHED -> context.getString(R.string.garage_repair_refs)
            GarageRepairOutcome.RC_LAUNCHED -> context.getString(R.string.garage_repair_rc)
            GarageRepairOutcome.ALREADY_RUNNING -> context.getString(R.string.garage_repair_running)
            GarageRepairOutcome.UNREACHABLE -> context.getString(R.string.garage_repair_unreachable)
            GarageRepairOutcome.RETRIED -> context.resources.getQuantityString(R.plurals.garage_repair_retried, retried.toInt(), retried.toInt())
            GarageRepairOutcome.NOTHING -> context.getString(R.string.garage_repair_nothing)
        }
    }
    return (parts + errors).joinToString("\n")
}

/** Confirms a tranquility change: full speed costs disk and network IO. */
@Composable
fun TranquilityConfirmDialog(node: GarageNode, value: Long, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val full = value == GARAGE_TRANQUILITY_FULL
    ConfirmDialog(
        title = stringResource(if (full) R.string.garage_tranquility_full_title else R.string.garage_tranquility_default_title, node.label),
        text = stringResource(if (full) R.string.garage_tranquility_full_text else R.string.garage_tranquility_default_text),
        confirm = stringResource(R.string.garage_tranquility_confirm),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        confirmColor = if (full) LocalStatusColors.current.warn else Color.Unspecified,
    )
}
