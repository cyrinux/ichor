package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.staggeredgrid.LazyVerticalStaggeredGrid
import androidx.compose.foundation.lazy.staggeredgrid.StaggeredGridCells
import androidx.compose.foundation.lazy.staggeredgrid.rememberLazyStaggeredGridState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.model.ARGO_CD_CATALOG_ID
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ClusterOutage
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.FLUX_CATALOG_ID
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.OverviewCard
import name.levis.ichor.model.OverviewLayout
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.model.TalosUpdateOffer
import name.levis.ichor.model.clusterSummary
import name.levis.ichor.model.groupNodes
import name.levis.ichor.model.inventoryBadges
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.isDenseCluster
import name.levis.ichor.model.lacksPublicIps
import name.levis.ichor.model.nodesAnswered
import name.levis.ichor.model.talosUpdateOffer
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.rememberTwoPanes
import name.levis.ichor.ui.components.VersionFooter
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.history.SinceLastLookedCard
import name.levis.ichor.ui.share.ClusterPins
import name.levis.ichor.ui.tour.TourAnchor
import name.levis.ichor.ui.tour.tourAnchor
import name.levis.ichor.ui.tour.tourScrollable

/**
 * What the overview's cards show besides the nodes: each one null while the cluster has nothing
 * for it or the role may not read it.
 */
internal class OverviewCardData(
    val apps: UiState<Inventory>,
    val dataServices: UiState<DataServices>?,
    val dataHints: String,
    val dataPlugins: List<PluginDetectItem>,
    val argo: UiState<ArgoStatus>?,
    val flux: UiState<FluxStatus>?,
    val alerts: UiState<AlertsOverview?>?,
    val pins: ClusterPins?,
)

/** What the role may do on the overview's nodes. */
internal class NodeListAccess(
    val canPower: Boolean,
    val canShell: Boolean,
    val canDrain: Boolean,
    val canUpgrade: Boolean,
    val canDetectPublicIps: Boolean,
)

@Composable
internal fun NodeList(
    overview: ClusterOverview,
    topology: ClusterTopology?,
    outage: ClusterOutage?,
    onRetry: () -> Unit,
    clusterName: String?,
    fingerprint: String?,
    time: UiState<ClusterTime>,
    certificate: ContextSummary?,
    cards: OverviewCardData,
    nav: OverviewScreenNavigation,
    access: NodeListAccess,
    skippedTalosUpdate: String?,
    onSkipTalosUpdate: (String) -> Unit,
    live: ClusterLiveState?,
    discovered: Int,
    onDiscovered: () -> Unit,
    layout: OverviewLayout,
    onCustomize: () -> Unit,
    nodesExpanded: Boolean,
    onToggleNodes: () -> Unit,
) {
    var sheetFor by remember { mutableStateOf<NodeOverview?>(null) }
    val wakeOnLan = rememberWakeOnLan(fingerprint)
    // A partial overview lacks the public IPs only peers see, and its nodes still come in.
    val publicIps = rememberPublicIpDetection(fingerprint, access.canDetectPublicIps && !overview.partial && overview.lacksPublicIps())
    RecordNodeMacs(fingerprint, if (overview.partial) emptyList() else overview.nodes)
    sheetFor?.let { node ->
        NodeActionsSheet(
            node = node,
            canPower = access.canPower,
            canShell = access.canShell,
            wol = wakeOnLan(node),
            canDrain = access.canDrain,
            onAction = { nav.onNodeAction(node, it) },
            onDismiss = { sheetFor = null },
        )
    }
    val nodeGroups = remember(overview, topology) { topology.groupNodes(overview.nodes) }
    // Not asked (an internet call) for the demo cluster, or while its card is hidden.
    val talosCheck = if (certificate?.isDemo == true || OverviewCard.TALOS_UPDATE in layout.hidden) null
    else rememberTalosUpdateCheck(overview.nodes)
    val talosUpdate = remember(talosCheck, overview.nodes, skippedTalosUpdate) {
        talosCheck?.let { talosUpdateOffer(it, overview.nodes, skippedTalosUpdate) }
    }
    // The rollout stays open across the upgrade screen and until closed, even once its card is
    // gone (no reachable node is outdated any more).
    var rollingOut by rememberSaveable { mutableStateOf(false) }
    if (rollingOut && access.canUpgrade && talosCheck != null) {
        TalosRolloutDialog(
            overview.nodes,
            talosCheck.latest,
            onUpgrade = { nav.onUpgrade(it, talosCheck.latest) },
            onDismiss = { rollingOut = false },
            onUpgradeAll = {
                rollingOut = false
                nav.onUpgradeCluster(talosCheck.latest)
            },
        )
    }
    // The demo tour scrolls down to the nodes card and its node in trouble when they are below the fold.
    val listState = rememberLazyListState()
    val nodesListed = OverviewCard.NODES in layout.visible && overview.nodes.isNotEmpty()
    val tourReachable = remember(nodesListed, nodeGroups) {
        if (!nodesListed) emptySet()
        else setOfNotNull(
            TourAnchor.Nodes,
            TourAnchor.DegradedNode.takeIf { !isDenseCluster(overview.nodes.size) && nodeGroups.firstDegraded() != null },
        )
    }
    // Two columns on a tablet or an unfolded foldable; crossing the width keeps the first card shown.
    val twoColumns = rememberTwoPanes()
    val gridState = rememberLazyStaggeredGridState()
    var shownTwoColumns by rememberSaveable { mutableStateOf(twoColumns) }
    LaunchedEffect(twoColumns) {
        if (twoColumns == shownTwoColumns) return@LaunchedEffect
        if (twoColumns) gridState.scrollToItem(listState.firstVisibleItemIndex) else listState.scrollToItem(gridState.firstVisibleItemIndex)
        shownTwoColumns = twoColumns
    }
    val content: OverviewItems.() -> Unit = {
        outage?.let { item(fullLine = true) { LastKnownBanner(it, onRetry) } }
        if (certificate?.isDemo == true) item(fullLine = true) {
            Card(Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.demo_notice), modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium)
            }
        }
        if (BuildConfig.SELF_UPDATE) item(fullLine = true) { UpdateBanner(onClick = nav.onSettings) } else item(fullLine = true) { StoreUpdateBanner() }
        if (BuildConfig.DONATIONS || BuildConfig.FEATURE_FUNDING) item(fullLine = true) { SupportCard(nav.onFunding) }
        certificate?.let { item(fullLine = true) { CertificateBanner(it, nav.onIssueConfig) } }
        if (discovered > 0) item(fullLine = true) { DiscoveredNodesBanner(discovered, onDiscovered) }
        fingerprint?.takeIf { it.isNotBlank() }?.let { item(key = "since", fullLine = true) { SinceLastLookedCard(it) } }
        // The cards, as arranged; a long press on one opens the arrangement.
        layout.visible.forEach { card ->
            overviewCard(
                card,
                OverviewCardContext(overview, cards, nav, onCustomize),
                talosUpdate = talosUpdate,
                canUpgrade = access.canUpgrade,
                onRollout = { rollingOut = true },
                onSkipTalosUpdate = onSkipTalosUpdate,
                summary = { modifier -> ClusterSummaryCard(
                    clusterName ?: overview.context,
                    clusterSummary(overview.nodes),
                    live,
                    overview.nodesAnswered,
                    nav.onInsights,
                    nav.onCapacity,
                    context = certificate,
                    modifier = modifier,
                ) },
                nodes = { titleModifier -> NodesCard(
                    nodeGroups,
                    publicIps,
                    expanded = nodesExpanded,
                    onToggle = onToggleNodes,
                    onNode = nav.onNode,
                    onLive = { nav.onNodeAction(it, NodeAction.LIVE) },
                    onMore = { sheetFor = it },
                    onAllNodes = nav.onAllNodes,
                    titleModifier = titleModifier,
                ) },
                time = time,
            )
        }
        // The Talos update card with no release to offer, or the Pinned one without pins, leaves the overview as empty.
        val shows: (OverviewCard) -> Boolean = {
            when (it) {
                OverviewCard.TALOS_UPDATE -> talosUpdate != null
                OverviewCard.PINNED -> cards.pins?.pins?.isNotEmpty() == true
                else -> true
            }
        }
        if (layout.visible.none(shows)) item(key = "all-hidden", fullLine = true) {
            Card(onClick = onCustomize, modifier = Modifier.fillMaxWidth()) {
                MutedText(stringResource(R.string.overview_edit_all_hidden), modifier = Modifier.padding(16.dp))
            }
        }
        item(key = "version", fullLine = true) { VersionFooter(onClick = nav.onChangelog) }
    }
    if (twoColumns) {
        LazyVerticalStaggeredGrid(
            columns = StaggeredGridCells.Fixed(2),
            state = gridState,
            contentPadding = PaddingValues(16.dp),
            verticalItemSpacing = 10.dp,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            modifier = Modifier.fillMaxSize(),
        ) { overviewItems().content() }
    } else {
        LazyColumn(
            state = listState,
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
            modifier = Modifier.fillMaxSize().tourScrollable(listState, tourReachable),
        ) { overviewItems().content() }
    }
}

/** What every card of [overviewCard] reads. */
private class OverviewCardContext(
    val overview: ClusterOverview,
    val cards: OverviewCardData,
    val nav: OverviewScreenNavigation,
    val onCustomize: () -> Unit,
)

/**
 * One card of the overview, when the cluster has something for it. [summary] and [nodes] draw the
 * two that need the list's own state, with the modifier that opens the arrangement.
 */
private fun OverviewItems.overviewCard(
    card: OverviewCard,
    on: OverviewCardContext,
    talosUpdate: TalosUpdateOffer?,
    canUpgrade: Boolean,
    onRollout: () -> Unit,
    onSkipTalosUpdate: (String) -> Unit,
    summary: @Composable (Modifier) -> Unit,
    nodes: @Composable (Modifier) -> Unit,
    time: UiState<ClusterTime>,
) {
    val overview = on.overview
    val cards = on.cards
    val nav = on.nav
    val onCustomize = on.onCustomize
    when (card) {
        OverviewCard.PINNED -> if (cards.pins != null && cards.pins.pins.isNotEmpty()) item(key = card.name) {
            // The Argo CD and Flux data only when the role reads them (else null): no chip then.
            val data = PinnedData(overview.nodes, (cards.argo as? UiState.Loaded)?.data, (cards.flux as? UiState.Loaded)?.data)
            PinnedCard(
                cards.pins.pins,
                data,
                onOpen = { nav.onOpenTarget(it.target) },
                onUnpin = { cards.pins.toggle(it.target, it.label) },
                // Only on the title: a long press on a row offers Unpin.
                titleModifier = Modifier.longPressToCustomize(onCustomize),
            )
        }
        OverviewCard.TALOS_UPDATE -> if (talosUpdate != null) item(key = card.name) {
            Box(Modifier.longPressToCustomize(onCustomize)) {
                TalosUpdateCard(
                    talosUpdate,
                    canUpgrade,
                    onRollout = onRollout,
                    onSkip = { onSkipTalosUpdate(talosUpdate.latest) },
                )
            }
        }
        OverviewCard.SUMMARY -> item(key = card.name, fullLine = spansBothColumns(card)) { summary(Modifier.longPressToCustomize(onCustomize)) }
        OverviewCard.APPS -> item(key = card.name) {
            val argoData = (cards.argo as? UiState.Loaded)?.data
            val inventory = (cards.apps as? UiState.Loaded)?.data
            val badges = remember(argoData, inventory) { if (argoData != null && inventory != null) argoData.inventoryBadges(inventory.apps) else emptyMap() }
            Box(Modifier.longPressToCustomize(onCustomize)) { AppsCard(cards.apps, onOpen = { nav.onApps(false) }, argoBadges = badges, onAttention = { nav.onApps(true) }) }
        }
        OverviewCard.DATA_SERVICES -> if (cards.dataServices != null) item(key = card.name) {
            val inventory = (cards.apps as? UiState.Loaded)?.data
            val appsById = remember(inventory) { inventory?.apps.orEmpty().associateBy { it.id } }
            val hinted = remember(cards.dataHints) { DataServiceKind.entries.filter { it.catalogId in cards.dataHints.split(',') } }
            Box(Modifier.longPressToCustomize(onCustomize)) {
                DataServicesCard(cards.dataServices, hinted, appsById, { nav.onDataServices(it, false) }, cards.dataPlugins, nav.onPlugin)
            }
        }
        OverviewCard.ARGO_CD -> if (cards.argo != null) item(key = card.name) {
            val argoTile = (cards.apps as? UiState.Loaded)?.data?.apps?.firstOrNull { it.id == ARGO_CD_CATALOG_ID }
            val downNodes = remember(overview) { overview.downHostnames() }
            Box(Modifier.longPressToCustomize(onCustomize)) { ArgoCard(cards.argo, argoTile, downNodes, nav.onArgoCD) }
        }
        OverviewCard.FLUX -> if (cards.flux != null) item(key = card.name) {
            val fluxTile = (cards.apps as? UiState.Loaded)?.data?.apps?.firstOrNull { it.id == FLUX_CATALOG_ID }
            Box(Modifier.longPressToCustomize(onCustomize)) { FluxCard(cards.flux, fluxTile, nav.onFlux) }
        }
        OverviewCard.NODES -> if (overview.nodes.isNotEmpty()) item(key = card.name) {
            Box(Modifier.tourAnchor(TourAnchor.Nodes)) {
                // Only on the title: a long press on a node row opens its actions.
                nodes(Modifier.longPressToCustomize(onCustomize))
            }
        }
        OverviewCard.ALERTS -> if (cards.alerts != null) item(key = card.name) {
            Box(Modifier.longPressToCustomize(onCustomize)) { AlertsCard(cards.alerts, nav.onAlerts) }
        }
        OverviewCard.TIME_DRIFT -> item(key = card.name) {
            Box(Modifier.longPressToCustomize(onCustomize)) {
                TimeDriftCard(time, overview.nodes.associate { it.node to it.hostname })
            }
        }
    }
}
