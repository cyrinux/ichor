package name.levis.talosmobile.ui.settings

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
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import name.levis.talosmobile.BuildConfig
import name.levis.talosmobile.data.REPO_URL_BASE
import name.levis.talosmobile.data.SPONSOR_URL
import name.levis.talosmobile.data.TALOS_URL
import name.levis.talosmobile.ui.components.SectionTitle

@Composable
fun AboutSection() {
    val context = LocalContext.current
    SectionTitle("About")
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(0.dp)) {
            Text(
                "Talos Viewer ${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})",
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            Link(Icons.Outlined.Code, "Source code on GitHub") { openUrl(context, REPO_URL_BASE + BuildConfig.UPDATE_REPO) }
            Link(Icons.AutoMirrored.Outlined.OpenInNew, "Talos Linux (talos.dev)") { openUrl(context, TALOS_URL) }
            Link(Icons.Outlined.FavoriteBorder, "Sponsor on GitHub") { openUrl(context, SPONSOR_URL) }
            Text(
                "Not affiliated with Sidero Labs. Talos is a trademark of Sidero Labs, Inc.",
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
