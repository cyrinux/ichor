package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeEvent
import name.levis.ichor.model.isWarning
import name.levis.ichor.ui.UiState
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** How many events show before "Show all". */
private const val EVENTS_PREVIEW = 8

/**
 * The Kubernetes events of an object, newest first, like the end of `kubectl describe`: of the
 * [kind] named [name], or with an empty [kind] of [name] and what it owns (a Deployment's
 * ReplicaSets and pods). Read once when shown.
 */
@Composable
fun KubeEventsList(namespace: String, kind: String, name: String, modifier: Modifier = Modifier) {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    val state by produceState<UiState<List<KubeEvent>>>(UiState.Loading, namespace, kind, name) {
        value = uiStateOf { talos.kubeEvents(namespace, kind, name) }
    }
    val now = remember(state) { System.currentTimeMillis() }
    var all by rememberSaveable(namespace, kind, name) { mutableStateOf(false) }

    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        when (val s = state) {
            UiState.Loading -> LinearProgressIndicator(Modifier.fillMaxWidth())
            is UiState.Failed -> InlineError(s.message.asString())
            is UiState.Loaded -> {
                if (s.data.isEmpty()) MutedText(stringResource(R.string.kube_events_empty))
                (if (all) s.data else s.data.take(EVENTS_PREVIEW)).forEach { KubeEventRow(it, now, showObject = kind.isEmpty()) }
                if (s.data.size > EVENTS_PREVIEW) {
                    TextButton(onClick = { all = !all }) {
                        Text(if (all) stringResource(R.string.checkup_show_less) else stringResource(R.string.checkup_show_all, s.data.size.toString()))
                    }
                }
            }
        }
    }
}

/**
 * One event: its reason (amber for a Warning), when it was last seen, how often, and its
 * message; [showObject] names what it is about, with its namespace when [showNamespace].
 */
@Composable
internal fun KubeEventRow(e: KubeEvent, now: Long, showObject: Boolean, showNamespace: Boolean = false) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            TagBadge(e.reason, if (e.isWarning) colors.warn else colors.muted)
            if (e.count > 1) MutedText("× ${e.count}")
            MutedText(stringResource(R.string.kube_events_ago, ageSince(e.last, now)), modifier = Modifier.weight(1f), maxLines = 1)
            if (e.source.isNotEmpty()) MutedText(e.source, maxLines = 1)
        }
        if (showObject) {
            val qualified = if (showNamespace && e.namespace.isNotEmpty()) "${e.namespace}/${e.name}" else e.name
            Text("${e.kind} $qualified", style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = colors.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Text(e.message, style = MaterialTheme.typography.bodySmall, maxLines = 6, overflow = TextOverflow.Ellipsis)
    }
}
