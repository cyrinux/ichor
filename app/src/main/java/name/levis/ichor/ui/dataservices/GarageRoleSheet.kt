package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.InputChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.GARAGE_MIN_CAPACITY
import name.levis.ichor.model.GarageCapacityUnit
import name.levis.ichor.model.GarageLayoutNode
import name.levis.ichor.model.garageCapacityBytes
import name.levis.ichor.model.garageCapacityText
import name.levis.ichor.model.garageSuggestedCapacity
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.util.formatBytes

/**
 * The role editor: the zone (the cluster's zones as suggestions), storage with a capacity or
 * gateway, and tags. Prefilled from the node's current or staged role, or, for a node joining,
 * from its disk and its Kubernetes node. Staging changes nothing until the layout is applied.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GarageRoleSheet(state: GarageRoleEditState, onStage: (zone: String, capacity: Long, gateway: Boolean, tags: List<String>) -> Unit, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val node = state.node
            Text(
                when {
                    node?.effectiveRole != null -> stringResource(R.string.garage_role_change_title, node.label)
                    node != null -> stringResource(R.string.garage_role_add_title, node.label)
                    else -> stringResource(R.string.garage_role_add_title, state.nodeId.take(16))
                },
                style = MaterialTheme.typography.titleMedium,
            )
            when {
                state.loading -> LinearProgressIndicator(Modifier.fillMaxWidth())
                state.error != null -> InlineError(state.error.asString())
                node == null -> InlineError(stringResource(R.string.garage_role_node_gone))
                else -> RoleForm(node, state.layout?.zones.orEmpty(), state.saving, onStage)
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RoleForm(node: GarageLayoutNode, zones: List<String>, saving: Boolean, onStage: (String, Long, Boolean, List<String>) -> Unit) {
    val current = node.effectiveRole
    val suggested = remember(node.id) { garageSuggestedCapacity(node.dataTotal) }
    val initialBytes = current?.takeIf { !it.gateway }?.capacity ?: suggested
    var zone by remember(node.id) { mutableStateOf(current?.zone ?: zones.singleOrNull().orEmpty()) }
    var gateway by remember(node.id) { mutableStateOf(current?.gateway == true) }
    var unit by remember(node.id) { mutableStateOf(GarageCapacityUnit.forBytes(initialBytes.coerceAtLeast(1))) }
    var capacityText by remember(node.id) { mutableStateOf(if (initialBytes > 0) garageCapacityText(initialBytes, unit) else "") }
    var tags by remember(node.id) { mutableStateOf(current?.tags ?: listOfNotNull(node.kubeNode.takeIf { it.isNotEmpty() })) }
    var newTag by remember(node.id) { mutableStateOf("") }

    val zoneValid = zone.trim().isNotEmpty() && zone.trim().length <= 64
    val capacity = if (gateway) 0L else garageCapacityBytes(capacityText, unit)
    val capacityValid = gateway || (capacity != null && capacity >= GARAGE_MIN_CAPACITY)
    val changed = current == null || zone.trim() != current.zone || gateway != current.gateway || (!gateway && capacity != current.capacity) || tags != current.tags
    val canStage = zoneValid && capacityValid && changed && !saving

    fun addTag() {
        val tag = newTag.trim()
        if (tag.isNotEmpty() && tag !in tags) tags = tags + tag
        newTag = ""
    }

    SectionTitle(stringResource(R.string.garage_role_zone))
    OutlinedTextField(
        zone, { zone = it }, Modifier.fillMaxWidth(),
        singleLine = true, isError = !zoneValid,
        supportingText = { Text(stringResource(if (zoneValid) R.string.garage_role_zone_hint else R.string.garage_role_zone_required)) },
    )
    if (zones.isNotEmpty()) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            zones.forEach { z -> FilterChip(selected = zone.trim() == z, onClick = { zone = z }, label = { Text(z) }) }
        }
    }

    SectionTitle(stringResource(R.string.garage_role_kind))
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        FilterChip(selected = !gateway, onClick = { gateway = false }, label = { Text(stringResource(R.string.garage_role_storage)) })
        FilterChip(selected = gateway, onClick = { gateway = true }, label = { Text(stringResource(R.string.garage_role_gateway)) })
    }
    if (gateway) {
        InfoNotice(stringResource(R.string.garage_role_gateway_text))
    } else {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                capacityText, { capacityText = it }, Modifier.weight(1f),
                label = { Text(stringResource(R.string.garage_role_capacity)) },
                singleLine = true, isError = !capacityValid,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                supportingText = {
                    Text(
                        when {
                            !capacityValid -> stringResource(R.string.garage_role_capacity_invalid)
                            node.dataTotal > 0 -> stringResource(R.string.garage_role_capacity_disk, formatBytes(node.dataTotal))
                            else -> stringResource(R.string.garage_role_capacity_hint)
                        },
                    )
                },
            )
            GarageCapacityUnit.entries.forEach { u ->
                FilterChip(selected = unit == u, onClick = {
                    // Keep the same bytes when the unit changes.
                    capacity?.let { capacityText = garageCapacityText(it, u) }
                    unit = u
                }, label = { Text(u.label) })
            }
        }
    }

    SectionTitle(stringResource(R.string.garage_role_tags))
    if (tags.isNotEmpty()) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            tags.forEach { tag ->
                InputChip(
                    selected = false,
                    onClick = { tags = tags - tag },
                    label = { Text(tag) },
                    trailingIcon = { Icon(Icons.Outlined.Close, stringResource(R.string.garage_role_tag_remove, tag), Modifier.size(16.dp)) },
                )
            }
        }
    }
    OutlinedTextField(
        newTag, { newTag = it }, Modifier.fillMaxWidth(),
        label = { Text(stringResource(R.string.garage_role_tag_add)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { addTag() }),
        trailingIcon = if (newTag.isNotBlank()) ({ OutlinedButton(onClick = ::addTag) { Text(stringResource(R.string.garage_role_tag_add_button)) } }) else null,
    )

    Row(Modifier.fillMaxWidth().padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp, Alignment.End)) {
        if (saving) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        Button(onClick = { onStage(zone.trim(), capacity ?: 0L, gateway, tags) }, enabled = canStage) {
            Text(stringResource(R.string.garage_layout_stage))
        }
    }
    InfoNotice(stringResource(R.string.garage_role_stage_text))
}
