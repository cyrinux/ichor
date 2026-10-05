package name.levis.ichor.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.INVENTORY
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.Integration
import name.levis.ichor.model.Integrations
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.allows
import name.levis.ichor.model.asApp
import name.levis.ichor.model.integrationHints
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.UiText
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.data.TalosJson

/** What the screen shows: the list, and why the cluster could not be asked when it could not. */
private data class IntegrationsView(val list: Integrations, val problem: UiText? = null)

/**
 * The projects the app reads through the Kubernetes API, with their websites, and which ones
 * the active cluster runs: the list comes from the Go core, the status from one API discovery.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun IntegrationsScreen(configs: ConfigRepository, talos: TalosRepository, onBack: () -> Unit) {
    val config by configs.config.collectAsStateWithLifecycle()
    val canAsk = config?.activeSummary?.allows(Feature.WORKLOADS) == true
    val static = remember { staticIntegrations() }
    val view by produceState(IntegrationsView(static), config?.activeContext, canAsk) {
        value = IntegrationsView(static)
        if (!canAsk) {
            value = IntegrationsView(static, UiText.Res(R.string.integrations_needs_admin))
            return@produceState
        }
        val hints = talos.cached<Inventory>(INVENTORY)?.value?.integrationHints().orEmpty()
        value = try {
            IntegrationsView(talos.integrations(hints))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            IntegrationsView(static, e.uiText())
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.integrations_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        LazyColumn(
            Modifier.padding(padding).fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item {
                MutedText(stringResource(R.string.integrations_desc))
                view.problem?.let { MutedText(it.asString(), Modifier.padding(top = 4.dp)) }
            }
            items(view.list.items, key = { it.id }) { IntegrationRow(it, checked = view.list.checked) }
        }
    }
}

@Composable
private fun IntegrationRow(item: Integration, checked: Boolean) {
    val context = LocalContext.current
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth().clickable(enabled = item.website.isNotEmpty()) { openUrl(context, item.website) }) {
        Row(Modifier.padding(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            AppIconTile(item.asApp, size = 40.dp)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(item.name, style = MaterialTheme.typography.bodyLarge)
                Text(
                    item.groups.joinToString(", ").ifEmpty { stringResource(R.string.integrations_no_api) },
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (checked) {
                    if (item.detected) {
                        StatusPill(
                            listOf(stringResource(R.string.integrations_detected), item.version).filter { it.isNotEmpty() }.joinToString(" · "),
                            colors.ok,
                        )
                    } else {
                        StatusPill(stringResource(R.string.integrations_not_detected), colors.muted)
                    }
                }
            }
            Icon(
                Icons.AutoMirrored.Outlined.OpenInNew,
                contentDescription = stringResource(R.string.integrations_website, item.name),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** The list alone, from the Go core: shown before (or without) a cluster's answer. */
private fun staticIntegrations(): Integrations =
    runCatching { TalosJson.decodeFromString(Integrations.serializer(), Ichorgo.integrations()) }.getOrDefault(Integrations())
