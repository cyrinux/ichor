package name.levis.ichor.widget

import androidx.glance.LocalContext
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.R
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.runtime.remember
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.shareLinkFor
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.ColorFilter
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalSize
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetManager
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
import name.levis.ichor.TalosApp
import name.levis.ichor.monitor.ClusterSnapshot
import java.util.Date

private val Card = Color(0xFF18263A)
private val Primary = Color(0xFFE8EEF5)
private val Secondary = Color(0xFF93A3B8)
private val Ok = Color(0xFF5BD18B)
private val Warn = Color(0xFFF2C14E)
private val Bad = Color(0xFFF0716B)

/** Below this height (one launcher cell) only the context and the count fit. */
private val CompactHeight = 90.dp

/**
 * Home-screen summary from the last background check of the widget's cluster (picked when it is
 * placed or reconfigured, see [WidgetConfigActivity]; by default the cluster on screen). Shows
 * counts only (no hostnames), as it stays visible when the app lock is on. Always the dark card
 * of the website mockup.
 */
class ClusterWidget : GlanceAppWidget() {
    override val sizeMode: SizeMode = SizeMode.Exact

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val app = context.applicationContext as TalosApp
        val store = app.monitorStore
        val widgetId = runCatching { GlanceAppWidgetManager(context).getAppWidgetId(id) }.getOrNull()
        // Read in the composition: provideGlance is not run again while a session is alive,
        // so a snapshot captured here would hide the one saved seconds after the widget is placed.
        provideContent {
            val state by store.state.collectAsState()
            val stored by app.configRepository.config.collectAsState()
            val choices by app.widgetClusters.choices.collectAsState()
            val names by app.clusterNames.names.collectAsState()
            val mask by app.uiPreferences.privacyMask.collectAsState()
            val choice = widgetId?.let { choices[it] }
            val snapshot = widgetSnapshot(state, widgetKeys(choice, stored?.summary, stored?.activeContext, state.active))
            // The name the user gave the cluster, as in the app (not in screenshot mode).
            val label = snapshot?.let { s -> names[s.fingerprint].takeIf { !mask.enabled && s.fingerprint.isNotBlank() } ?: s.context }
            // A widget on a cluster of its own opens that cluster, like its share link (selected once unlocked).
            val clusterId = widgetClusterId(choice, stored?.summary)
            val link = remember(clusterId) { clusterId?.let { shareLinkFor(ShareTarget.screen(ShareTarget.CLUSTER), it) } }
            WidgetContent(snapshot, label, System.currentTimeMillis(), openIntent(context, link))
        }
    }
}

/** The app as is without [link], else opened like that share link (on its cluster, once unlocked). */
private fun openIntent(context: Context, link: String?): Intent {
    val intent = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    return if (link == null) intent else intent.setAction(Intent.ACTION_VIEW).setData(Uri.parse(link))
}

@Composable
private fun WidgetContent(s: ClusterSnapshot?, label: String?, now: Long, open: Intent) {
    // Glance's context is the application's; below API 33 apply the in-app language to it.
    val res = AppLocale.wrap(LocalContext.current)
    val stale = isStale(s, now)
    val compact = LocalSize.current.height < CompactHeight
    Column(
        modifier = GlanceModifier.fillMaxSize().background(Card).cornerRadius(16.dp)
            .padding(horizontal = 14.dp, vertical = 12.dp).clickable(actionStartActivity(open)),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label ?: "Talos",
            style = TextStyle(color = ColorProvider(Secondary), fontSize = 12.sp),
            maxLines = 1,
        )
        Text(
            if (s == null) "–" else res.getString(R.string.widget_ready_count, s.readyCount, s.nodes.size),
            style = TextStyle(
                color = ColorProvider(if (stale) Primary.copy(alpha = 0.5f) else Primary),
                fontSize = 24.sp,
                fontWeight = FontWeight.Medium,
            ),
            maxLines = 1,
        )
        if (compact) return@Column
        Spacer(GlanceModifier.height(4.dp))
        when {
            s == null -> Label(res.getString(R.string.widget_setup))
            stale -> Label(
                res.getString(R.string.widget_updated_at, android.text.format.DateFormat.getTimeFormat(res).format(Date(s.takenAt))),
            )
            else -> Row(verticalAlignment = Alignment.CenterVertically) {
                statusItems(s).forEachIndexed { i, item ->
                    if (i > 0) Spacer(GlanceModifier.width(12.dp))
                    StatusChip(res, item)
                }
            }
        }
    }
}

@Composable
private fun StatusChip(res: Context, item: StatusItem) {
    val plural = res.resources
    val (color, text) = when (item) {
        is StatusItem.NotReady -> Warn to plural.getQuantityString(R.plurals.widget_not_ready, item.count, item.count)
        is StatusItem.Unreachable -> Bad to plural.getQuantityString(R.plurals.widget_unreachable, item.count, item.count)
        StatusItem.AllReady -> Ok to res.getString(R.string.widget_all_ready)
        is StatusItem.Etcd -> if (item.alarms == 0) {
            Ok to res.getString(R.string.widget_etcd_no_alarms)
        } else {
            Bad to plural.getQuantityString(R.plurals.widget_etcd_alarms, item.alarms, item.alarms)
        }
    }
    Image(
        provider = ImageProvider(R.drawable.widget_dot),
        contentDescription = null,
        modifier = GlanceModifier.size(7.dp),
        colorFilter = ColorFilter.tint(ColorProvider(color)),
    )
    Spacer(GlanceModifier.width(5.dp))
    Label(text)
}

@Composable
private fun Label(text: String) {
    Text(text, style = TextStyle(color = ColorProvider(Secondary), fontSize = 12.sp), maxLines = 1)
}

class ClusterWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = ClusterWidget()

    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        (context.applicationContext as TalosApp).launchSync(runNow = true)
    }

    override fun onDeleted(context: Context, appWidgetIds: IntArray) {
        super.onDeleted(context, appWidgetIds)
        (context.applicationContext as TalosApp).widgetClusters.forget(appWidgetIds)
    }

    override fun onDisabled(context: Context) {
        super.onDisabled(context)
        (context.applicationContext as TalosApp).launchSync()
    }
}
