package name.levis.ichor.model

// How a refusal of KubePluginAction (go/ichorgo/plugin_action.go) reads, from the Go error's
// text: the apps match its codes and fixed phrases, never its translated words. In screenshot
// mode Go masks names inside the message; the prefixes stay.

/** A failed plugin action, by what the user can do about it. */
sealed interface PluginActionFailure {
    /** enabledWhen does not hold on the object now (409 ActionDisabled): [reason] is the plugin's disabledReason. */
    data class Disabled(val reason: String) : PluginActionFailure

    /** The object moved between the check and the write (409, pinned): reload and try again. */
    data object Changed : PluginActionFailure

    /** A param was refused; [param] names it, [message] is Go's (English) detail. */
    data class Param(val param: String, val message: String) : PluginActionFailure

    /** The demo cluster runs no action. */
    data object Demo : PluginActionFailure

    /** The API server's 403: the account's role does not allow it. [message] is its detail. */
    data class Forbidden(val message: String) : PluginActionFailure

    /** Anything else, shown as Go said it. */
    data class Other(val message: String) : PluginActionFailure
}

private val DISABLED = Regex("""\(409 ActionDisabled\):\s*(.*)""", RegexOption.DOT_MATCHES_ALL)
private val PARAM = Regex("""parameter "((?:[^"\\]|\\.)*)":\s*(.*)""", RegexOption.DOT_MATCHES_ALL)
private const val CHANGED = "changed since it was checked"
private const val DEMO = "unavailable in the demo cluster"
private const val FORBIDDEN = "Kubernetes API: permission denied:"

/** What [message] (a KubePluginAction error) means; [PluginActionFailure.Other] when it is none of the known refusals. */
fun pluginActionFailure(message: String): PluginActionFailure {
    DISABLED.find(message)?.let { return PluginActionFailure.Disabled(it.groupValues[1].trim()) }
    if (CHANGED in message) return PluginActionFailure.Changed
    PARAM.find(message)?.let { return PluginActionFailure.Param(it.groupValues[1].replace("\\\"", "\""), it.groupValues[2].trim()) }
    if (DEMO in message) return PluginActionFailure.Demo
    if (message.startsWith(FORBIDDEN)) return PluginActionFailure.Forbidden(message.removePrefix(FORBIDDEN).trim())
    return PluginActionFailure.Other(message)
}
