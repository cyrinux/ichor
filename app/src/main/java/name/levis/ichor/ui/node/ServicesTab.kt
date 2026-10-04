package name.levis.ichor.ui.node

import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.model.ServiceAction
import name.levis.ichor.model.FeatureSupport
import name.levis.ichor.model.ServiceInfo
import name.levis.ichor.model.VersionNotice
import name.levis.ichor.ui.components.FeatureMenuItem
import name.levis.ichor.model.offeredActions
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/** [onAction] is null when the config's role cannot control services: no menu then. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServicesTab(
    node: String,
    onService: (String) -> Unit,
    onAction: ((ServiceRequest) -> Unit)?,
    busy: Boolean,
    actionsNotice: VersionNotice? = null,
    vm: ServicesViewModel = viewModel(key = "services-$node", factory = factory { ServicesViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> Column(Modifier.fillMaxSize()) {
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(s.data, key = { it.id }) { svc ->
                        ServiceRow(
                            svc,
                            onClick = { onService(svc.id) },
                            actions = if (onAction == null || busy) emptyList() else svc.offeredActions(),
                            onAction = { action -> onAction?.invoke(ServiceRequest(svc.id, action)) },
                            actionsNotice = actionsNotice,
                        )
                    }
                }
            }
            DataFreshness(s, edgeToEdge = false)
        }
    }
}

private val ServiceAction.icon
    get() = when (this) {
        ServiceAction.RESTART -> Icons.Outlined.RestartAlt
        ServiceAction.STOP -> Icons.Outlined.Stop
        ServiceAction.START -> Icons.Outlined.PlayArrow
    }

@Composable
private fun ServiceRow(
    svc: ServiceInfo,
    onClick: () -> Unit,
    actions: List<ServiceAction>,
    onAction: (ServiceAction) -> Unit,
    actionsNotice: VersionNotice?,
) {
    val colors = LocalStatusColors.current
    var menuOpen by remember { mutableStateOf(false) }
    Card(
        Modifier.fillMaxWidth().combinedClickable(
            onClick = onClick,
            onLongClick = if (actions.isEmpty()) null else ({ menuOpen = true }),
            onLongClickLabel = stringResource(R.string.service_actions),
        ),
    ) {
        Column(
            Modifier.padding(start = 16.dp, top = 12.dp, bottom = 12.dp, end = if (actions.isEmpty()) 16.dp else 4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(svc.id, style = MaterialTheme.typography.titleSmall)
                    MutedText(svc.state)
                }
                when (svc.health) {
                    "healthy" -> StatusPill(stringResource(R.string.common_status_healthy), colors.ok)
                    "unhealthy" -> StatusPill(stringResource(R.string.common_status_unhealthy), colors.bad)
                    else -> StatusPill(stringResource(R.string.node_service_no_check), colors.muted)
                }
                if (actions.isNotEmpty()) {
                    Box {
                        IconButton(onClick = { menuOpen = true }) {
                            Icon(Icons.Outlined.MoreVert, stringResource(R.string.service_actions))
                        }
                        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                            actions.forEach { action ->
                                FeatureMenuItem(
                                    label = stringResource(action.label),
                                    icon = action.icon,
                                    support = FeatureSupport(supported = actionsNotice == null, minVersion = actionsNotice?.minVersion.orEmpty()),
                                    onClick = {
                                        menuOpen = false
                                        onAction(action)
                                    },
                                )
                            }
                        }
                    }
                }
            }
            val detail = svc.message?.takeIf { svc.health == "unhealthy" } ?: svc.lastEvent
            detail?.takeIf { it.isNotBlank() }?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (svc.health == "unhealthy") colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 6.dp, end = 12.dp),
                )
            }
        }
    }
}
