package name.levis.ichor.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import name.levis.ichor.BuildConfig
import name.levis.ichor.R

/** The running version, quiet at the foot of a screen; tapping it opens [onClick] (what's new) when given. */
@Composable
fun VersionFooter(modifier: Modifier = Modifier, onClick: (() -> Unit)? = null) {
    val label = stringResource(R.string.changelog_title)
    Text(
        stringResource(R.string.version_footer, BuildConfig.VERSION_NAME),
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
        modifier = modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClickLabel = label, onClick = onClick) else Modifier)
            .padding(vertical = 12.dp),
    )
}
