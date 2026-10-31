package name.levis.ichor.ui.logsearch

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.design.IchorTextButton

/** Picks a label, then one of its values, to add to the query. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun LogSearchLabelsSheet(
    label: String?,
    picker: UiState<List<String>>,
    onLabel: (String) -> Unit,
    onValue: (String) -> Unit,
    onBack: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 16.dp)) {
            Text(
                if (label == null) stringResource(R.string.log_search_pick_label) else stringResource(R.string.log_search_pick_value, label),
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            if (label != null) IchorTextButton(onClick = onBack, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.log_search_labels)) }
            when (picker) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(picker.message, { if (label == null) onBack() else onLabel(label) })
                is UiState.Loaded -> if (picker.data.isEmpty()) {
                    EmptyText(stringResource(R.string.log_search_no_values))
                } else {
                    LazyColumn {
                        items(picker.data, key = { it }) { item ->
                            Text(
                                item,
                                fontFamily = FontFamily.Monospace,
                                style = MaterialTheme.typography.bodyMedium,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable(role = Role.Button) { if (label == null) onLabel(item) else onValue(item) }
                                    .padding(horizontal = 16.dp, vertical = 12.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}
