package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ActionBar

/**
 * The sections a screen's menu reads in: each action's section ([label], a string resource) and
 * the menu split into them ([sections], in section order, the saved order kept inside each).
 */
class ActionGroups<A>(val label: (A) -> Int, val sections: (List<A>) -> List<Pair<Int, List<A>>>)

/**
 * A screen's app-bar actions as arranged in [bar]: its icons, then the rest behind a ⋮ menu, which
 * also leads to arranging them ([customizeLabel]). Actions not [offered] here (a role, a tab, what
 * the cluster runs) take no room; those not [enabled] show greyed out, and those with a
 * [refusal] (the credentials may not run it) too, the reason under their label in the menu.
 * With [groups], the menu reads in titled sections; without, it is one list.
 */
@Composable
fun <A : Enum<A>> ActionBarActions(
    bar: ActionBar<A>,
    look: ActionLook<A>,
    onClick: (A) -> Unit,
    customizeLabel: String,
    onCustomize: () -> Unit,
    offered: (A) -> Boolean = { true },
    enabled: (A) -> Boolean = { true },
    refusal: (A) -> String? = { null },
    /** Marks where an action is (its icon, else the ⋮ button its menu is behind), for a coach mark. */
    marker: (A) -> Modifier = { Modifier },
    groups: ActionGroups<A>? = null,
) {
    bar.icons.filter(offered).forEach { action ->
        TooltipIconButton(look.icon(action), look.label(action), onClick = { onClick(action) }, modifier = marker(action), enabled = enabled(action) && refusal(action) == null)
    }
    var open by remember { mutableStateOf(false) }
    val inMenu = bar.menu.filter(offered).fold<A, Modifier>(Modifier) { acc, action -> acc.then(marker(action)) }
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true }, modifier = inMenu)
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            val close = { open = false }
            val menu = bar.menu.filter(offered)
            val item: @Composable (A) -> Unit = { action ->
                val refused = refusal(action)
                MenuAction(look.icon(action), look.label(action), close, { onClick(action) }, enabled(action) && refused == null, refused)
            }
            if (groups == null) {
                menu.forEach { item(it) }
            } else {
                groups.sections(menu).forEachIndexed { index, (label, actions) ->
                    if (index > 0) HorizontalDivider()
                    MenuSectionHeader(stringResource(label))
                    actions.forEach { item(it) }
                }
            }
            if (menu.isNotEmpty()) HorizontalDivider()
            MenuAction(Icons.Outlined.Edit, customizeLabel, close, onCustomize)
        }
    }
}

/** A section's title in a menu: small, muted, not an action (a heading for TalkBack). */
@Composable
private fun MenuSectionHeader(title: String) {
    Text(
        title,
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 4.dp).semantics { heading() },
    )
}

@Composable
private fun MenuAction(icon: ImageVector, label: String, close: () -> Unit, onClick: () -> Unit, enabled: Boolean = true, reason: String? = null) {
    DropdownMenuItem(
        text = {
            if (reason == null) {
                Text(label)
            } else {
                Column {
                    Text(label)
                    Text(reason, style = MaterialTheme.typography.labelSmall)
                }
            }
        },
        leadingIcon = { Icon(icon, contentDescription = null) },
        enabled = enabled,
        onClick = {
            close()
            onClick()
        },
    )
}
