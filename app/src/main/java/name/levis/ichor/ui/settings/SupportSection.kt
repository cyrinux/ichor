package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.REPO_URL_BASE
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows
import name.levis.ichor.model.notice
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.rememberClusterSupport
import name.levis.ichor.ui.components.text

/** GitHub's issue chooser: bug report or integration request. */
private val REPORT_BUG_URL = "$REPO_URL_BASE${BuildConfig.UPDATE_REPO}/issues/new/choose"

/** Settings → Support: support bundle, activity log, integration requests and bug reports. */
@Composable
fun SupportSection(summary: ContextSummary?, onSupportBundle: () -> Unit, onActivity: () -> Unit, onIntegrations: () -> Unit) {
    SectionTitle(stringResource(R.string.settings_section_support))
    // `talosctl support` reads logs and resources of every chosen node (any role).
    if (summary?.allows(Feature.SUPPORT_BUNDLE) == true) {
        MutedText(stringResource(R.string.settings_support_bundle_desc))
        val support = rememberClusterSupport(TalosFeature.SUPPORT_BUNDLE)
        OutlinedButton(onClick = onSupportBundle, enabled = support.supported, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.settings_support_bundle_open))
        }
        support.notice?.let { InfoNotice(it.text()) }
    }
    // On this device only, whatever the cluster: no role needed.
    MutedText(stringResource(R.string.activity_settings_desc))
    OutlinedButton(onClick = onActivity, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.activity_open))
    }
    // Listing the cluster's API groups needs the admin kubeconfig.
    if (summary?.allows(Feature.WORKLOADS) == true) {
        MutedText(stringResource(R.string.settings_integrations_desc))
        OutlinedButton(onClick = onIntegrations, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.settings_integrations_open))
        }
    }
    val context = LocalContext.current
    OutlinedButton(onClick = { openUrl(context, REPORT_BUG_URL) }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.settings_report_bug))
    }
    StartupTraceSetting(context.applicationContext as TalosApp)
}

/** Opt-in: where a cold start's time goes, in logcat (tag Ichor.Startup), for a bug report. */
@Composable
private fun StartupTraceSetting(app: TalosApp) {
    val enabled by app.uiPreferences.startupTrace.collectAsStateWithLifecycle()
    Card(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.settings_startup_trace), style = MaterialTheme.typography.titleMedium)
                MutedText(stringResource(R.string.settings_startup_trace_summary))
            }
            Switch(checked = enabled, onCheckedChange = app::setStartupTrace, modifier = Modifier.padding(start = 12.dp))
        }
    }
}
