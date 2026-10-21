package name.levis.ichor.ui.gitops

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.SopsInfo
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A SOPS-encrypted file in the viewer: who it is encrypted for (each key by its ends), when it
 * was last encrypted, how many values are, and the ones meant to be still in clear. The phone
 * never decrypts it: it is edited on a computer with sops.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SopsCard(info: SopsInfo, modifier: Modifier = Modifier) {
    SectionCard(
        stringResource(R.string.sops_encrypted),
        modifier,
        leading = { Icon(Icons.Outlined.Lock, contentDescription = null) },
    ) {
        if (info.recipients.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                info.recipients.forEach { Tag("${it.type} ${it.short}", mono = true) }
            }
        }
        MutedText(stringResource(R.string.sops_encrypted_values, info.encryptedKeys))
        if (info.lastModified.isNotEmpty()) MutedText(stringResource(R.string.sops_last_modified, info.lastModified))
        if (info.clearKeys.isNotEmpty()) {
            Text(
                stringResource(R.string.sops_clear_keys, info.clearKeys.joinToString()),
                color = LocalStatusColors.current.bad,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        MutedText(stringResource(R.string.sops_edit_elsewhere))
    }
}
