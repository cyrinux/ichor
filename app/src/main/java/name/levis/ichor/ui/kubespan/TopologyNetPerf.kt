package name.levis.ichor.ui.kubespan

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.workloads.NetPerfTab
import name.levis.ichor.ui.workloads.NetPerfViewModel

/**
 * Above the map: the chip that turns node taps into picking a pair for a network test, with
 * what to tap next; while a test runs, a chip that brings it back.
 */
@Composable
internal fun NetPerfPickBar(picking: Boolean, picked: Int, running: Boolean, onToggle: () -> Unit, onOpen: () -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
        if (running) {
            AssistChip(
                onClick = onOpen,
                label = { Text(stringResource(R.string.topology_test_running)) },
                leadingIcon = { CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp) },
            )
        } else {
            FilterChip(
                selected = picking,
                onClick = onToggle,
                label = { Text(stringResource(R.string.topology_test_pick)) },
                leadingIcon = { Icon(Icons.Filled.Speed, contentDescription = null, modifier = Modifier.size(18.dp)) },
            )
        }
        if (picking && !running) {
            MutedText(
                stringResource(if (picked == 0) R.string.topology_pick_client_hint else R.string.topology_pick_server_hint),
                modifier = Modifier.weight(1f),
            )
        }
    }
}

/** The network test over the map, its setup filled with the nodes picked there. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun NetPerfSheet(vm: NetPerfViewModel, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        NetPerfTab(vm, Modifier.padding(bottom = 16.dp))
    }
}
