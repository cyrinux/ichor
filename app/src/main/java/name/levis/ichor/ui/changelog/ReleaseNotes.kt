package name.levis.ichor.ui.changelog

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ChangelogKind
import name.levis.ichor.model.ChangelogRelease
import name.levis.ichor.model.ChangelogSection
import name.levis.ichor.model.knownKind
import name.levis.ichor.model.publishedDate
import name.levis.ichor.model.visibleSections
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import name.levis.ichor.ui.components.MutedText

/** The heading of a section: translated for the known kinds, else the title of the JSON. */
@Composable
fun ChangelogSection.heading(): String = when (knownKind) {
    ChangelogKind.NEW -> stringResource(R.string.changelog_kind_new)
    ChangelogKind.FIXED -> stringResource(R.string.changelog_kind_fixed)
    ChangelogKind.FASTER -> stringResource(R.string.changelog_kind_faster)
    ChangelogKind.BREAKING -> stringResource(R.string.changelog_kind_breaking)
    null -> title.ifBlank { kind }
}

/** One release: "version · date", then its sections as bullet lists. */
@Composable
fun ReleaseNotes(release: ChangelogRelease, modifier: Modifier = Modifier) {
    val date = remember(release.publishedAt) {
        release.publishedDate?.let { DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).format(it) }
    }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(listOfNotNull(release.version, date).joinToString(" · "), style = MaterialTheme.typography.titleSmall)
        val sections = release.visibleSections
        if (sections.isEmpty()) {
            MutedText(stringResource(R.string.changelog_maintenance))
        }
        sections.forEach { section ->
            Text(
                section.heading(),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.padding(top = 4.dp),
            )
            section.items.forEach { item ->
                Row {
                    Text("•", style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(end = 8.dp))
                    Text(item, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

/** Several releases, newest first. */
@Composable
fun ReleaseNotesList(releases: List<ChangelogRelease>, modifier: Modifier = Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(16.dp)) {
        releases.forEach { ReleaseNotes(it) }
    }
}

/** "Updated to X": what changed since the build that ran before the update. */
@Composable
fun WhatsNewDialog(versionName: String, releases: List<ChangelogRelease>, onFullChangelog: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.changelog_updated_to, versionName)) },
        text = { ReleaseNotesList(releases, Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState())) },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) } },
        dismissButton = { TextButton(onClick = onFullChangelog) { Text(stringResource(R.string.changelog_full)) } },
    )
}
