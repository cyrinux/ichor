package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.BackupJobStatus
import name.levis.ichor.model.VolumeSnapshots
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.util.timeAgo

@Composable
fun SnapshotsTab(status: VolumeSnapshots) {
    LazyColumn {
        items(listOf(status.error, status.contentsError, status.classesError, status.claimsError).filter { it.isNotEmpty() }) { MutedText(it, Modifier.padding(16.dp)) }
        items(status.snapshots, key = { it.key }) { s ->
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(s.key)
                Text(stringResource(if (s.ready) R.string.snapshot_ready else R.string.snapshot_pending))
                MutedText(stringResource(R.string.snapshot_source, s.pvc, s.className))
                MutedText(stringResource(R.string.snapshot_created, timeAgo(s.createdAt)))
                if (s.error.isNotEmpty()) MutedText(s.error)
            }
            HorizontalDivider()
        }
        items(status.claims.filter { it.watched }, key = { it.namespace + "/" + it.name }) { c ->
            Column(Modifier.padding(16.dp)) {
                Text(c.namespace + "/" + c.name)
                MutedText(stringResource(R.string.snapshot_last_good, timeAgo(c.lastGoodAt)))
                if (c.stale) Text(stringResource(R.string.snapshot_stale))
            }
        }
        items(status.classes, key = { it.name }) { c ->
            Column(Modifier.padding(16.dp)) {
                Text(c.name)
                MutedText(c.driver + " · " + c.deletionPolicy)
                if (c.default) MutedText(stringResource(R.string.snapshot_default))
            }
        }
        items(status.contents, key = { it.name }) { c ->
            Column(Modifier.padding(16.dp)) {
                Text(c.name)
                MutedText(c.namespace + "/" + c.snapshot)
                MutedText(c.className + " · " + c.driver)
                MutedText(stringResource(R.string.snapshot_created, timeAgo(c.createdAt)))
                MutedText(stringResource(if (c.ready) R.string.snapshot_ready else R.string.snapshot_pending))
                if (c.error.isNotEmpty()) MutedText(c.error)
            }
        }
        if (status.snapshots.isEmpty() && status.error.isEmpty()) item { MutedText(stringResource(R.string.snapshot_empty), Modifier.padding(16.dp)) }
    }
}

@Composable
fun BackupJobsTab(status: BackupJobStatus) {
    LazyColumn {
        items(listOf(status.error, status.jobsError).filter { it.isNotEmpty() }) { MutedText(it, Modifier.padding(16.dp)) }
        items(status.jobs, key = { it.namespace + "/" + it.name }) { j ->
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(j.namespace + "/" + j.name)
                MutedText(j.schedule)
                MutedText(stringResource(R.string.backup_last_success, timeAgo(j.lastSuccess)))
                Text(stringResource(R.string.backup_failure_streak, j.failureStreak))
                if (j.suspended) MutedText(stringResource(R.string.backup_suspended))
            }
            HorizontalDivider()
        }
    }
}
