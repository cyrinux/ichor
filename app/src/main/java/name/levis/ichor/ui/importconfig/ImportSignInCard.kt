package name.levis.ichor.ui.importconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.SignInFailure
import name.levis.ichor.model.SignInProgress
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The clusters found in a cloud account being signed in after the import, one after the other: how many, a bar and
 * the one being signed in.
 */
@Composable
fun SignInProgressCard(progress: SignInProgress) {
    Box(Modifier.fillMaxSize().padding(16.dp), Alignment.Center) {
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.kube_import_adding_count, progress.total.toString()), style = MaterialTheme.typography.titleMedium)
                LinearProgressIndicator(
                    progress = { if (progress.total > 0) progress.done.toFloat() / progress.total else 0f },
                    modifier = Modifier.fillMaxWidth(),
                )
                if (progress.current.isNotEmpty()) {
                    MutedText(stringResource(R.string.kube_import_signing_in, progress.current), maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                if (progress.failed.isNotEmpty()) {
                    Text(progress.failed.joinToString(", "), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

/** The clusters are added, but [failed] could not be signed in: they say "Sign in" on their home. */
@Composable
fun SignInFailedCard(failed: List<SignInFailure>, onDone: () -> Unit) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.kube_import_signin_failed), style = MaterialTheme.typography.titleMedium)
        failed.forEach { failure ->
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(failure.name, style = MaterialTheme.typography.titleSmall)
                    Text(failure.error, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        Button(onClick = onDone, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
    }
}
