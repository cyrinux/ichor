package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

/** KubePluginAction's refusals as go/ichorgo/plugin_action.go words them. */
class PluginActionErrorTest {
    @Test
    fun disabledCarriesTheReason() {
        assertEquals(
            PluginActionFailure.Disabled("Already asleep"),
            pluginActionFailure("Kubernetes API (409 ActionDisabled): Already asleep"),
        )
    }

    @Test
    fun changedSinceChecked() {
        assertEquals(
            PluginActionFailure.Changed,
            pluginActionFailure("Kubernetes API (409 Conflict): SleepInfo weekend changed since it was checked: refresh and try again"),
        )
    }

    @Test
    fun paramErrorsNameTheParam() {
        // As ActionParams returns it, and wrapped as a 422 when the body is rendered.
        assertEquals(
            PluginActionFailure.Param("size", "want an integer, got \"big\""),
            pluginActionFailure("parameter \"size\": want an integer, got \"big\""),
        )
        assertEquals(
            PluginActionFailure.Param("mode", "want one of fast, safe, got \"x\""),
            pluginActionFailure("Kubernetes API (422 Invalid): parameter \"mode\": want one of fast, safe, got \"x\""),
        )
        assertEquals(PluginActionFailure.Param("tier", "is required"), pluginActionFailure("parameter \"tier\": is required"))
    }

    @Test
    fun demoRefuses() {
        assertEquals(PluginActionFailure.Demo, pluginActionFailure("this action is unavailable in the demo cluster"))
    }

    @Test
    fun forbidden() {
        assertEquals(
            PluginActionFailure.Forbidden("sleepinfos.kube-green.com \"weekend\" is forbidden"),
            pluginActionFailure("Kubernetes API: permission denied: sleepinfos.kube-green.com \"weekend\" is forbidden"),
        )
    }

    @Test
    fun anythingElseAsSaid() {
        val message = "Kubernetes API: not found: sleepinfos.kube-green.com \"weekend\" not found"
        assertEquals(PluginActionFailure.Other(message), pluginActionFailure(message))
        assertEquals(PluginActionFailure.Other(""), pluginActionFailure(""))
    }
}
