package name.levis.ichor.ui.settings

import name.levis.ichor.R
import androidx.compose.ui.res.stringResource
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.NewReleases
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import name.levis.ichor.data.TALOS_URL
import name.levis.ichor.ui.components.SectionTitle

@Composable
fun AboutSection(onChangelog: () -> Unit) {
    val context = LocalContext.current
    var donating by rememberSaveable { mutableStateOf<Donation?>(null) }
    donating?.let { DonateDialog(it) { donating = null } }
    SectionTitle(stringResource(R.string.about_section))
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(0.dp)) {
            Text(
                stringResource(R.string.about_version, BuildConfig.VERSION_NAME, BuildConfig.VERSION_CODE),
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            Link(Icons.Outlined.NewReleases, stringResource(R.string.changelog_title), onChangelog)
            Link(Icons.Outlined.Code, stringResource(R.string.about_source)) { openUrl(context, REPO_URL_BASE + BuildConfig.UPDATE_REPO) }
            Link(Icons.AutoMirrored.Outlined.OpenInNew, stringResource(R.string.about_talos)) { openUrl(context, TALOS_URL) }
            Link(Icons.Outlined.FavoriteBorder, stringResource(R.string.about_sponsor)) { openUrl(context, SPONSOR_URL) }
            DonationRow(Donation.Bitcoin) { donating = Donation.Bitcoin }
            DonationRow(Donation.Ethereum) { donating = Donation.Ethereum }
            Text(
                stringResource(R.string.about_disclaimer),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun Link(icon: ImageVector, label: String, onClick: () -> Unit) {
    TextButton(onClick = onClick, modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp)) {
        Icon(icon, contentDescription = null)
        Text(label, modifier = Modifier.weight(1f).padding(start = 12.dp))
    }
}

fun openUrl(context: Context, url: String) {
    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}
