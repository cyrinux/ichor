package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.ICHOR_WEBSITE_URL

/**
 * An (i) next to a label: tapping it explains the feature or how to read a value. With [link],
 * the explanation also offers that page of the documentation.
 */
@Composable
fun InfoHint(title: String, text: String, modifier: Modifier = Modifier, link: String? = null) {
    var open by rememberSaveable { mutableStateOf(false) }
    IchorIconButton(onClick = { open = true }, modifier = modifier) {
        Icon(
            Icons.Outlined.Info,
            contentDescription = stringResource(R.string.common_more_info),
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(18.dp),
        )
    }
    if (open) {
        val uri = LocalUriHandler.current
        AlertDialog(
            onDismissRequest = { open = false },
            title = { Text(title) },
            text = { Text(text, modifier = Modifier.verticalScroll(rememberScrollState())) },
            confirmButton = { IchorTextButton(onClick = { open = false }) { Text(stringResource(R.string.common_ok)) } },
            dismissButton = if (link == null) null else {
                {
                    IchorTextButton(onClick = {
                        open = false
                        uri.openUri(link)
                    }) { Text(stringResource(R.string.common_documentation)) }
                }
            },
        )
    }
}

/** The website's section on CronJobs: their icon, name and description labels. */
const val CRONJOBS_DOCS_URL = "${ICHOR_WEBSITE_URL}features.html#cronjobs"

/** The website's section on Argo CD: an app's own icon. */
const val ARGOCD_DOCS_URL = "${ICHOR_WEBSITE_URL}features.html#argocd"

/** The website's runbook reference (docs/runbooks.md, published by scripts/site-reference.py). */
const val RUNBOOKS_DOCS_URL = "${ICHOR_WEBSITE_URL}runbooks/"

/** A muted line closing a list, whose (i) tells how a resource names its own icon ([text]). */
@Composable
fun CustomIconHint(text: String, link: String, modifier: Modifier = Modifier) {
    val title = stringResource(R.string.custom_icon_hint)
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center) {
        Text(title, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        InfoHint(title, text, link = link)
    }
}
