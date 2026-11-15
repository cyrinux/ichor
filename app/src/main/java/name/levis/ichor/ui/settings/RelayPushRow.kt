package name.levis.ichor.ui.settings

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.isDemo
import name.levis.ichor.monitor.RelayPair
import name.levis.ichor.monitor.RelayProblem
import name.levis.ichor.monitor.relayLabel
import name.levis.ichor.monitor.relayRepository
import name.levis.ichor.monitor.relayStore
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorSwitch
import name.levis.ichor.ui.design.IchorTextButton
import org.unifiedpush.android.connector.UnifiedPush

private const val DISTRIBUTORS_URL = "https://unifiedpush.org/users/distributors/"

/**
 * "Push from ichor-relay" for the cluster on screen, under the Alertmanager row: turning it on
 * finds the relay (Go checks this config may read its pairing token), picks a UnifiedPush
 * distributor and registers with the relay's VAPID key; RelayPushService takes it from there.
 * Turning it off unpairs in one tap; the poller keeps alerting either way.
 */
@Composable
fun RelayPushRow(app: TalosApp, enabled: Boolean) {
    val stored by app.configRepository.config.collectAsStateWithLifecycle()
    val cluster = stored?.activeSummary?.takeIf { it.fingerprint.isNotBlank() } ?: return
    val pairs by app.relayStore.pairs.collectAsStateWithLifecycle()
    val pair = pairs[cluster.fingerprint] ?: RelayPair()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    var message by remember(cluster.fingerprint) { mutableStateOf<String?>(null) }
    var noDistributor by remember(cluster.fingerprint) { mutableStateOf(false) }
    var choose by remember { mutableStateOf<DistributorChoice?>(null) }
    val title = stringResource(R.string.relay_push)

    fun toggle(on: Boolean) {
        message = null
        noDistributor = false
        busy = true
        scope.launch {
            try {
                if (on) {
                    turnOn(app, context, cluster, onNoDistributor = { noDistributor = true }, onChoose = { choose = it }) { message = it }
                } else {
                    turnOff(app, context, cluster.fingerprint, pair)
                }
            } finally {
                busy = false
            }
        }
    }

    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            MutedText(message ?: relayStatus(context, cluster, pair))
            if (noDistributor) {
                val uriHandler = LocalUriHandler.current
                IchorTextButton(onClick = { uriHandler.openUri(DISTRIBUTORS_URL) }) { Text(stringResource(R.string.relay_get_distributor)) }
            }
        }
        IchorSwitch(
            checked = pair.enabled,
            onCheckedChange = { toggle(it) },
            enabled = enabled && !busy && !cluster.isDemo,
            modifier = Modifier.padding(start = 12.dp).semantics { contentDescription = title },
        )
    }

    choose?.let { choice ->
        IchorAlertDialog(
            onDismissRequest = { choose = null },
            title = { Text(stringResource(R.string.relay_pick_distributor)) },
            text = {
                Column {
                    choice.packages.forEach { pkg ->
                        IchorTextButton(onClick = {
                            choose = null
                            UnifiedPush.saveDistributor(context, pkg)
                            scope.launch { choice.then() }
                        }) { Text(appLabel(context, pkg)) }
                    }
                }
            },
            confirmButton = {},
            dismissButton = { IchorTextButton(onClick = { choose = null }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** Several distributors installed and none chosen yet: [then] registers once one is saved. */
private class DistributorChoice(val packages: List<String>, val then: suspend () -> Unit)

/** The row's line: why it cannot be on, how it is paired, or what went wrong. */
private fun relayStatus(context: Context, cluster: ContextSummary, pair: RelayPair): String = when {
    cluster.isDemo -> context.getString(R.string.relay_demo)
    !pair.enabled -> context.getString(R.string.relay_push_desc)
    pair.problem == RelayProblem.UNDECRYPTED -> context.getString(R.string.relay_update_distributor)
    pair.problem.isNotEmpty() -> context.getString(R.string.relay_not_paired, pair.detail.ifBlank { pair.problem })
    pair.paired -> context.getString(R.string.relay_paired_via, appLabel(context, pair.distributor))
    else -> context.getString(R.string.relay_pairing)
}

private suspend fun turnOn(
    app: TalosApp,
    context: Context,
    cluster: ContextSummary,
    onNoDistributor: () -> Unit,
    onChoose: (DistributorChoice) -> Unit,
    fail: (String) -> Unit,
) {
    val distributors = UnifiedPush.getDistributors(context)
    if (distributors.isEmpty()) {
        fail(context.getString(R.string.relay_no_distributor))
        onNoDistributor()
        return
    }
    val relays = relayRepository(app, cluster.name)
    val found: PromSource
    val vapid: String
    try {
        val discovery = relays.discover()
        val source = discovery.sources.firstOrNull()
        if (!discovery.canPair || source == null) {
            fail(if (discovery.reason.isBlank()) context.getString(R.string.relay_none_found) else context.getString(R.string.relay_cannot_pair, discovery.reason))
            return
        }
        found = source
        vapid = relays.pairing(source).vapidPublicKey
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        fail(context.getString(R.string.relay_cannot_pair, e.message.orEmpty()))
        return
    }
    val label = relayLabel(app, cluster)
    val register: suspend () -> Unit = {
        app.relayStore.update(cluster.fingerprint) { RelayPair(enabled = true, source = found, label = label) }
        UnifiedPush.register(context, cluster.fingerprint, label, vapid.ifBlank { null })
    }
    val saved = UnifiedPush.getSavedDistributor(context)
    when {
        saved != null && saved in distributors -> register()
        distributors.size == 1 -> {
            UnifiedPush.saveDistributor(context, distributors.single())
            register()
        }
        else -> onChoose(DistributorChoice(distributors, register))
    }
}

/** Removes the device from the relay (best effort: it may be gone), then the distributor's registration. */
private suspend fun turnOff(app: TalosApp, context: Context, fingerprint: String, pair: RelayPair) {
    val source = pair.source
    val cluster = app.configRepository.config.value?.activeSummary
    if (source != null && pair.deviceId.isNotEmpty() && cluster != null) {
        runCatching { relayRepository(app, cluster.name).unregister(source, pair.deviceId) }
    }
    runCatching { UnifiedPush.unregister(context, fingerprint) }
    app.relayStore.clear(fingerprint)
}

/** The distributor's name as the launcher shows it ("ntfy"), else its package. */
private fun appLabel(context: Context, pkg: String): String = runCatching {
    val pm = context.packageManager
    val info = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        pm.getApplicationInfo(pkg, PackageManager.ApplicationInfoFlags.of(0))
    } else {
        @Suppress("DEPRECATION")
        pm.getApplicationInfo(pkg, 0)
    }
    pm.getApplicationLabel(info).toString()
}.getOrDefault(pkg)
