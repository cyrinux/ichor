package name.levis.ichor.ui.integrations

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.IntegrationFamily
import name.levis.ichor.model.IntegrationGroup
import name.levis.ichor.model.IntegrationReport
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.ui.components.pageContent

/**
 * Request an integration: the operators this cluster runs that Ichor does not show yet
 * (os:admin). The groups the user ticks go into a GitHub issue they review before sending;
 * only group, version and kind names, never namespaces or object names.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun IntegrationsScreen(onBack: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: IntegrationsViewModel = viewModel(factory = factory { IntegrationsViewModel(app.talosRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val picked by vm.picked.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()

    LaunchedEffect(config?.activeContext, generation) { vm.load(config?.activeContext to generation) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.integrations_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            Loaded(state, vm::refresh) { report -> Report(report, picked, vm) }
        }
    }
}

@Composable
private fun Report(report: IntegrationReport, picked: Set<String>, vm: IntegrationsViewModel) {
    val context = LocalContext.current
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item { MutedText(stringResource(R.string.integrations_intro)) }
        if (report.families.isEmpty()) item { EmptyText(stringResource(R.string.integrations_none)) }
        items(report.families, key = { it.id }) { family ->
            FamilyCard(
                family,
                picked,
                onToggle = vm::toggle,
                onSearch = { openUrl(context, vm.searchUrl(family)) },
                onRequest = { openUrl(context, vm.issueUrl(family)) },
            )
        }
        if (report.supported.isNotEmpty()) {
            item { MutedText(stringResource(R.string.integrations_supported, report.supported.joinToString(", "))) }
        }
        item {
            OutlinedButton(onClick = { openUrl(context, INTEGRATION_FORM_URL) }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.integrations_other))
            }
        }
    }
}

@Composable
private fun FamilyCard(
    family: IntegrationFamily,
    picked: Set<String>,
    onToggle: (String) -> Unit,
    onSearch: () -> Unit,
    onRequest: () -> Unit,
) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 12.dp)) {
            Text(family.id, style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(horizontal = 16.dp))
            family.groups.forEach { group -> GroupRow(group, group.name in picked) { onToggle(group.name) } }
            Row(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
            ) {
                OutlinedButton(onClick = onSearch) { Text(stringResource(R.string.integrations_search)) }
                Button(onClick = onRequest, enabled = family.groups.any { it.name in picked }) {
                    Text(stringResource(R.string.integrations_request))
                }
            }
        }
    }
}

@Composable
private fun GroupRow(group: IntegrationGroup, checked: Boolean, onToggle: () -> Unit) {
    Row(
        Modifier.fillMaxWidth()
            .toggleable(value = checked, role = Role.Checkbox, onValueChange = { onToggle() })
            .padding(horizontal = 4.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = null, modifier = Modifier.padding(12.dp))
        Column(Modifier.weight(1f).padding(end = 16.dp)) {
            Text(group.label, style = MaterialTheme.typography.bodyMedium)
            if (group.kinds.isNotEmpty()) MutedText(group.kinds.joinToString(", "))
        }
    }
}
