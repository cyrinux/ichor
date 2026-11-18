package name.levis.ichor.ui

import android.content.Context
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.R
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.BastionProblem
import name.levis.ichor.model.bastionProblem
import name.levis.ichor.model.signInMethodName
import name.levis.ichor.model.signInNeeded

/**
 * Text produced outside the UI (view models, managers) and resolved with the current
 * locale only when shown, so a language change also applies to pending messages.
 */
sealed interface UiText {
    /** Verbatim text, e.g. an error message from the Go core or a node. */
    data class Raw(val text: String) : UiText

    /** A string resource; [args] may themselves be [UiText]. */
    class Res(@StringRes val id: Int, vararg val args: Any) : UiText

    /**
     * A call refused because the kubeconfig cluster needs a sign-in ([method] may be "", [reason]
     * is what the core said): screens offer to sign in rather than show the core's code.
     */
    data class SignInRequired(val method: String, val reason: String = "") : UiText

    /** A call through the SSH bastion failed ([problem]): screens offer the bastion settings. */
    data class Bastion(val problem: BastionProblem) : UiText

    fun resolve(context: Context): String = when (this) {
        is Raw -> text
        is Res -> context.getString(id, *args.map { if (it is UiText) it.resolve(context) else it }.toTypedArray())
        is SignInRequired -> signInMethodName(method)?.let { context.getString(R.string.kube_signin_required_error, context.getString(it)) }
            ?: method.takeIf { it.isNotEmpty() }?.let { context.getString(R.string.kube_signin_required_error, it) }
            ?: context.getString(R.string.kube_signin_required_error_plain)
        is Bastion -> context.getString(problem.text)
    }
}

/** The app, to localize texts made where no screen is at hand (see [Throwable.userMessage]); set at start. */
object AppTexts {
    @Volatile
    var context: Context? = null
}

/**
 * A Go core error [message] as the user reads it: a cluster that needs a sign-in is said in
 * the app's language, never with the core's code. Anything else stays as it is.
 */
fun goErrorText(message: String): String {
    val text = bastionProblem(message)?.let { UiText.Bastion(it) }
        ?: signInNeeded(message)?.let { UiText.SignInRequired(it.method, it.reason) }
        ?: return message
    return AppTexts.context?.let { text.resolve(AppLocale.wrap(it)) } ?: if (text is UiText.Bastion) message else SIGN_IN_REQUIRED_FALLBACK
}

/** Only before the app started (unit tests): the core's code must not show either way. */
private const val SIGN_IN_REQUIRED_FALLBACK = "Sign in to this cluster to continue"

@Composable
fun UiText.asString(): String = resolve(LocalContext.current)

/** An exception whose message is shown to the user, translated. */
open class LocalizedException(val text: UiText) : Exception(text.toString())

fun Throwable.uiText(): UiText = (this as? LocalizedException)?.text
    ?: bastionProblem(message)?.let { UiText.Bastion(it) }
    ?: signInNeeded(message)?.let { UiText.SignInRequired(it.method, it.reason) }
    ?: UiText.Raw(userMessage())

/** The sentence of a bastion [BastionProblem]: what failed and where to look. */
@get:StringRes
val BastionProblem.text: Int
    get() = when (this) {
        BastionProblem.UNREACHABLE -> R.string.bastion_problem_unreachable
        BastionProblem.HOST_KEY -> R.string.bastion_problem_host_key
        BastionProblem.AUTH -> R.string.bastion_problem_auth
        BastionProblem.KEY -> R.string.bastion_problem_key
        BastionProblem.FORWARD -> R.string.bastion_problem_forward
    }
