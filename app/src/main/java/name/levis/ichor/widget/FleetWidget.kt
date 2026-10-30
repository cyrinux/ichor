package name.levis.ichor.widget

import android.content.Context
import android.content.Intent
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.ColorFilter
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalContext
import androidx.glance.LocalSize
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.background
import androidx.glance.layout.Alignment
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.FleetFinding
import name.levis.ichor.model.FleetTone
import name.levis.ichor.monitor.FleetGlanceRow
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.fleet.trackLabel
import name.levis.ichor.util.timeAgo

/**
 * Home-screen glance at every cluster: "2 / 6 need attention" in the worst tone's colour, then
 * the first of those clusters with their top finding; a tap opens the fleet screen. Drawn from
 * the ranking the monitor kept after its last check ([name.levis.ichor.monitor.FleetGlance]),
 * so it never calls the Go core or a cluster. The names are the app's (masked in screenshot
 * mode), the findings as the fleet screen words them; no hostname is added to them.
 */
class FleetWidget : GlanceAppWidget() {
    override val sizeMode: SizeMode = SizeMode.Exact

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val store = (context.applicationContext as TalosApp).monitorStore
        val open = Intent(context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            .putExtra(MainActivity.EXTRA_OPEN, DeepLink.FLEET.name)
        provideContent {
            val state by store.state.collectAsState()
            val now = System.currentTimeMillis()
            val rows = fleetWidgetRowsFor(LocalSize.current.height.value)
            FleetContent(fleetWidgetView(state.fleet, now, rows), now, open)
        }
    }
}

@Composable
private fun FleetContent(view: FleetWidgetView?, now: Long, open: Intent) {
    // Glance's context is the application's; below API 33 apply the in-app language to it.
    val res = AppLocale.wrap(LocalContext.current)
    val compact = LocalSize.current.height < CompactHeight
    Column(
        modifier = GlanceModifier.fillMaxSize().background(Card).cornerRadius(16.dp)
            .padding(horizontal = 14.dp, vertical = 12.dp).clickable(actionStartActivity(open)),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Label(res.getString(R.string.fleet_title))
        Text(
            if (view == null) "–" else res.getString(R.string.fleet_widget_count, view.attention, view.total),
            style = TextStyle(color = ColorProvider(countColor(view)), fontSize = 24.sp, fontWeight = FontWeight.Medium),
            maxLines = 1,
        )
        if (compact) return@Column
        when {
            view == null -> Label(res.getString(R.string.widget_setup))
            view.allStale -> Label(
                if (view.oldestCheck > 0) res.getString(R.string.fleet_widget_oldest, timeAgo(view.oldestCheck, now)) else res.getString(R.string.fleet_never_checked),
            )
            else -> {
                Label(res.getString(R.string.fleet_widget_need_attention))
                Spacer(GlanceModifier.height(4.dp))
                FleetDetail(res, view)
            }
        }
    }
}

/** The rows of the clusters needing a look, else "All 6 fine" or how many were not checked lately. */
@Composable
private fun FleetDetail(res: Context, view: FleetWidgetView) {
    if (view.rows.isEmpty()) {
        val text = if (view.unchecked > 0) {
            res.getString(R.string.fleet_summary_unchecked, view.unchecked)
        } else {
            res.getString(R.string.fleet_widget_all_fine, view.total)
        }
        Label(text)
        return
    }
    view.rows.forEach { row ->
        Row(verticalAlignment = Alignment.CenterVertically) {
            Image(
                provider = ImageProvider(R.drawable.widget_dot),
                contentDescription = null,
                modifier = GlanceModifier.size(7.dp),
                colorFilter = ColorFilter.tint(ColorProvider(toneColor(row.tone))),
            )
            Spacer(GlanceModifier.width(5.dp))
            Label(rowText(res, row))
        }
    }
}

/** "prod-eu · Alert: KubeNodeNotReady", or the name alone without a finding. */
private fun rowText(res: Context, row: FleetGlanceRow): String =
    row.finding?.let { res.getString(R.string.fleet_widget_row, row.name, findingText(res, it)) } ?: row.name

/** The finding as the fleet screen words it; its subject was already dropped when screenshot mode hides it. */
private fun findingText(res: Context, finding: FleetFinding): String {
    val track = res.getString(trackLabel(finding))
    return if (finding.subject.isBlank()) track else res.getString(R.string.fleet_finding, track, finding.subject)
}

private fun countColor(view: FleetWidgetView?): Color = when {
    view == null -> Primary
    view.allStale -> Primary.copy(alpha = 0.5f)
    else -> toneColor(view.tone)
}

private fun toneColor(tone: FleetTone): Color = when (tone) {
    FleetTone.BAD -> Bad
    FleetTone.WARN -> Warn
    FleetTone.UNKNOWN -> Secondary
    FleetTone.OK -> Ok
}

class FleetWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = FleetWidget()

    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        (context.applicationContext as TalosApp).launchSync(runNow = true)
    }

    override fun onDisabled(context: Context) {
        super.onDisabled(context)
        (context.applicationContext as TalosApp).launchSync()
    }
}
