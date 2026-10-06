package name.levis.ichor.ui.components

import android.content.Context
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.flow.Flow

/**
 * A toast for each item of [results]: [message] gives its text and whether it reports a
 * failure (shown longer), or null for an item that needs no toast.
 */
@Composable
fun <R> ResultToasts(results: Flow<R>, message: (Context, R) -> Pair<String, Boolean>?) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
            val (text, failed) = message(context, r) ?: return@collect
            Toast.makeText(context, text, if (failed) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }
}
