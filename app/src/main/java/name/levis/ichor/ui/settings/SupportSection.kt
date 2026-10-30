package name.levis.ichor.ui.settings

import name.levis.ichor.setStartupTrace
import name.levis.ichor.setDiagPersist
import android.os.Build
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import name.levis.ichor.ui.design.IchorCard
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
fun SupportSection(
    summary: ContextSummary?,
    onSupportBundle: () -> Unit,
    onActivity: () -> Unit,
    onIntegrations: () -> Unit,
    onDiagnostics: () -> Unit,
) {
    SectionTitle(stringResource(R.string.settings_section_support))
    // The core could not open its encrypted store: say so rather than lose the log quietly.
    val dataStoreProblem by (LocalContext.current.applicationContext as TalosApp).dataStoreProblem.collectAsStateWithLifecycle()
    dataStoreProblem?.let { InfoNotice(stringResource(R.string.settings_support_data_store_off, it)) }
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
    OutlinedButton(onClick = { openInBrowser(context, REPORT_BUG_URL) }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.settings_report_bug))
    }
    DiagnosticsSetting(context.applicationContext as TalosApp, onDiagnostics)
    StartupTraceSetting(context.applicationContext as TalosApp)
}

/**
 * The redacted diagnostics log and the zip made from it: kept across launches or not (off by
 * default), and "Export diagnostics…". Nothing leaves the phone unless the user shares it.
 */
@Composable
private fun DiagnosticsSetting(app: TalosApp, onDiagnostics: () -> Unit) {
    val persist by app.uiPreferences.diagPersist.collectAsStateWithLifecycle()
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.settings_diagnostics_title), style = MaterialTheme.typography.titleMedium)
            MutedText(stringResource(R.string.settings_diagnostics_summary))
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) MutedText(stringResource(R.string.diagnostics_exits_unsupported))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.settings_diagnostics_persist), style = MaterialTheme.typography.bodyLarge)
                    MutedText(stringResource(R.string.settings_diagnostics_persist_summary))
                }
                Switch(checked = persist, onCheckedChange = app::setDiagPersist, modifier = Modifier.padding(start = 12.dp))
            }
            OutlinedButton(onClick = onDiagnostics, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.settings_diagnostics_open))
            }
        }
    }
}

/** Opt-in: where a cold start's time goes, in logcat (tag Ichor.Startup), for a bug report. */
@Composable
private fun StartupTraceSetting(app: TalosApp) {
    val enabled by app.uiPreferences.startupTrace.collectAsStateWithLifecycle()
    IchorCard(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.settings_startup_trace), style = MaterialTheme.typography.titleMedium)
                MutedText(stringResource(R.string.settings_startup_trace_summary))
            }
            Switch(checked = enabled, onCheckedChange = app::setStartupTrace, modifier = Modifier.padding(start = 12.dp))
        }
    }
}
