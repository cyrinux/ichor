package name.levis.ichor.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.AlertDialog
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.GitHost
import name.levis.ichor.model.GitHostKind
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle

/** What the add sheet is open for: a new host, or one of the list to change. */
private sealed interface HostSheet {
    data object New : HostSheet
    data class Edit(val host: GitHost) : HostSheet
}

/**
 * Settings → Git hosts: a token per forge host, to read and change what Argo CD and Flux deploy.
 * The token is typed once, encrypted, and never shown again; Check says who it signs in as.
 */
@Composable
fun GitHostsSection(app: TalosApp) {
    val store = app.gitHostStore
    val hosts by store.hosts.collectAsStateWithLifecycle()
    val logins by store.logins.collectAsStateWithLifecycle()
    var sheet by remember { mutableStateOf<HostSheet?>(null) }
    var removing by remember { mutableStateOf<GitHost?>(null) }

    SectionTitle(stringResource(R.string.git_hosts_title))
    MutedText(stringResource(R.string.git_hosts_desc))
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            if (hosts.isEmpty()) {
                MutedText(stringResource(R.string.git_hosts_empty), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            }
            hosts.forEachIndexed { index, host ->
                if (index > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp))
                HostRow(host, logins[host.host], onOpen = { sheet = HostSheet.Edit(host) }, onRemove = { removing = host })
            }
        }
    }
    IchorOutlinedButton(onClick = { sheet = HostSheet.New }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.git_hosts_add))
    }
    MutedText(stringResource(R.string.git_hosts_explainer))

    sheet?.let { current ->
        GitHostSheet(
            app = app,
            editing = (current as? HostSheet.Edit)?.host,
            onDismiss = { sheet = null },
        )
    }
    removing?.let { host ->
        AlertDialog(
            onDismissRequest = { removing = null },
            title = { Text(stringResource(R.string.git_host_remove_title, host.host)) },
            text = { Text(stringResource(R.string.git_host_remove_text)) },
            confirmButton = {
                IchorTextButton(onClick = {
                    store.remove(host.host)
                    removing = null
                }) { Text(stringResource(R.string.git_host_remove)) }
            },
            dismissButton = { IchorTextButton(onClick = { removing = null }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

@Composable
private fun HostRow(host: GitHost, login: String?, onOpen: () -> Unit, onRemove: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(start = 16.dp, top = 4.dp, bottom = 4.dp, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(host.host, style = MaterialTheme.typography.bodyLarge)
            MutedText(gitHostKindLabel(host.kind))
            if (host.aliases.isNotEmpty()) {
                MutedText(stringResource(R.string.git_host_aliases_line, host.aliases.joinToString(", ")))
            }
            MutedText(login?.let { stringResource(R.string.git_host_signed_in, it) } ?: stringResource(R.string.git_host_not_checked))
        }
        IchorIconButton(onClick = onRemove) {
            Icon(Icons.Outlined.Delete, contentDescription = stringResource(R.string.git_host_remove))
        }
    }
}

/** GitHub, GitLab and Gitea/Forgejo are names, not translated; Auto is. */
@Composable
internal fun gitHostKindLabel(kind: GitHostKind): String = kind.label ?: stringResource(R.string.git_host_kind_auto)
