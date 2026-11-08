package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import name.levis.ichor.ui.components.Tag

/** A cluster's tags under its name. Shown in screenshot mode too: they are the user's words, no cluster's name. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ClusterTagChips(tags: List<String>) {
    FlowRow(
        modifier = Modifier.padding(top = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        tags.forEach { Tag(it) }
    }
}
