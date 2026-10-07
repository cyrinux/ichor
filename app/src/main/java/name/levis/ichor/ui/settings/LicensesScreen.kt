package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.mikepenz.aboutlibraries.ui.compose.android.produceLibraries
import com.mikepenz.aboutlibraries.ui.compose.m3.LibrariesContainer
import name.levis.ichor.R
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.pageContent

/** Third-party components and their licenses, generated at build time by the AboutLibraries plugin. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LicensesScreen(onBack: () -> Unit) {
    val libraries by produceLibraries(R.raw.aboutlibraries)
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.about_licenses)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        LibrariesContainer(libraries, Modifier.pageContent(padding).fillMaxSize())
    }
}
