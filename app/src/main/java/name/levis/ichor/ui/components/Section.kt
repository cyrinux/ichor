package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import name.levis.ichor.model.versionNotice
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/** One independently loaded part of a screen, so a failure does not hide the other parts. */
sealed interface Section<out T> {
    data object Loading : Section<Nothing>
    data class Ok<T>(val value: T) : Section<T>
    data class Failed(val message: String) : Section<Nothing>
}

/** Runs [block] into a [Section]; cancellation is rethrown. */
suspend fun <T> sectionOf(block: suspend () -> T): Section<T> = try {
    Section.Ok(block())
} catch (e: kotlinx.coroutines.CancellationException) {
    throw e
} catch (e: Throwable) {
    Section.Failed(e.userMessage())
}

/** An inline error; "this Talos version cannot do that" is shown as muted information instead. */
@Composable
fun InlineError(message: String, modifier: Modifier = Modifier) {
    val notice = versionNotice(message)
    if (notice != null) {
        InfoNotice(notice.text(), modifier)
    } else {
        Text(message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall, modifier = modifier)
    }
}

@Composable
fun <T> SectionBody(section: Section<T>, content: @Composable (T) -> Unit) {
    when (section) {
        Section.Loading -> LinearProgressIndicator(Modifier.fillMaxWidth())
        is Section.Failed -> InlineError(section.message)
        is Section.Ok -> content(section.value)
    }
}
