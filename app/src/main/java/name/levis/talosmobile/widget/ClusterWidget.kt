package name.levis.talosmobile.widget

import androidx.glance.LocalContext
import name.levis.talosmobile.i18n.AppLocale
import name.levis.talosmobile.R
import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.action.actionStartActivity
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
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import name.levis.talosmobile.MainActivity
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.monitor.ClusterSnapshot
import java.text.DateFormat
import java.util.Date

private val Ok = androidx.compose.ui.graphics.Color(0xFF5BD18B)
private val Warn = androidx.compose.ui.graphics.Color(0xFFF2C14E)
private val Bad = androidx.compose.ui.graphics.Color(0xFFFF6B6B)

/**
 * Home-screen summary from the last background check. Shows counts only (no hostnames), as it
 * stays visible when the app lock is on.
 */
class ClusterWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val snapshot = (context.applicationContext as TalosApp).monitorStore.snapshot()
        provideContent { GlanceTheme { WidgetContent(snapshot) } }
    }
}

@Composable
private fun WidgetContent(s: ClusterSnapshot?) {
    // Glance's context is the application's; below API 33 apply the in-app language to it.
    val res = AppLocale.wrap(LocalContext.current)
    val onSurface = GlanceTheme.colors.onSurface
    Column(
        modifier = GlanceModifier.fillMaxSize().background(GlanceTheme.colors.widgetBackground)
            .cornerRadius(20.dp).padding(14.dp).clickable(actionStartActivity<MainActivity>()),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (s == null) {
            Text("Talos", style = TextStyle(color = onSurface, fontWeight = FontWeight.Bold))
            Text(res.getString(R.string.widget_waiting), style = TextStyle(color = onSurface, fontSize = 12.sp))
            return@Column
        }
        Text(s.context, style = TextStyle(color = onSurface, fontSize = 12.sp))
        Text(
            res.getString(R.string.widget_ready_count, s.readyCount, s.nodes.size),
            style = TextStyle(
                color = ColorProvider(if (s.readyCount == s.nodes.size) Ok else Warn),
                fontSize = 22.sp,
                fontWeight = FontWeight.Bold,
            ),
        )
        Spacer(GlanceModifier.height(2.dp))
        Row {
            Count(res.resources.getQuantityString(R.plurals.widget_not_ready, s.notReadyCount, s.notReadyCount), s.notReadyCount, Warn)
            Spacer(GlanceModifier.width(10.dp))
            Count(res.resources.getQuantityString(R.plurals.widget_down, s.unreachableCount, s.unreachableCount), s.unreachableCount, Bad)
        }
        val etcd = when {
            !s.etcdChecked -> res.getString(R.string.widget_etcd_unknown)
            s.etcdAlarms.isEmpty() -> res.getString(R.string.widget_etcd_no_alarms)
            else -> res.resources.getQuantityString(R.plurals.widget_etcd_alarms, s.etcdAlarms.size, s.etcdAlarms.size)
        }
        Text(etcd, style = TextStyle(color = onSurface, fontSize = 12.sp))
        Text(
            res.getString(R.string.widget_updated, DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(s.takenAt))),
            style = TextStyle(color = GlanceTheme.colors.onSurfaceVariant, fontSize = 11.sp),
        )
    }
}

@Composable
private fun Count(text: String, n: Int, color: androidx.compose.ui.graphics.Color) {
    Text(
        text,
        style = TextStyle(
            color = if (n > 0) ColorProvider(color) else GlanceTheme.colors.onSurfaceVariant,
            fontSize = 12.sp,
        ),
    )
}

class ClusterWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = ClusterWidget()

    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        (context.applicationContext as TalosApp).launchSync(runNow = true)
    }

    override fun onDisabled(context: Context) {
        super.onDisabled(context)
        (context.applicationContext as TalosApp).launchSync()
    }
}
