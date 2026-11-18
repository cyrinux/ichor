package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeBastionRepository
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KubeBastionInfo
import name.levis.ichor.model.bastionOffered

/** "Through SSH bastion…" in a cluster row's menu, next to the API address. */
@Composable
internal fun BastionMenuItem(onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(stringResource(R.string.bastion_menu)) },
        leadingIcon = { Icon(Icons.Outlined.Lan, contentDescription = null) },
        onClick = onClick,
    )
}

/** "Through SSH bastion <host>" under a cluster that goes through one; nothing in screenshot mode. */
@Composable
internal fun BastionRowLine(context: ContextSummary, masked: Boolean) {
    if (!bastionOffered(context, masked)) return
    val app = LocalContext.current.applicationContext as TalosApp
    val bastions = remember(app) { KubeBastionRepository(app.configRepository) }
    val revision by KubeBastionRepository.revision.collectAsStateWithLifecycle()
    val info by produceState<KubeBastionInfo?>(null, context.name, context.fingerprint, revision) {
        value = runCatching { bastions.info(context.name) }.getOrNull()
    }
    info?.takeIf { it.configured }?.let {
        Text(stringResource(R.string.bastion_row, it.host.removeSuffix(":22")), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
