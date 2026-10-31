package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.VisibilityOff
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.SPONSOR_URL
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.update.StoreUpdateState
import name.levis.ichor.update.UpdateState
import name.levis.ichor.util.daysUntil

/**
 * Shown when the client certificate expires within [CERT_WARN_DAYS] days (like the alert).
 * An os:admin can renew it right away; anyone else needs a new talosconfig from an admin.
 */
@Composable
internal fun CertificateBanner(summary: ContextSummary, onIssueConfig: () -> Unit) {
    if (summary.certNotAfter <= 0) return
    val days = daysUntil(summary.certNotAfter)
    if (days > CERT_WARN_DAYS) return
    val canRenew = summary.allows(Feature.ISSUE_CONFIG)
    val count = kotlin.math.abs(days).toInt()
    val text = if (days < 0) {
        pluralStringResource(R.plurals.overview_cert_expired, count, count)
    } else {
        pluralStringResource(R.plurals.overview_cert_expires, count, count)
    }
    val color = if (days < 0) LocalStatusColors.current.bad else LocalStatusColors.current.warn
    val content: @Composable () -> Unit = {
        Column(Modifier.padding(16.dp)) {
            Text(text, style = MaterialTheme.typography.bodyMedium, color = color)
            MutedText(stringResource(if (canRenew) R.string.overview_cert_renew else R.string.overview_cert_ask_admin))
        }
    }
    if (canRenew) IchorCard(onClick = onIssueConfig, modifier = Modifier.fillMaxWidth()) { content() }
    else IchorCard(Modifier.fillMaxWidth()) { content() }
}

/**
 * Reminds that screenshot mode is on, i.e. names and addresses on screen are not the real
 * ones. A small icon rather than a label, so it stays out of the way in the screenshots.
 */
@Composable
internal fun ScreenshotModeIcon(modifier: Modifier = Modifier.padding(end = 4.dp).size(14.dp)) {
    val prefs = (LocalContext.current.applicationContext as TalosApp).uiPreferences
    val mask by prefs.privacyMask.collectAsStateWithLifecycle()
    if (!mask.enabled) return
    Icon(
        Icons.Outlined.VisibilityOff,
        contentDescription = stringResource(R.string.settings_screenshot_mode),
        tint = MaterialTheme.colorScheme.primary,
        modifier = modifier,
    )
}

/** Shown when the daily check found a newer release; opens Settings → Updates. */
@Composable
internal fun UpdateBanner(onClick: () -> Unit) {
    val updates = (LocalContext.current.applicationContext as TalosApp).updateManager
    val state by updates.state.collectAsStateWithLifecycle()
    val available = state as? UpdateState.Available ?: return
    IchorCard(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.overview_update_banner, available.info.version),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(16.dp),
        )
    }
}

/** Play build: an update Play has (declined at launch), being downloaded, or ready to install. */
@Composable
internal fun StoreUpdateBanner() {
    val updates = (LocalContext.current.applicationContext as TalosApp).storeUpdater
    val state by updates.state.collectAsStateWithLifecycle()
    val (text, onClick) = when (state) {
        StoreUpdateState.None -> return
        StoreUpdateState.Available -> R.string.overview_store_update_available to updates::start
        StoreUpdateState.Downloading -> R.string.overview_store_update_downloading to null
        StoreUpdateState.Downloaded -> R.string.overview_store_update_ready to updates::install
    }
    val content: @Composable () -> Unit = {
        Text(
            stringResource(text),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(16.dp),
        )
    }
    if (onClick != null) IchorCard(onClick = onClick, modifier = Modifier.fillMaxWidth()) { content() }
    else IchorCard(Modifier.fillMaxWidth()) { content() }
}

/**
 * Occasional, dismissable ask to support the project (see SupportPrompt for the timing): GitHub
 * Sponsors in the open-source builds, [onFunding] (Play in-app purchases) in the Play build.
 */
@Composable
internal fun SupportCard(onFunding: () -> Unit) {
    val context = LocalContext.current
    val prompt = (context.applicationContext as TalosApp).supportPrompt
    val visible by prompt.visible.collectAsStateWithLifecycle()
    if (!visible) return
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.overview_support_title), style = MaterialTheme.typography.titleSmall)
            MutedText(stringResource(R.string.overview_support_body))
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                IchorTextButton(onClick = prompt::never) { Text(stringResource(R.string.overview_support_never)) }
                IchorTextButton(onClick = prompt::later) { Text(stringResource(R.string.overview_support_later)) }
                IchorTextButton(onClick = {
                    prompt.later()
                    if (BuildConfig.FEATURE_FUNDING) onFunding() else openUrl(context, SPONSOR_URL)
                }) { Text(stringResource(if (BuildConfig.FEATURE_FUNDING) R.string.funding_title else R.string.overview_support_sponsor)) }
            }
        }
    }
}
