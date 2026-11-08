package name.levis.ichor.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.data.UiStyle
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorTextButton

/** The design language that draws cards, bars and buttons, independent of the colour theme. */
@Composable
internal fun UiStyleSetting(prefs: UiPreferences) {
    val current by prefs.uiStyle.collectAsStateWithLifecycle()
    var picking by remember { mutableStateOf(false) }

    IchorCard(Modifier.fillMaxWidth().clickable(role = Role.Button) { picking = true }) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.settings_ui_style), style = MaterialTheme.typography.titleMedium)
            MutedText(stringResource(R.string.settings_ui_style_desc))
            MutedText(stringResource(current.label))
        }
    }

    if (picking) {
        IchorAlertDialog(
            onDismissRequest = { picking = false },
            title = { Text(stringResource(R.string.settings_ui_style)) },
            text = {
                Column(Modifier.selectableGroup()) {
                    UiStyle.entries.forEach { style ->
                        Row(
                            Modifier.fillMaxWidth()
                                .selectable(selected = style == current, role = Role.RadioButton, onClick = {
                                    picking = false
                                    prefs.setUiStyle(style)
                                })
                                .padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = style == current, onClick = null)
                            Text(stringResource(style.label), modifier = Modifier.padding(start = 12.dp))
                        }
                    }
                }
            },
            confirmButton = {
                IchorTextButton(onClick = { picking = false }) { Text(stringResource(R.string.common_cancel)) }
            },
        )
    }
}
