package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.QrCode2
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.issueconfig.QrCode
import name.levis.ichor.ui.theme.LocalStatusColors

/** One value of a boot card, under a tab label when the card holds several (docker, podman). */
data class BootValue(val label: String, val text: String)

/**
 * A boot medium: [title], an optional [hint], its [values] (tabs when more than one) in
 * monospace, and Copy, Share and QR code for the shown one. [sensitive]: the value carries a
 * secret (Omni's join token), kept out of the clipboard preview and flagged on the card.
 */
@Composable
fun BootMediaCard(title: String, values: List<BootValue>, hint: String? = null, sensitive: Boolean = false) {
    var tab by rememberSaveable(title) { mutableIntStateOf(0) }
    var qr by rememberSaveable(title) { mutableStateOf(false) }
    val shown = values[tab.coerceIn(values.indices)]
    SectionCard(title) {
        hint?.let { MutedText(it) }
        if (sensitive) Text(stringResource(R.string.boot_media_sensitive), color = LocalStatusColors.current.warn, style = MaterialTheme.typography.bodySmall)
        if (values.size > 1) BootTabs(values.map { it.label }, tab) { tab = it }
        SelectionContainer {
            Text(shown.text, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
        }
        BootActions(title, shown, sensitive, onQr = { qr = true })
    }
    if (qr) BootQrSheet(title, shown.text, onDismiss = { qr = false })
}

@Composable
private fun BootTabs(labels: List<String>, selected: Int, onSelect: (Int) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        labels.forEachIndexed { i, label ->
            SegmentedButton(
                selected = selected == i,
                onClick = { onSelect(i) },
                shape = SegmentedButtonDefaults.itemShape(i, labels.size),
                icon = {},
            ) { Text(label) }
        }
    }
}

@Composable
private fun BootActions(title: String, value: BootValue, sensitive: Boolean, onQr: () -> Unit) {
    val context = LocalContext.current
    val name = listOf(title, value.label).filter { it.isNotBlank() }.joinToString(" · ")
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
        TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.boot_media_copy, name), onClick = {
            copyToClipboard(context, name, value.text, sensitive = sensitive)
        })
        TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.boot_media_share, name), onClick = {
            shareText(context, value.text, name)
        })
        TooltipIconButton(Icons.Outlined.QrCode2, stringResource(R.string.boot_media_qr, name), onClick = onQr)
    }
}

/** [text] as a QR code for the laptop next to the machine, or why it does not fit one. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun BootQrSheet(title: String, text: String, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(title, style = MaterialTheme.typography.titleLarge)
            QrCode(
                text = text,
                contentDescription = stringResource(R.string.boot_media_qr, title),
                onTooLarge = { MutedText(stringResource(R.string.boot_media_qr_too_large)) },
            )
            IchorTextButton(onClick = onDismiss, modifier = Modifier.align(Alignment.End)) { Text(stringResource(R.string.common_close)) }
        }
    }
}
