package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.JobRow
import name.levis.ichor.model.JobRunState
import name.levis.ichor.model.KubeJobs
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.filteredJobs
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.factory

/** The Jobs of a namespace (null for every one), failures first. */
class JobsViewModel(private val browser: KubeBrowserRepository) : NamespaceLoadingViewModel<KubeJobs>() {
    override suspend fun fetch() = browser.jobs(namespace)
}

/**
 * The Jobs of the namespace the Kubernetes screens list: failed ones first (with why), then
 * suspended and running ones, newest first, each with how long it ran, its completions and
 * the CronJob that started it. A tap opens the Job's summary ([onObject]), its CronJob chip
 * the CronJob's; Delete confirms through the object screen's generic delete.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JobsScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: JobsViewModel = viewModel(factory = factory { JobsViewModel(app.kubeBrowser) }),
) {
    val control = rememberListScope(vm)
    var deleting by remember { mutableStateOf<KubeObjectRef?>(null) }

    deleting?.let { ref ->
        JobDelete(ref) { deleted ->
            deleting = null
            if (deleted) vm.refresh()
        }
    }

    KubeScopedList(
        title = stringResource(R.string.kube_jobs_title),
        vm = vm,
        control = control,
        onBack = onBack,
        items = KubeJobs::jobs,
        namespaceOf = JobRow::namespace,
        filter = { rows, query -> rows.filteredJobs(query) },
        key = JobRow::key,
        search = R.string.kube_jobs_search,
        empty = R.string.kube_jobs_empty,
        noMatch = R.string.kube_jobs_no_match,
    ) { job ->
        JobRowItem(
            job,
            onClick = { onObject(job.ref) },
            onOwner = onObject,
            onDelete = { deleting = job.ref },
        )
    }
}

/**
 * The object screen's delete for [ref], from the list: its preview, propagation and refusal.
 * [onDone] tells whether the Job was deleted.
 */
@Composable
private fun JobDelete(ref: KubeObjectRef, onDone: (deleted: Boolean) -> Unit) {
    val vm: KubeObjectViewModel = viewModel(
        key = "job-delete-${ref.namespace}/${ref.name}",
        factory = factory { KubeObjectViewModel(app.kubeBrowser, ref) },
    )
    val deletion by vm.delete.collectAsStateWithLifecycle()
    val denial = rememberKubeCanDenial("delete", ref.group, ref.resource, ref.namespace, ref.name)
    LaunchedEffect(vm) { vm.startDelete() }
    LaunchedEffect(vm) { vm.deleted.collect { onDone(true) } }
    deletion?.let { d ->
        KubeObjectDeleteDialog(
            ref,
            d,
            denial,
            onPropagation = vm::choosePropagation,
            onConfirm = vm::confirmDelete,
            onRetry = vm::loadDeletePreview,
            onDismiss = {
                vm.cancelDelete()
                onDone(false)
            },
        )
    }
}

@Composable
private fun JobRow.stateLabel(): String = when (runState) {
    null -> stringResource(R.string.cronjobs_suspended)
    JobRunState.RUNNING -> stringResource(R.string.cronjobs_state_running)
    JobRunState.SUCCEEDED -> stringResource(R.string.cronjobs_state_succeeded)
    JobRunState.FAILED -> stringResource(R.string.cronjobs_state_failed)
    JobRunState.NEVER -> state
}

@Composable
private fun JobRowItem(job: JobRow, onClick: () -> Unit, onOwner: (KubeObjectRef) -> Unit, onDelete: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(start = 16.dp, top = 10.dp, bottom = 10.dp, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    job.name,
                    style = MaterialTheme.typography.bodyMedium,
                    fontFamily = FontFamily.Monospace,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(job.stateLabel(), style = MaterialTheme.typography.labelMedium, color = job.level.color())
            }
            val facts = listOfNotNull(
                job.durationSeconds?.let { localizedDuration(it) },
                stringResource(R.string.kube_jobs_completions, job.completions).takeIf { job.completions.isNotEmpty() },
                stringResource(R.string.cronjobs_manual).takeIf { job.manual },
            )
            if (facts.isNotEmpty()) MutedText(facts.joinToString("  ·  "))
            if (job.reason.isNotEmpty()) {
                Text(job.reason, style = MaterialTheme.typography.labelMedium, color = job.level.color())
            }
            job.ownerRef?.let { owner ->
                AssistChip(onClick = { onOwner(owner) }, label = { Text(stringResource(R.string.kube_jobs_owner, job.owner)) })
            }
        }
        TooltipIconButton(Icons.Outlined.Delete, stringResource(R.string.kb_delete), onClick = onDelete)
    }
}
