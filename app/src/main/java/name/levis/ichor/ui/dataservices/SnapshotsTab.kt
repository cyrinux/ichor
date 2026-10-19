package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.SnapshotClaim
import name.levis.ichor.model.SnapshotClass
import name.levis.ichor.model.SnapshotContent
import name.levis.ichor.model.VolumeSnapshot
import name.levis.ichor.model.VolumeSnapshots
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.cardGroupTitle
import name.levis.ichor.ui.components.cardRows
import name.levis.ichor.util.timeAgo

private val rowPadding = Modifier.padding(horizontal = 16.dp)

/** The VolumeSnapshots, then the claims watched for a recent one, the classes and the contents. */
@Composable
fun SnapshotsTab(status: VolumeSnapshots) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
        items(listOf(status.error, status.contentsError, status.classesError, status.claimsError).filter { it.isNotEmpty() }) {
            InlineError(stringResource(R.string.data_services_unreadable, it), Modifier.padding(16.dp))
        }
        if (status.snapshots.isEmpty() && status.error.isEmpty()) {
            item { EmptyState(Icons.Outlined.PhotoCamera, stringResource(R.string.snapshot_empty)) }
        }
        cardRows(status.snapshots, key = { it.key }) { SnapshotRow(it) }
        section(R.string.snapshot_section_claims, status.claims.filter { it.watched }, { "claim|${it.namespace}/${it.name}" }) { ClaimRow(it) }
        section(R.string.snapshot_section_classes, status.classes, { "class|${it.name}" }) { ClassRow(it) }
        section(R.string.snapshot_section_contents, status.contents, { "content|${it.name}" }) { ContentRow(it) }
    }
}

private fun <T> LazyListScope.section(title: Int, rows: List<T>, key: (T) -> String, row: @Composable (T) -> Unit) {
    if (rows.isEmpty()) return
    cardGroupTitle("title|$title", title)
    cardRows(rows, key = key, content = row)
}

/** Critical with an error, a warning while not ready yet. */
private fun snapshotHealth(ready: Boolean, error: String): ServiceHealth = when {
    error.isNotEmpty() -> ServiceHealth.CRITICAL
    !ready -> ServiceHealth.WARNING
    else -> ServiceHealth.OK
}

@Composable
private fun SnapshotRow(s: VolumeSnapshot) {
    val pending = stringResource(R.string.snapshot_pending).takeIf { !s.ready && s.error.isEmpty() }
    ResourceRow(
        snapshotHealth(s.ready, s.error),
        s.key,
        rowPadding,
        subtitle = listOfNotNull(s.pvc, s.className, timeAgo(s.createdAt), pending).filter { it.isNotEmpty() }.joinToString(" · ").ifEmpty { null },
    ) { ErrorLine(s.error) }
}

@Composable
private fun ClaimRow(c: SnapshotClaim) {
    val subtitle = if (c.lastGoodAt > 0) {
        listOfNotNull(stringResource(R.string.snapshot_last_good, timeAgo(c.lastGoodAt)), stringResource(R.string.snapshot_stale).takeIf { c.stale })
    } else {
        listOf(stringResource(R.string.snapshot_stale))
    }
    ResourceRow(if (c.stale) ServiceHealth.WARNING else ServiceHealth.OK, "${c.namespace}/${c.name}", rowPadding, subtitle = subtitle.joinToString(" · "))
}

@Composable
private fun ClassRow(c: SnapshotClass) {
    ResourceRow(
        null,
        c.name,
        rowPadding,
        subtitle = c.driver.ifEmpty { null },
        trailing = {
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                if (c.default) Tag(stringResource(R.string.snapshot_default))
                if (c.deletionPolicy.isNotEmpty()) Tag(c.deletionPolicy)
            }
        },
    )
}

@Composable
private fun ContentRow(c: SnapshotContent) {
    val pending = stringResource(R.string.snapshot_pending).takeIf { !c.ready && c.error.isEmpty() }
    val source = if (c.snapshot.isNotEmpty()) "${c.namespace}/${c.snapshot}" else ""
    ResourceRow(
        snapshotHealth(c.ready, c.error),
        c.name,
        rowPadding,
        subtitle = listOfNotNull(source, c.className, timeAgo(c.createdAt), pending).filter { it.isNotEmpty() }.joinToString(" · ").ifEmpty { null },
    ) { ErrorLine(c.error) }
}

@Composable
private fun ErrorLine(error: String) {
    if (error.isEmpty()) return
    Text(error, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
}
