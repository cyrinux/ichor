package name.levis.ichor.ui.metrics

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.PromLink
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.PromTarget
import name.levis.ichor.model.PromTargetPool
import name.levis.ichor.model.PromTargets
import name.levis.ichor.model.linkOf
import name.levis.ichor.model.monitorRef
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The Monitoring tab: the scrape targets that are down, by pool; the rule groups, those in
 * trouble first; the Prometheus Operator's servers. A row opens what it is about ([onLink]).
 */
@Composable
fun MonitoringTab(state: MonitoringState, source: PromSource, onLink: (PromLink) -> Unit) {
    // Fixed while the data is on screen: the ages must not drift between recompositions.
    val now = remember(state.targets.value, state.rules.value) { System.currentTimeMillis() }
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "source") { MutedText(stringResource(R.string.metrics_source_line, source.label)) }
        if (state.loading) item(key = "loading") { LinearProgressIndicator(Modifier.fillMaxWidth()) }
        targetsSection(state, now, onLink)
        rulesSection(state, now, onLink)
        operatorSection(state, onLink)
    }
}

/** A section's title and what it counts. */
@Composable
internal fun MonitoringHeader(title: String, summary: String?) {
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        summary?.let { MutedText(it) }
    }
}

private fun LazyListScope.targetsSection(state: MonitoringState, now: Long, onLink: (PromLink) -> Unit) {
    val targets = state.targets.value
    item(key = "targets") { MonitoringHeader(stringResource(R.string.monitoring_targets), targets?.let { targetsSummary(it) }) }
    state.targets.error?.let { item(key = "targets-error") { InlineError(it) } }
    if (targets == null) return
    val down = targets.pools.filter { it.down > 0 }
    items(down, key = { "pool-${it.pool}" }) { pool -> PoolCard(pool, now, onLink) }
    if (targets.truncated) item(key = "targets-truncated") { MutedText(stringResource(R.string.monitoring_targets_truncated)) }
}

@Composable
private fun targetsSummary(t: PromTargets): String = when {
    t.down > 0 -> stringResource(R.string.monitoring_targets_down, t.down.toString(), t.total.toString())
    else -> stringResource(R.string.monitoring_targets_all_up, t.total.toString())
}

/** A scrape pool with down targets: its monitor (tap: its object), then each down target. */
@Composable
private fun PoolCard(pool: PromTargetPool, now: Long, onLink: (PromLink) -> Unit) {
    val colors = LocalStatusColors.current
    val open = stringResource(R.string.common_open)
    val monitor = pool.monitorRef
    OutlinedCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 4.dp)) {
            Row(
                Modifier.fillMaxWidth()
                    .clickable(enabled = monitor != null, role = Role.Button, onClickLabel = open) { monitor?.let { onLink(PromLink.Object(it)) } }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Column(Modifier.weight(1f)) {
                    Text(
                        if (monitor != null) "${pool.namespace}/${pool.name}" else pool.pool,
                        style = MaterialTheme.typography.titleSmall,
                        fontFamily = FontFamily.Monospace,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    MutedText(pool.kind.ifEmpty { stringResource(R.string.monitoring_config_job) }, maxLines = 1)
                }
                TagBadge(stringResource(R.string.monitoring_pool_counts, pool.down.toString(), pool.up.toString()), colors.bad)
            }
            pool.targets.forEach { target ->
                HorizontalDivider()
                TargetRow(target, pool.linkOf(target), now, onLink)
            }
        }
    }
}

/** A down target: where it is scraped, why it fails, when it was last tried. */
@Composable
private fun TargetRow(target: PromTarget, link: PromLink?, now: Long, onLink: (PromLink) -> Unit) {
    val colors = LocalStatusColors.current
    val open = stringResource(R.string.common_open)
    Column(
        Modifier.fillMaxWidth()
            .clickable(enabled = link != null, role = Role.Button, onClickLabel = open) { link?.let(onLink) }
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            target.pod.ifEmpty { target.service }.ifEmpty { target.instance },
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        MutedText(target.scrapeUrl.ifEmpty { target.instance }, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (target.lastError.isNotEmpty()) {
            Text(target.lastError, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = colors.bad, maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
        MutedText(
            if (target.lastScrape > 0) stringResource(R.string.monitoring_last_scrape, ageSince(target.lastScrape, now))
            else stringResource(R.string.monitoring_never_scraped),
        )
    }
}
