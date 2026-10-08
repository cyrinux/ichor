package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CastAIStatus

/** The two things CAST AI does that the tab shows: right-sizing workloads, and consolidating nodes. */
private enum class CastAIPane { WORKLOADS, NODES }

/**
 * CAST AI: the Workload Autoscaler's recommendations and, on clusters where CAST AI drives
 * Karpenter, its node consolidations. Without any consolidation the switch is left out.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CastAITab(status: CastAIStatus) {
    var pane by rememberSaveable { mutableStateOf(CastAIPane.WORKLOADS) }
    if (status.plans.isEmpty() && status.plansError.isEmpty()) {
        CastAIWorkloads(status)
        return
    }
    Column(Modifier.fillMaxSize()) {
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp)) {
            CastAIPane.entries.forEachIndexed { i, p ->
                SegmentedButton(
                    selected = p == pane,
                    onClick = { pane = p },
                    shape = SegmentedButtonDefaults.itemShape(i, CastAIPane.entries.size),
                ) {
                    Text(
                        when (p) {
                            CastAIPane.WORKLOADS -> stringResource(R.string.castai_pane_workloads, status.recommendations.size)
                            CastAIPane.NODES -> stringResource(R.string.castai_pane_nodes, status.plans.size)
                        },
                        maxLines = 1,
                    )
                }
            }
        }
        when (pane) {
            CastAIPane.WORKLOADS -> CastAIWorkloads(status)
            CastAIPane.NODES -> CastAIPlans(status)
        }
    }
}
