package name.levis.ichor.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import name.levis.ichor.R

/**
 * A row that shows or hides its details on tap: TalkBack hears it as a button that expands
 * or collapses ([actionLabel] when the generic words say too little), and whether it is
 * expanded now. A disabled row has neither.
 */
@Composable
fun Modifier.expandable(expanded: Boolean, enabled: Boolean = true, actionLabel: String? = null, onToggle: () -> Unit): Modifier {
    val state = stringResource(if (expanded) R.string.common_expanded else R.string.common_collapsed)
    val action = actionLabel ?: stringResource(if (expanded) R.string.common_collapse else R.string.common_expand)
    return clickable(enabled = enabled, role = Role.Button, onClickLabel = action, onClick = onToggle)
        .then(if (enabled) Modifier.semantics { stateDescription = state } else Modifier)
}
