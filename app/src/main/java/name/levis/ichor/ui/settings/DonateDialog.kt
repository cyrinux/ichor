package name.levis.ichor.ui.settings

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalanceWallet
import androidx.compose.material.icons.outlined.CurrencyBitcoin
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.BTC_ADDRESS
import name.levis.ichor.data.ETH_ADDRESS
import name.levis.ichor.ui.issueconfig.QrCode

/** A crypto donation target: [scheme] makes the BIP21 / EIP-681 style URI wallets understand. */
enum class Donation(val scheme: String, val address: String, val label: Int, val icon: ImageVector) {
    Bitcoin("bitcoin", BTC_ADDRESS, R.string.about_donate_btc, Icons.Outlined.CurrencyBitcoin),
    Ethereum("ethereum", ETH_ADDRESS, R.string.about_donate_eth, Icons.Outlined.AccountBalanceWallet),
}

val Donation.uri: String get() = "$scheme:$address"

/** Keeps both ends of [address] visible, which is what people compare. */
fun middleEllipsis(address: String, keep: Int = 8): String =
    if (address.length <= 2 * keep + 1) address else address.take(keep) + "…" + address.takeLast(keep)

@Composable
fun DonationRow(donation: Donation, onClick: () -> Unit) {
    TextButton(onClick = onClick, modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp)) {
        Icon(donation.icon, contentDescription = null)
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(stringResource(donation.label))
            Text(
                middleEllipsis(donation.address),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Clip,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
fun DonateDialog(donation: Donation, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val address = donation.address
    val uri = donation.uri
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(donation.label)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                QrCode(
                    text = uri,
                    contentDescription = stringResource(R.string.about_donate_qr),
                    onTooLarge = {},
                    modifier = Modifier.widthIn(max = 240.dp),
                )
                SelectionContainer {
                    Text(address, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { openWallet(context, uri) }) { Text(stringResource(R.string.about_donate_open_wallet)) }
        },
        dismissButton = {
            TextButton(onClick = { copyAddress(context, address) }) { Text(stringResource(R.string.about_donate_copy)) }
        },
    )
}

private fun copyAddress(context: Context, address: String) {
    context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText(address, address))
    Toast.makeText(context, R.string.about_donate_copied, Toast.LENGTH_SHORT).show()
}

private fun openWallet(context: Context, uri: String) {
    try {
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(uri)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: ActivityNotFoundException) {
        Toast.makeText(context, R.string.about_donate_no_wallet, Toast.LENGTH_SHORT).show()
    }
}
