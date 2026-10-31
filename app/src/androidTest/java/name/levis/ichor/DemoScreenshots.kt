package name.levis.ichor

import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.ui.components.SMOKE_KUBE_HOME
import name.levis.ichor.ui.nav.Routes
import name.levis.ichor.ui.nav.TestRoute
import name.levis.ichor.ui.node.CGROUPS_TAB
import name.levis.ichor.ui.settings.SettingsPage
import name.levis.ichor.ui.workloads.NETPERF_TAB
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The website and store screenshots, taken from the offline demo on an emulator
 * (scripts/ci-screenshots.sh, workflow screenshots.yml). Not part of the smoke test: ui-smoke.sh
 * leaves this class out. Each screen is a raw screencap in [DIR]; the script crops the system
 * bars off and writes docs/screenshots/ and the Play Store copies.
 */
@RunWith(AndroidJUnit4::class)
class DemoScreenshots {
    @get:Rule
    val compose = createEmptyComposeRule()

    @Test
    fun captureTheDemo() {
        DemoWalk(compose, DIR).captureListingScreens()
    }

    /** The screens of the website's feature cards, each opened directly from the demo. */
    @Test
    fun captureTheFeatures() {
        val walk = DemoWalk(compose, DIR)
        FEATURE_SHOTS.forEach { (name, route) ->
            walk.openDemo().use { scenario ->
                scenario.onActivity { TestRoute.nav!!.navigate(route) }
                walk.capture("feature-$name", FEATURE_SETTLE)
            }
        }
        walk.open("ichor://demo/kube").use {
            walk.waitFor(hasTestTag(SMOKE_KUBE_HOME))
            walk.capture("feature-kube-home", FEATURE_SETTLE)
        }
    }

    private companion object {
        /** Where the raw captures go: the shell user writes here, ci-screenshots.sh pulls it. */
        const val DIR = "/data/local/tmp/ichor-screenshots"
        const val FEATURE_SETTLE = 5_000L

        /** Names match scripts/ci-screenshots.sh and docs/screenshots/feature-NAME.webp. */
        val FEATURE_SHOTS = listOf(
            "plugins" to Routes.settingsPage(SettingsPage.PLUGINS),
            "gitops" to Routes.gitOpsSource("argocd", "Application", "argocd", "grafana", 0),
            "capacity" to Routes.KUBE_CAPACITY,
            "argocd-diff" to Routes.argoDiff("argocd", "grafana"),
            "argocd" to Routes.ARGO_CD,
            "metrics" to Routes.metrics(),
            "data-services" to Routes.dataServices(),
            "castai" to Routes.dataServices(DataServiceKind.CASTAI),
            "cgroups" to "node/tabs?addr=192.0.2.10&host=demo-cp-1&tab=$CGROUPS_TAB",
            "apps" to Routes.apps(),
            "workloads" to Routes.workloads(KubeFocus(0)),
            "pods" to Routes.workloads(KubeFocus(1)),
            "cronjobs" to Routes.workloads(KubeFocus(2)),
            "netperf" to Routes.workloads(KubeFocus(NETPERF_TAB)),
            "drift" to Routes.INSIGHTS,
            "import" to Routes.IMPORT,
        )
    }
}
