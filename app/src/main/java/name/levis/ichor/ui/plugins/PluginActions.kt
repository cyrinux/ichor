package name.levis.ichor.ui.plugins

import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.R
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.model.PluginAction
import name.levis.ichor.model.PluginActionFailure
import name.levis.ichor.model.PluginRef
import name.levis.ichor.model.displayName
import name.levis.ichor.model.paramLabel
import name.levis.ichor.model.pluginActionFailure
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.uiText

/** The in-flight key of [actionId] on the object [ref]. */
fun pluginActionKey(ref: PluginRef, actionId: String): String = "${ref.key}|$actionId"

/**
 * Outcome of the action [label] on [target] ("namespace/name"), shown once: [failure] null
 * when it went through; [error] is the Go error as the app shows any other one.
 */
data class PluginActionOutcome(val label: String, val target: String, val failure: PluginActionFailure?, val error: UiText?)

/**
 * Runs plugin [pluginId]'s actions in [scope] (the tab's ViewModel's), off the main thread
 * (GoCall runs on IO). [onChanged] reloads the tab: after an action went through, and after
 * one refused because the object is not as the tab showed it (changed since read, or the
 * action no longer applies to it).
 */
class PluginActions(
    private val scope: CoroutineScope,
    private val plugins: PluginRepository,
    private val pluginId: String,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<PluginActionOutcome>(scope)

    /** [pluginActionKey]s of the actions in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<PluginActionOutcome> get() = actions.results

    /** Runs [action] on [ref] with [paramsJson] (see [name.levis.ichor.model.encodeParams]). */
    fun run(ref: PluginRef, action: PluginAction, paramsJson: String) {
        val target = ref.displayName
        actions.launch(
            pluginActionKey(ref, action.id),
            { plugins.action(pluginId, action.id, ref.namespace, ref.name, paramsJson) },
            { outcome ->
                val e = outcome.exceptionOrNull()
                val failure = e?.let { pluginActionFailure(it.message.orEmpty()) }?.let { f ->
                    // The toast names the param as the form labelled it.
                    if (f is PluginActionFailure.Param) f.copy(param = action.paramLabel(f.param)) else f
                }
                // The tab shows the object as it was: read it again so that a retry sees it now.
                if (failure == PluginActionFailure.Changed || failure is PluginActionFailure.Disabled) onChanged()
                PluginActionOutcome(action.label, target, failure, e?.uiText())
            },
            onChanged,
        )
    }
}

/** What a row's buttons need: the actions in flight and how to run one. */
@Immutable
class PluginActionRunner(val busy: Set<String>, val run: (ref: PluginRef, action: PluginAction, paramsJson: String) -> Unit)

/** A toast for each outcome of [results]: done, or why it was refused. */
@Composable
fun PluginActionToasts(results: Flow<PluginActionOutcome>) = ResultToasts(results) { context, r ->
    val text = when (val f = r.failure) {
        null -> context.getString(R.string.plugin_action_done, r.label, r.target)
        is PluginActionFailure.Disabled -> context.getString(R.string.plugin_action_refused, r.label, r.target, f.reason)
        PluginActionFailure.Changed -> context.getString(R.string.plugin_action_changed, r.target)
        is PluginActionFailure.Param -> context.getString(R.string.plugin_action_param_refused, r.label, f.param, f.message)
        PluginActionFailure.Demo -> context.getString(R.string.plugin_action_demo)
        is PluginActionFailure.Forbidden -> context.getString(R.string.plugin_action_forbidden, r.label, r.target)
        is PluginActionFailure.Other -> context.getString(R.string.plugin_action_failed, r.label, r.target, r.error?.resolve(context) ?: f.message)
    }
    text to (r.failure != null)
}
