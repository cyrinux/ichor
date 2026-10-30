package name.levis.ichor.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.SMOKE_SETTINGS_SCREEN
import name.levis.ichor.ui.components.pageContent

/** Settings: one row per group (see [SettingsPage]), each opening its own page. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(configs: ConfigRepository, onBack: () -> Unit, onOpen: (SettingsPage) -> Unit) {
    val config by configs.config.collectAsStateWithLifecycle()
    IchorScaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.settings_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).testTag(SMOKE_SETTINGS_SCREEN)) {
            visibleSettingsPages(config?.activeSummary).forEach { page -> SettingsPageRow(page) { onOpen(page) } }
        }
    }
}

@Composable
private fun SettingsPageRow(page: SettingsPage, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(stringResource(page.title)) },
        supportingContent = { Text(stringResource(page.summary)) },
        // The title already names the row: the icons are decoration for TalkBack.
        leadingContent = { Icon(page.icon, contentDescription = null) },
        trailingContent = { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null) },
        modifier = Modifier.clickable(role = Role.Button, onClick = onClick),
    )
}
