package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AWSImageCost
import name.levis.ichor.model.AWSImageSpec
import name.levis.ichor.model.AWSOptions
import name.levis.ichor.model.helperSubnets
import name.levis.ichor.model.securityGroupsIn
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

private val amiArchitectures = listOf("amd64", "arm64")

/**
 * "Make a Talos AMI": the architecture (that of the instance type asked for, editable), the
 * helper's subnet and security group (the card's picks, editable) and the cost line, then a plain
 * confirmation: nothing of the cluster is touched and the helper is terminated afterwards.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AWSImageSheet(
    options: AWSOptions,
    initialArch: String,
    initialSubnet: String,
    initialGroup: String,
    template: String,
    estimate: suspend (arch: String, region: String) -> AWSImageCost,
    onStart: (AWSImageSpec) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    var arch by remember { mutableStateOf(initialArch.ifBlank { amiArchitectures.first() }) }
    var subnetId by remember { mutableStateOf(initialSubnet) }
    var groupId by remember { mutableStateOf(initialGroup) }
    var cost by remember { mutableStateOf<UiState<AWSImageCost>?>(null) }
    var retry by remember { mutableStateOf(0) }
    val subnets = options.helperSubnets()
    val subnet = subnets.firstOrNull { it.id == subnetId }
    val groups = subnet?.let { options.securityGroupsIn(it.vpcId) }.orEmpty()
    val group = groupId.takeIf { id -> groups.any { it.id == id } }.orEmpty()

    LaunchedEffect(arch, retry) {
        cost = UiState.Loading
        cost = uiStateOf { estimate(arch, options.region) }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.aws_make_ami)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MutedText(stringResource(R.string.aws_image_intro))
                Text(stringResource(R.string.hetzner_snapshot_arch), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    amiArchitectures.forEach { a -> IchorFilterChip(selected = a == arch, onClick = { arch = a }, label = { Text(a) }) }
                }
                Text(stringResource(R.string.aws_image_subnet), style = MaterialTheme.typography.labelLarge)
                MutedText(stringResource(R.string.aws_image_subnet_hint))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    subnets.forEach { s ->
                        IchorFilterChip(selected = s.id == subnetId, onClick = { subnetId = s.id }, label = { Text(subnetLabel(s)) })
                    }
                }
                if (subnet != null) {
                    Text(stringResource(R.string.aws_image_security_group), style = MaterialTheme.typography.labelLarge)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        IchorFilterChip(selected = group.isEmpty(), onClick = { groupId = "" }, label = { Text(stringResource(R.string.aws_image_security_group_default)) })
                        groups.forEach { g ->
                            IchorFilterChip(selected = g.id == group, onClick = { groupId = g.id }, label = { Text(g.name.ifBlank { g.id }) })
                        }
                    }
                }
                when (val state = cost) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, { retry++ })
                    is UiState.Loaded -> Text(costLine(state.data), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            IchorButton(
                onClick = {
                    onStart(AWSImageSpec(templateNode = template, arch = arch, region = options.region, subnetId = subnetId, securityGroupId = group))
                },
                enabled = cost is UiState.Loaded && subnet != null,
            ) { Text(stringResource(R.string.aws_image_confirm)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** "About 10 minutes of t3.small ($0.02/h), then $0.05 per GB-month", or the same without the prices. */
@Composable
private fun costLine(cost: AWSImageCost): String =
    if (cost.hourly.isBlank() || cost.snapshotMonthlyPerGB.isBlank()) {
        stringResource(R.string.aws_image_cost_unknown, cost.minutes, cost.instanceType)
    } else {
        stringResource(
            R.string.aws_image_cost,
            cost.minutes,
            cost.instanceType,
            "${cost.hourly} ${cost.currency}",
            "${cost.snapshotMonthlyPerGB} ${cost.currency}",
        )
    }
