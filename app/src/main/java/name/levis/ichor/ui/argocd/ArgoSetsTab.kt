package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoAppSet
import name.levis.ichor.model.ArgoProject
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.dataservices.EmptyLine
import name.levis.ichor.ui.dataservices.HealthDot
import name.levis.ichor.ui.theme.LocalStatusColors

/** ApplicationSets (worst of their apps, conditions) and AppProjects (description, sync windows). */
@Composable
fun ArgoSetsTab(status: ArgoStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        item(key = "sets-title") { SectionTitle(stringResource(R.string.argo_app_sets), Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp)) }
        if (status.appSetsError.isNotEmpty()) item(key = "sets-error") {
            InlineError(stringResource(R.string.data_services_unreadable, status.appSetsError), Modifier.padding(16.dp))
        }
        if (status.appSets.isEmpty() && status.appSetsError.isEmpty()) item(key = "sets-empty") { EmptyLine(stringResource(R.string.argo_no_app_sets)) }
        items(status.appSets.sortedBy { it.serviceHealth.ordinal }, key = { "set/${it.namespace}/${it.name}" }) { set ->
            AppSetRow(set)
            HorizontalDivider()
        }
        item(key = "projects-title") { SectionTitle(stringResource(R.string.argo_projects), Modifier.padding(start = 16.dp, end = 16.dp, top = 24.dp)) }
        if (status.projects.isEmpty()) item(key = "projects-empty") { EmptyLine(stringResource(R.string.argo_no_projects)) }
        items(status.projects, key = { "project/${it.namespace}/${it.name}" }) { project ->
            ProjectRow(project)
            HorizontalDivider()
        }
    }
}

@Composable
private fun AppSetRow(set: ArgoAppSet) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.AccountTree, contentDescription = null, tint = muted, modifier = Modifier.size(20.dp))
            Spacer(Modifier.size(12.dp))
            Text(set.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(pluralStringResource(R.plurals.argo_apps, set.apps, set.apps), style = MaterialTheme.typography.labelMedium, color = muted)
            Spacer(Modifier.size(8.dp))
            HealthDot(set.serviceHealth)
        }
        // ErrorOccurred=True is the one that matters; the others read as status lines.
        set.conditions.forEach { c ->
            val error = c.type == "ErrorOccurred" && c.status == "True"
            Text(
                "${c.type}: ${c.message}",
                style = MaterialTheme.typography.labelSmall,
                color = if (error) LocalStatusColors.current.bad else muted,
                modifier = Modifier.padding(start = 32.dp),
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun ProjectRow(project: ArgoProject) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.Folder, contentDescription = null, tint = muted, modifier = Modifier.size(20.dp))
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(project.name, style = MaterialTheme.typography.titleSmall)
            if (project.description.isNotEmpty()) {
                Text(project.description, style = MaterialTheme.typography.bodySmall, color = muted, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        if (project.syncWindows > 0) {
            Icon(Icons.Outlined.Schedule, contentDescription = null, tint = muted, modifier = Modifier.size(16.dp))
            Text(
                pluralStringResource(R.plurals.argo_sync_windows, project.syncWindows, project.syncWindows),
                style = MaterialTheme.typography.labelMedium,
                color = muted,
                modifier = Modifier.padding(start = 4.dp),
            )
        }
    }
}
