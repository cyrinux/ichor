package name.levis.ichor.ui.crossplane

import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.CrossplaneAction
import name.levis.ichor.model.CrossplaneObject
import name.levis.ichor.model.actionKey
import name.levis.ichor.model.actions
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.uiText

/** Outcome of [action] on the object [label], shown once. */
data class CrossplaneActionResult(val label: String, val action: CrossplaneAction, val error: UiText?)

/** Pause, resume and reconcile requests, run in [scope] (a ViewModel's); [onChanged] reads the screen again after one. */
class CrossplaneActions(
    scope: CoroutineScope,
    private val browser: KubeBrowserRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<CrossplaneActionResult>(scope)
    /** [actionKey]s of the objects with a request in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<CrossplaneActionResult> get() = actions.results

    fun run(o: CrossplaneObject, action: CrossplaneAction) {
        actions.launch(o.actionKey, { browser.crossplaneAction(o.ref(), action) }, { CrossplaneActionResult(o.label, action, it.exceptionOrNull()?.uiText()) }) {
            onChanged()
        }
    }
}

@Composable
private fun actionLabel(action: CrossplaneAction): String = stringResource(
    when (action) {
        CrossplaneAction.PAUSE -> R.string.crossplane_pause
        CrossplaneAction.RESUME -> R.string.crossplane_resume
        CrossplaneAction.RECONCILE -> R.string.crossplane_reconcile
    },
)

@Composable
fun CrossplaneActionToasts(results: Flow<CrossplaneActionResult>) = ResultToasts(results) { context, r ->
    val label = context.getString(
        when (r.action) {
            CrossplaneAction.PAUSE -> R.string.crossplane_pause
            CrossplaneAction.RESUME -> R.string.crossplane_resume
            CrossplaneAction.RECONCILE -> R.string.crossplane_reconcile
        },
    )
    val message = r.error?.resolve(context)?.let { context.getString(R.string.crossplane_action_failed, label, r.label, it) }
        ?: context.getString(R.string.crossplane_action_done, r.label, label)
    message to (r.error != null)
}

/**
 * The row's overflow menu: Pause or Resume (whichever applies) and Reconcile now. Whether the
 * credentials may patch the object is asked when the menu opens (not for every row), and a
 * refusal disables the items with the reason.
 */
@Composable
fun CrossplaneActionsMenu(o: CrossplaneObject, busy: Boolean, onPick: (CrossplaneAction) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { open = true }, enabled = !busy) {
            Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.crossplane_actions, o.label))
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            val denial = rememberKubeCanDenial("patch", o.group, o.resource, o.namespace, o.name)
            o.actions().forEach { action ->
                DropdownMenuItem(
                    text = { Text(actionLabel(action)) },
                    enabled = denial == null,
                    onClick = { open = false; onPick(action) },
                )
            }
            if (denial != null) DropdownMenuItem(text = { KubeDenialNote(denial) }, onClick = {}, enabled = false)
        }
    }
}

/** What [action] does to [o], to confirm: a pause also says it blocks deletion and, on a composite, not its parts. */
@Composable
fun CrossplaneConfirm(o: CrossplaneObject, action: CrossplaneAction, composite: Boolean, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val (title, text) = when (action) {
        CrossplaneAction.PAUSE -> stringResource(R.string.crossplane_pause_title, o.label) to
            listOfNotNull(
                stringResource(R.string.crossplane_pause_text),
                stringResource(R.string.crossplane_pause_composite).takeIf { composite },
            ).joinToString("\n\n")
        CrossplaneAction.RESUME -> stringResource(R.string.crossplane_resume_title, o.label) to stringResource(R.string.crossplane_resume_text)
        CrossplaneAction.RECONCILE -> stringResource(R.string.crossplane_reconcile_title, o.label) to stringResource(R.string.crossplane_reconcile_text)
    }
    ConfirmDialog(title = title, text = text, confirm = actionLabel(action), onConfirm = onConfirm, onDismiss = onDismiss)
}
