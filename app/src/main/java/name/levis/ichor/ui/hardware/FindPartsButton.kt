package name.levis.ichor.ui.hardware

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Build
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichorgo.Ichorgo

/**
 * "Find spare parts" for one part ([kind] system, memory or disk): opens the Ichor site's parts
 * page in the browser with the part's maker, model and serial after the #, where the vendor's
 * page and a web search are one tap away. The app contacts no vendor. Nothing when the part
 * is virtual (VIRTUAL-DISK, QEMU, VMware...) or has neither a model nor a serial (firmware
 * fillers count as blank).
 */
@Composable
internal fun FindPartsButton(kind: String, manufacturer: String, model: String, serial: String) {
    val url = remember(kind, manufacturer, model, serial) { Ichorgo.partsLookupLink(kind, manufacturer, model, serial) }
    if (url.isBlank()) return
    val context = LocalContext.current
    val label = stringResource(R.string.hardware_find_parts)
    val hint = stringResource(R.string.hardware_find_parts_hint)
    IchorTextButton(onClick = { openUrl(context, url) }, modifier = Modifier.semantics { contentDescription = "$label. $hint" }) {
        Icon(Icons.Outlined.Build, contentDescription = null, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Text(label)
    }
}
