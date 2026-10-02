package name.levis.ichor.ui

import android.content.Context
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

/**
 * Text produced outside the UI (view models, managers) and resolved with the current
 * locale only when shown, so a language change also applies to pending messages.
 */
sealed interface UiText {
    /** Verbatim text, e.g. an error message from the Go core or a node. */
    data class Raw(val text: String) : UiText

    /** A string resource; [args] may themselves be [UiText]. */
    class Res(@StringRes val id: Int, vararg val args: Any) : UiText

    fun resolve(context: Context): String = when (this) {
        is Raw -> text
        is Res -> context.getString(id, *args.map { if (it is UiText) it.resolve(context) else it }.toTypedArray())
    }
}

@Composable
fun UiText.asString(): String = resolve(LocalContext.current)

/** An exception whose message is shown to the user, translated. */
open class LocalizedException(val text: UiText) : Exception(text.toString())

fun Throwable.uiText(): UiText = (this as? LocalizedException)?.text ?: UiText.Raw(userMessage())
