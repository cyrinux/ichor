package name.levis.ichor.ui.settings

import androidx.annotation.StringRes
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
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.SUPPORTED_INTEGRATION_VIA_API
import name.levis.ichor.model.SUPPORTED_INTEGRATION_VIA_INVENTORY
import name.levis.ichor.model.SUPPORTED_INTEGRATION_VIA_PODS
import name.levis.ichor.model.SUPPORTED_INTEGRATION_VIA_SERVICES
import name.levis.ichor.model.SupportedIntegration
import name.levis.ichor.model.SupportedIntegrations
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.allows
import name.levis.ichor.model.asApp
import name.levis.ichor.model.supportedIntegrationHints
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
import name.levis.ichor.ui.components.pageContent

/** What the screen shows: the list, and why the cluster could not be asked when it could not. */
private data class SupportedIntegrationsView(val list: SupportedIntegrations, val problem: UiText? = null)

/**
 * The projects the app reads through the Kubernetes API, with their websites, and which ones
 * the active cluster runs: the list comes from the Go core, the status from one API discovery.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SupportedIntegrationsScreen(configs: ConfigRepository, kube: KubeRepository, onBack: () -> Unit) {
    val config by configs.config.collectAsStateWithLifecycle()
    val canAsk = config?.activeSummary?.allows(Feature.WORKLOADS) == true
    val static = remember { staticSupportedIntegrations() }
    val view by produceState(SupportedIntegrationsView(static), config?.activeContext, canAsk) {
        value = SupportedIntegrationsView(static)
        if (!canAsk) {
            value = SupportedIntegrationsView(static, UiText.Res(R.string.supported_integrations_needs_admin))
            return@produceState
        }
        val hints = kube.cached<Inventory>(INVENTORY)?.value?.supportedIntegrationHints().orEmpty()
        value = try {
            SupportedIntegrationsView(kube.supportedIntegrations(hints))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            SupportedIntegrationsView(static, e.uiText())
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.supported_integrations_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        LazyColumn(
            Modifier.pageContent(padding).fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item {
                MutedText(stringResource(R.string.supported_integrations_desc))
                view.problem?.let { MutedText(it.asString(), Modifier.padding(top = 4.dp)) }
            }
            items(view.list.items, key = { it.id }) { SupportedIntegrationRow(it, checked = view.list.checked) }
        }
    }
}

@Composable
private fun SupportedIntegrationRow(item: SupportedIntegration, checked: Boolean) {
    val context = LocalContext.current
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth().clickable(enabled = item.website.isNotEmpty()) { openUrl(context, item.website) }) {
        Row(Modifier.padding(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            AppIconTile(item.asApp, size = 40.dp)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(item.name, style = MaterialTheme.typography.bodyLarge)
                Text(
                    item.groups.joinToString(", ").ifEmpty { stringResource(R.string.supported_integrations_no_api) },
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (checked) {
                    if (item.detected) {
                        StatusPill(
                            listOf(stringResource(R.string.supported_integrations_detected), item.version).filter { it.isNotEmpty() }.joinToString(" · "),
                            colors.ok,
                        )
                        foundBy(item.via)?.let { how ->
                            MutedText(listOf(stringResource(how), item.namespace).filter { it.isNotEmpty() }.joinToString(" · "))
                        }
                    } else {
                        StatusPill(stringResource(R.string.supported_integrations_not_detected), colors.muted)
                    }
                }
            }
            Icon(
                Icons.AutoMirrored.Outlined.OpenInNew,
                contentDescription = stringResource(R.string.supported_integrations_website, item.name),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** How the integration was found, null for a value this version does not know. */
@StringRes
private fun foundBy(via: String): Int? = when (via) {
    SUPPORTED_INTEGRATION_VIA_API -> R.string.supported_integrations_found_api
    SUPPORTED_INTEGRATION_VIA_PODS -> R.string.supported_integrations_found_pods
    SUPPORTED_INTEGRATION_VIA_SERVICES -> R.string.supported_integrations_found_services
    SUPPORTED_INTEGRATION_VIA_INVENTORY -> R.string.supported_integrations_found_inventory
    else -> null
}

/** The list alone, from the Go core: shown before (or without) a cluster's answer. */
private fun staticSupportedIntegrations(): SupportedIntegrations =
    runCatching { TalosJson.decodeFromString(SupportedIntegrations.serializer(), Ichorgo.supportedIntegrations()) }.getOrDefault(SupportedIntegrations())
