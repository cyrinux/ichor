package name.levis.ichor.widget

import android.appwidget.AppWidgetManager
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.glance.appwidget.updateAll
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.monitor.clusterFingerprints
import name.levis.ichor.monitor.monitoredContexts
import name.levis.ichor.ui.theme.TalosTheme

/**
 * Picks the cluster a widget shows, when it is placed (before Android 12) or reconfigured: one of
 * the clusters checked in the background, or the cluster on screen (the default). Backing out
 * keeps the widget as it was.
 */
class WidgetConfigActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val widgetId = intent?.extras?.getInt(AppWidgetManager.EXTRA_APPWIDGET_ID, AppWidgetManager.INVALID_APPWIDGET_ID)
            ?: AppWidgetManager.INVALID_APPWIDGET_ID
        // Placed either way: cancelling would take the widget off the home screen.
        setResult(RESULT_OK, Intent().putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, widgetId))
        if (widgetId == AppWidgetManager.INVALID_APPWIDGET_ID) {
            finish()
            return
        }
        val app = application as TalosApp
        setContent {
            val themeMode by app.uiPreferences.themeMode.collectAsStateWithLifecycle()
            // The configs may still be sealed (security key): then only "the cluster on screen" is offered.
            val stored by produceState(app.configRepository.config.value) {
                value = value ?: runCatching { app.configRepository.load() }.getOrNull()
            }
            val unwatched by app.unwatchedClusters.fingerprints.collectAsStateWithLifecycle()
            val names by app.clusterNames.names.collectAsStateWithLifecycle()
            val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
            val choices by app.widgetClusters.choices.collectAsStateWithLifecycle()
            // Each with the fingerprints of all its contexts: the widget may have been set on another one.
            val clusters = stored?.let { s -> monitoredContexts(s.summary, s.activeContext, unwatched).map { it to clusterFingerprints(s.summary, it) } }.orEmpty()
            TalosTheme(themeMode) {
                Surface(Modifier.fillMaxSize()) {
                    WidgetClusterPicker(
                        clusters = clusters,
                        labels = ClusterLabels(names, mask.enabled),
                        selected = choices[widgetId],
                        onPick = { fingerprint -> pick(app, widgetId, fingerprint) },
                    )
                }
            }
        }
    }

    private fun pick(app: TalosApp, widgetId: Int, fingerprint: String?) {
        app.widgetClusters.set(widgetId, fingerprint)
        lifecycleScope.launch {
            ClusterWidget().updateAll(app)
            finish()
        }
    }
}

@Composable
private fun WidgetClusterPicker(
    clusters: List<Pair<ContextSummary, List<String>>>,
    labels: ClusterLabels,
    selected: String?,
    onPick: (String?) -> Unit,
) {
    Column(Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(vertical = 16.dp)) {
        Text(
            stringResource(R.string.widget_config_title),
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
        )
        PickerRow(
            title = stringResource(R.string.widget_config_active),
            detail = stringResource(R.string.widget_config_active_desc),
            selected = selected == null || clusters.none { (_, own) -> selected in own },
            onClick = { onPick(null) },
        )
        clusters.filter { (cluster, _) -> cluster.fingerprint.isNotBlank() }.forEach { (cluster, own) ->
            PickerRow(
                title = labels.of(cluster),
                detail = null,
                selected = selected in own,
                onClick = { onPick(cluster.fingerprint) },
            )
        }
    }
}

@Composable
private fun PickerRow(title: String, detail: String?, selected: Boolean, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(title) },
        supportingContent = detail?.let { { Text(it) } },
        leadingContent = { RadioButton(selected = selected, onClick = null) },
        modifier = Modifier.selectable(selected = selected, role = Role.RadioButton, onClick = onClick),
    )
}
