package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import name.levis.ichor.ui.design.IchorAssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeServices
import name.levis.ichor.model.LbController
import name.levis.ichor.model.ServiceRow
import name.levis.ichor.model.filteredServices
import name.levis.ichor.ui.app
import name.levis.ichor.ui.apps.RouteCertBadge
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory

/** The Services of a namespace (null for every one), with their addresses, endpoints and routes. */
class ServicesViewModel(private val browser: KubeBrowserRepository) : NamespaceLoadingViewModel<KubeServices>() {
    override suspend fun fetch() = browser.services(namespace)
}

/**
 * The Services of the namespace the Kubernetes screens list, problems first: load balancers
 * without an address (with a hint on the pool to check), then Services with no ready
 * endpoint. A tap opens the Service's summary ([onObject]); a route opens its URL, and its
 * object summary behind the info button beside it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServicesScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    onLoadBalancing: () -> Unit,
    vm: ServicesViewModel = viewModel(factory = factory { ServicesViewModel(app.kubeBrowser) }),
) {
    KubeScopedList(
        title = stringResource(R.string.kube_services_title),
        vm = vm,
        control = rememberListScope(vm),
        onBack = onBack,
        items = KubeServices::services,
        namespaceOf = ServiceRow::namespace,
        filter = { rows, query -> rows.filteredServices(query) },
        key = ServiceRow::key,
        search = R.string.kube_services_search,
        empty = R.string.kube_services_empty,
        noMatch = R.string.kube_services_no_match,
        notes = { loaded ->
            val notes = Modifier.padding(horizontal = 16.dp, vertical = 4.dp)
            if (loaded?.partialAccess == true) MutedText(stringResource(R.string.kube_services_partial), modifier = notes)
            if (loaded?.anyPending == true) {
                MutedText(stringResource(lbHint(loaded.lbController)), modifier = notes)
                IchorTextButton(onClick = onLoadBalancing, modifier = Modifier.padding(horizontal = 4.dp)) {
                    Text(stringResource(R.string.lb_open))
                }
            }
        },
    ) { svc ->
        ServiceRowItem(
            svc,
            onClick = { onObject(KubeObjectRef.service(svc.namespace, svc.name)) },
            onObject = onObject,
        )
    }
}

/** What to check when a load balancer has no address, by the controller found. */
private fun lbHint(controller: String): Int = when (LbController.of(controller)) {
    LbController.METALLB -> R.string.kube_services_hint_metallb
    LbController.CILIUM -> R.string.kube_services_hint_cilium
    LbController.KUBE_VIP -> R.string.kube_services_hint_kubevip
    null -> R.string.kube_services_hint_none
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ServiceRowItem(svc: ServiceRow, onClick: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    val uriHandler = LocalUriHandler.current
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                svc.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(svc.type, style = MaterialTheme.typography.labelMedium, color = svc.level.color())
        }
        val inside = listOfNotNull(
            stringResource(R.string.kube_services_headless).takeIf { svc.headless } ?: svc.clusterIP.takeIf { it.isNotEmpty() },
            svc.externalName.takeIf { it.isNotEmpty() }?.let { "→ $it" },
            svc.ports.joinToString(", ").takeIf { it.isNotEmpty() },
        )
        if (inside.isNotEmpty()) MutedText(inside.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        when {
            svc.pending -> Text(
                stringResource(R.string.kube_services_no_address),
                style = MaterialTheme.typography.labelMedium,
                color = svc.level.color(),
            )
            svc.addresses.isNotEmpty() -> Text(
                svc.addresses.joinToString(", "),
                style = MaterialTheme.typography.labelMedium,
                fontFamily = FontFamily.Monospace,
            )
        }
        svc.readyText?.let { MutedText(stringResource(R.string.kube_services_ready, it)) }
        if (svc.routes.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                svc.routes.forEach { route ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        IchorAssistChip(
                            onClick = { runCatching { uriHandler.openUri(route.url) } },
                            label = { Text(route.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        )
                        RouteCertBadge(route)
                        route.objectRef?.let { ref ->
                            IchorIconButton(onClick = { onObject(ref) }, modifier = Modifier.size(32.dp)) {
                                Icon(
                                    Icons.Outlined.Info,
                                    contentDescription = stringResource(R.string.apps_detail_routes_object),
                                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                                    modifier = Modifier.size(18.dp),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}
