package name.levis.ichor.ui.settings

import name.levis.ichor.R
import androidx.compose.ui.res.stringResource
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Gavel
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.NewReleases
import androidx.compose.material.icons.outlined.Tour
import android.widget.Toast
import androidx.compose.runtime.rememberCoroutineScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.ui.userMessage
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import name.levis.ichor.BuildConfig
import name.levis.ichor.data.REPO_URL_BASE
import name.levis.ichor.data.SPONSOR_URL
import name.levis.ichor.data.ICHOR_WEBSITE_URL
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle

private const val LICENSE_PATH = "/blob/main/LICENSE"

@Composable
fun AboutSection(
    onChangelog: () -> Unit,
    onLicenses: () -> Unit,
    onSupportedIntegrations: () -> Unit,
    onFunding: () -> Unit,
    onReplayTour: suspend () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var donating by rememberSaveable { mutableStateOf<Donation?>(null) }
    donating?.let { DonateDialog(it) { donating = null } }
    SectionTitle(stringResource(R.string.about_section))
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(0.dp)) {
            Text(
                stringResource(R.string.about_version, BuildConfig.VERSION_NAME, BuildConfig.VERSION_CODE),
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            Link(Icons.Outlined.NewReleases, stringResource(R.string.changelog_title), onChangelog)
            Link(Icons.Outlined.Code, stringResource(R.string.about_source)) { openUrl(context, REPO_URL_BASE + BuildConfig.UPDATE_REPO) }
            Link(Icons.Outlined.Gavel, stringResource(R.string.about_license)) { openUrl(context, REPO_URL_BASE + BuildConfig.UPDATE_REPO + LICENSE_PATH) }
            Link(Icons.Outlined.Description, stringResource(R.string.about_licenses), onLicenses)
            Link(Icons.Outlined.Extension, stringResource(R.string.supported_integrations_title), onSupportedIntegrations)
            Link(Icons.Outlined.Tour, stringResource(R.string.tour_replay)) {
                scope.launch {
                    try {
                        onReplayTour()
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        Toast.makeText(context, e.userMessage(), Toast.LENGTH_LONG).show()
                    }
                }
            }
            Link(Icons.AutoMirrored.Outlined.OpenInNew, stringResource(R.string.about_website)) { openUrl(context, ICHOR_WEBSITE_URL) }
            if (BuildConfig.FEATURE_FUNDING) Link(Icons.Outlined.FavoriteBorder, stringResource(R.string.funding_title), onFunding)
            if (BuildConfig.DONATIONS) {
                Link(Icons.Outlined.FavoriteBorder, stringResource(R.string.about_sponsor)) { openUrl(context, SPONSOR_URL) }
                DonationRow(Donation.Bitcoin) { donating = Donation.Bitcoin }
                DonationRow(Donation.Ethereum) { donating = Donation.Ethereum }
            }
            MutedText(
                stringResource(R.string.about_icon_credits),
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            MutedText(
                stringResource(R.string.about_disclaimer),
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun Link(icon: ImageVector, label: String, onClick: () -> Unit) {
    IchorTextButton(onClick = onClick, modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp)) {
        Icon(icon, contentDescription = null)
        Text(label, modifier = Modifier.weight(1f).padding(start = 12.dp))
    }
}

fun openUrl(context: Context, url: String) {
    try {
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: android.content.ActivityNotFoundException) {
        // No browser installed: nothing to open the link with.
    }
}

/**
 * Opens [url] in the default browser, never in an app that claims the link: the GitHub app takes
 * github.com/…/issues/new, drops the issue form (its fields and labels) and the CRD bot never
 * sees the request. With no default browser chosen (the system resolver answers), or none at
 * all, it falls back to [openUrl].
 */
fun openInBrowser(context: Context, url: String) {
    val probe = Intent(Intent.ACTION_VIEW, Uri.parse(BROWSER_PROBE_URL)).addCategory(Intent.CATEGORY_BROWSABLE)
    val browser = context.packageManager.resolveActivity(probe, PackageManager.MATCH_DEFAULT_ONLY)?.activityInfo?.packageName
    if (browser == null || browser == SYSTEM_RESOLVER) return openUrl(context, url)
    try {
        context.startActivity(
            Intent(Intent.ACTION_VIEW, Uri.parse(url)).setPackage(browser).addCategory(Intent.CATEGORY_BROWSABLE).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    } catch (_: android.content.ActivityNotFoundException) {
        openUrl(context, url)
    }
}

// A link no app claims, so only browsers answer it.
private const val BROWSER_PROBE_URL = "https://example.com"
private const val SYSTEM_RESOLVER = "android"
