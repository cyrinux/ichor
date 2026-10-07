package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.ActionBar

/**
 * A screen's app-bar actions as arranged in [bar]: its icons, then the rest behind a ⋮ menu, which
 * also leads to arranging them ([customizeLabel]). Actions not [offered] here (a role, a tab, what
 * the cluster runs) take no room; those not [enabled] show greyed out.
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
) {
    bar.icons.filter(offered).forEach { action ->
        TooltipIconButton(look.icon(action), look.label(action), onClick = { onClick(action) }, enabled = enabled(action))
    }
    var open by remember { mutableStateOf(false) }
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            val close = { open = false }
            val menu = bar.menu.filter(offered)
            menu.forEach { action ->
                MenuAction(look.icon(action), look.label(action), close, { onClick(action) }, enabled(action))
            }
            if (menu.isNotEmpty()) HorizontalDivider()
            MenuAction(Icons.Outlined.Edit, customizeLabel, close, onCustomize)
        }
    }
}

@Composable
private fun MenuAction(icon: ImageVector, label: String, close: () -> Unit, onClick: () -> Unit, enabled: Boolean = true) {
    DropdownMenuItem(
        text = { Text(label) },
        leadingIcon = { Icon(icon, contentDescription = null) },
        enabled = enabled,
        onClick = {
            close()
            onClick()
        },
    )
}
