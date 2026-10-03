package name.levis.ichor.security

import android.content.ActivityNotFoundException
import android.content.Intent
import android.provider.Settings
import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Fingerprint
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import name.levis.ichor.R

/** How long the "protected" state shows before the app opens. */
private const val DONE_PAUSE_MS = 1_100L

/** Delay between the entrances of the feature rows. */
private const val STAGGER_MS = 120L

/**
 * Shown instead of the app while a real cluster is stored without the app lock (right after
 * the first import, or on updating from a version where the lock was optional). The only
 * ways forward are enabling the lock or deleting the stored config.
 */
@Composable
fun LockOnboarding(onEnabled: () -> Unit, onDeleteConfig: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var available by remember { mutableStateOf(canAuthenticate(context)) }
    var error by remember { mutableStateOf<String?>(null) }
    var done by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }

    // Back from the system settings with a screen lock set up.
    LifecycleResumeEffect(Unit) {
        available = canAuthenticate(context)
        onPauseOrDispose {}
    }
    LaunchedEffect(done) {
        if (!done) return@LaunchedEffect
        delay(DONE_PAUSE_MS)
        onEnabled()
    }
    BackHandler { context.findFragmentActivity()?.moveTaskToBack(true) }

    fun enable() {
        val activity = context.findFragmentActivity() ?: return
        scope.launch {
            when (val result = authenticate(activity, context.getString(R.string.settings_auth_enable_lock))) {
                AuthResult.Success -> done = true
                is AuthResult.Failure -> error = result.message
            }
        }
    }

    Surface(Modifier.fillMaxSize()) {
        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).safeDrawingPadding().padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Column(Modifier.widthIn(max = 480.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                ShieldHero(done)
                Spacer(Modifier.height(24.dp))
                AnimatedContent(done, label = "title") { protected ->
                    Text(
                        stringResource(if (protected) R.string.lock_onboarding_done else R.string.lock_onboarding_title),
                        style = MaterialTheme.typography.headlineSmall,
                        textAlign = TextAlign.Center,
                    )
                }
                Spacer(Modifier.height(8.dp))
                Text(
                    stringResource(R.string.lock_onboarding_intro),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(24.dp))
                Features()
                Spacer(Modifier.height(28.dp))
                AnimatedVisibility(!done, exit = fadeOut()) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        if (available) {
                            Button(onClick = ::enable, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                                Icon(Icons.Outlined.Fingerprint, contentDescription = null)
                                Text(stringResource(R.string.settings_auth_enable_lock), modifier = Modifier.padding(start = 8.dp))
                            }
                            error?.let {
                                Spacer(Modifier.height(8.dp))
                                Text(it, color = MaterialTheme.colorScheme.error, textAlign = TextAlign.Center)
                            }
                        } else {
                            NoScreenLock(onOpenSettings = { openSecuritySettings(context) })
                        }
                        TextButton(onClick = { confirmDelete = true }) {
                            Text(stringResource(R.string.lock_onboarding_delete))
                        }
                    }
                }
            }
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text(stringResource(R.string.lock_onboarding_delete_title)) },
            text = { Text(stringResource(R.string.lock_onboarding_delete_body)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    onDeleteConfig()
                }) { Text(stringResource(R.string.lock_delete_config), color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** A shield with slow pulsing rings; it turns into a check once the lock is on. */
@Composable
private fun ShieldHero(done: Boolean) {
    val pulse = rememberInfiniteTransition(label = "pulse")
    val wave by pulse.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(2_400, easing = LinearEasing), RepeatMode.Restart),
        label = "wave",
    )
    val primary = MaterialTheme.colorScheme.primary
    Box(Modifier.size(160.dp), contentAlignment = Alignment.Center) {
        // Two rings half a period apart.
        listOf(wave, (wave + 0.5f) % 1f).forEach { phase ->
            Box(
                Modifier.size(96.dp).scale(1f + phase * 0.65f).alpha((1f - phase) * 0.35f)
                    .background(primary, CircleShape),
            )
        }
        Box(
            Modifier.size(96.dp).background(MaterialTheme.colorScheme.primaryContainer, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            AnimatedContent(
                done,
                transitionSpec = { (scaleIn() + fadeIn()) togetherWith (scaleOut() + fadeOut()) },
                label = "shield",
            ) { protected ->
                Icon(
                    if (protected) Icons.Outlined.VerifiedUser else Icons.Outlined.Shield,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onPrimaryContainer,
                    modifier = Modifier.size(48.dp),
                )
            }
        }
    }
}

private data class Feature(val icon: ImageVector, val title: Int, val description: Int)

private val FEATURES = listOf(
    Feature(Icons.Outlined.Fingerprint, R.string.lock_onboarding_unlock, R.string.lock_onboarding_unlock_desc),
    Feature(Icons.Outlined.PowerSettingsNew, R.string.lock_onboarding_actions, R.string.lock_onboarding_actions_desc),
    Feature(Icons.Outlined.VisibilityOff, R.string.lock_onboarding_private, R.string.lock_onboarding_private_desc),
)

/** What the lock does, the rows sliding in one after the other. */
@Composable
private fun Features() {
    var shown by remember { mutableStateOf(0) }
    LaunchedEffect(Unit) {
        while (shown < FEATURES.size) {
            delay(STAGGER_MS)
            shown++
        }
    }
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        FEATURES.forEachIndexed { index, feature ->
            AnimatedVisibility(index < shown, enter = fadeIn() + slideInVertically { it / 2 }) {
                FeatureRow(feature)
            }
        }
    }
}

@Composable
private fun FeatureRow(feature: Feature) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Box(
            Modifier.size(44.dp).background(MaterialTheme.colorScheme.secondaryContainer, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Icon(feature.icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSecondaryContainer)
        }
        Column(Modifier.padding(start = 16.dp)) {
            Text(stringResource(feature.title), style = MaterialTheme.typography.titleSmall)
            Text(
                stringResource(feature.description),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun NoScreenLock(onOpenSettings: () -> Unit) {
    Card(
        Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer),
    ) {
        Column(Modifier.padding(16.dp)) {
            Text(
                stringResource(R.string.lock_onboarding_no_lock),
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onErrorContainer,
            )
            Text(
                stringResource(R.string.settings_auth_needs_lock),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onErrorContainer,
            )
            Spacer(Modifier.height(12.dp))
            Button(onClick = onOpenSettings, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.lock_onboarding_open_settings))
            }
        }
    }
}

private fun openSecuritySettings(context: android.content.Context) {
    val intent = Intent(Settings.ACTION_SECURITY_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    try {
        context.startActivity(intent)
    } catch (_: ActivityNotFoundException) {
        context.startActivity(Intent(Settings.ACTION_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
