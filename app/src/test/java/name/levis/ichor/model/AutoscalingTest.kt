package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AutoscalingTest {

    private val demo = TalosJson.decodeFromString(AutoscalingReport.serializer(), """
{"hpa":{"installed":true,"version":"v2","error":"","truncated":false,"items":[{"namespace":"billing","name":"invoices","target":{"kind":"Deployment","name":"invoices"},"min":1,"max":4,"current":1,"desired":1,"metrics":[{"type":"Resource","name":"cpu","current":"-","target":"75%"}],"conditions":{"ableToScale":true,"scalingActive":false,"scalingLimited":false,"message":"the HPA was unable to compute the replica count: unable to get metrics for resource cpu"},"atMax":false,"lastScaleAt":0,"state":"notReady"},{"namespace":"shop","name":"web","target":{"kind":"Deployment","name":"web"},"min":2,"max":6,"current":6,"desired":6,"metrics":[{"type":"Resource","name":"cpu","current":"94%","target":"70%"}],"conditions":{"ableToScale":true,"scalingActive":true,"scalingLimited":true,"message":"the desired replica count is more than the maximum replica count"},"atMax":true,"lastScaleAt":1760000000000,"state":"atMax"},{"namespace":"shop","name":"api","target":{"kind":"Deployment","name":"api"},"min":3,"max":10,"current":3,"desired":3,"metrics":[{"type":"Resource","name":"cpu","current":"12%","target":"60%"}],"conditions":{"ableToScale":true,"scalingActive":true,"scalingLimited":true,"message":"the desired replica count is less than the minimum replica count"},"atMax":false,"lastScaleAt":1759990000000,"state":"limited"},{"namespace":"media","name":"keda-hpa-transcoder","target":{"kind":"Deployment","name":"transcoder"},"min":1,"max":8,"current":4,"desired":4,"metrics":[{"type":"External","name":"s0-prometheus","current":"41","target":"10"}],"conditions":{"ableToScale":true,"scalingActive":true,"scalingLimited":false},"atMax":false,"lastScaleAt":0,"managedBy":"keda","scaledObject":"transcoder","state":"ok"}]},"keda":{"installed":true,"version":"v1alpha1","error":"","truncated":false,"scaledObjects":[{"namespace":"shop","name":"office-hours","target":{"kind":"Deployment","name":"backoffice"},"min":0,"max":2,"current":0,"desired":0,"triggers":[{"type":"cron","summary":"0 8 * * 1-5 → 0 19 * * 1-5 Europe/Berlin"}],"fallbackReplicas":0,"paused":false,"ready":false,"active":false,"fallback":false,"message":"ScaledObject doesn't have correct scaleTargetRef specification","state":"notReady"},{"namespace":"shop","name":"mailer","target":{"kind":"Deployment","name":"mailer"},"min":1,"max":5,"current":2,"desired":2,"triggers":[{"type":"redis","summary":"outbox"}],"fallbackReplicas":2,"paused":false,"ready":true,"active":false,"fallback":true,"message":"scaler redis failed 3 times: falling back to 2 replicas","hpaName":"keda-hpa-mailer","state":"fallback"},{"namespace":"media","name":"uploader","target":{"kind":"Deployment","name":"uploader"},"min":0,"max":20,"current":0,"desired":0,"triggers":[{"type":"kafka","summary":"uploads"}],"fallbackReplicas":0,"paused":true,"pausedReplicas":5,"ready":true,"active":false,"fallback":false,"state":"paused"},{"namespace":"media","name":"transcoder","target":{"kind":"Deployment","name":"transcoder"},"min":1,"max":8,"current":4,"desired":4,"triggers":[{"type":"prometheus","summary":"sum(rate(jobs_queued_total[2m]))"}],"fallbackReplicas":0,"paused":false,"ready":true,"active":true,"fallback":false,"hpaName":"keda-hpa-transcoder","state":"ok"}],"scaledJobs":[{"namespace":"media","name":"thumbnailer","triggers":[{"type":"rabbitmq","summary":"thumbnails"}],"maxReplicaCount":20,"paused":false,"ready":true,"state":"ok"}]},"vpa":{"installed":true,"version":"v1","error":"","truncated":false,"items":[{"namespace":"vault","name":"vault","target":{"kind":"StatefulSet","name":"vault"},"updateMode":"Initial","containers":[],"recommendationProvided":false,"message":"no pods match this VPA object","state":"notReady"},{"namespace":"billing","name":"invoices","target":{"kind":"Deployment","name":"invoices"},"updateMode":"Off","containers":[{"name":"app","cpuTarget":"250m","memoryTarget":"384Mi","cpuLower":"120m","cpuUpper":"900m","memoryLower":"256Mi","memoryUpper":"1Gi","cpuRequest":"100m","memoryRequest":"256Mi"}],"recommendationProvided":true,"state":"ok"}]},"karpenter":{"installed":true,"version":"v1","error":"","truncated":false,"nodePools":[{"name":"general","limits":{"cpu":"64","memory":"256Gi"},"resources":{"cpu":"64","memory":"212Gi","nodes":8},"atLimit":true,"disruption":{"consolidationPolicy":"WhenEmptyOrUnderutilized","consolidateAfter":"1m","budgets":[{"nodes":"10%","reasons":[]},{"nodes":"0","schedule":"0 9 * * mon-fri","duration":"8h","reasons":["Underutilized"]}]},"weight":10,"ready":true,"capacityTypes":["spot","on-demand"]},{"name":"gpu","limits":{"cpu":"32","memory":""},"resources":{"cpu":"8","memory":"60Gi","nodes":1},"atLimit":false,"disruption":{"consolidationPolicy":"WhenEmpty","consolidateAfter":"10m","budgets":[{"nodes":"1","reasons":[]}]},"weight":0,"ready":true,"capacityTypes":["on-demand"]}],"nodeClaims":[{"name":"general-x7k2p","pool":"general","nodeName":"","instanceType":"m7g.2xlarge","capacityType":"spot","launched":true,"registered":false,"initialized":false,"ready":false,"message":"Node not registered with cluster","created":1760000000000,"pending":true,"drifted":false,"disrupting":false},{"name":"general-4mq9d","pool":"general","nodeName":"node-10-0-12-7","instanceType":"m7g.2xlarge","capacityType":"spot","launched":true,"registered":true,"initialized":true,"ready":true,"created":1759900000000,"pending":false,"drifted":true,"disrupting":false},{"name":"gpu-s2v8w","pool":"gpu","nodeName":"node-10-0-40-3","instanceType":"g6.2xlarge","capacityType":"on-demand","launched":true,"registered":true,"initialized":true,"ready":true,"created":1759800000000,"pending":false,"drifted":false,"disrupting":false}]},"workloads":[{"kind":"Deployment","namespace":"billing","name":"invoices","by":"hpa","autoscaler":"invoices","min":1,"max":4,"current":1,"desired":1,"paused":false,"state":"notReady"},{"kind":"Deployment","namespace":"shop","name":"backoffice","by":"keda","autoscaler":"office-hours","min":0,"max":2,"current":0,"desired":0,"paused":false,"state":"notReady"},{"kind":"StatefulSet","namespace":"vault","name":"vault","by":"vpa","autoscaler":"vault","min":0,"max":0,"current":0,"desired":0,"paused":false,"state":"notReady"},{"kind":"Deployment","namespace":"shop","name":"web","by":"hpa","autoscaler":"web","min":2,"max":6,"current":6,"desired":6,"paused":false,"state":"atMax"},{"kind":"Deployment","namespace":"shop","name":"api","by":"hpa","autoscaler":"api","min":3,"max":10,"current":3,"desired":3,"paused":false,"state":"limited"},{"kind":"Deployment","namespace":"shop","name":"mailer","by":"keda","autoscaler":"mailer","min":1,"max":5,"current":2,"desired":2,"paused":false,"state":"fallback"},{"kind":"Deployment","namespace":"media","name":"uploader","by":"keda","autoscaler":"uploader","min":0,"max":20,"current":0,"desired":0,"paused":true,"state":"paused"},{"kind":"Deployment","namespace":"billing","name":"invoices","by":"vpa","autoscaler":"invoices","min":0,"max":0,"current":0,"desired":0,"paused":false,"state":"ok"},{"kind":"Deployment","namespace":"media","name":"transcoder","by":"keda","autoscaler":"transcoder","min":1,"max":8,"current":4,"desired":4,"paused":false,"state":"ok"}],"workloadsTruncated":false}
    """)

    @Test
    fun decodesTheDemoReport() {
        assertTrue(demo.hpa.installed)
        assertEquals("v2", demo.hpa.version)
        assertEquals(4, demo.hpa.items.size)
        assertEquals(4, demo.keda.scaledObjects.size)
        assertEquals(1, demo.keda.scaledJobs.size)
        assertEquals(2, demo.vpa.items.size)
        assertEquals(2, demo.karpenter.nodePools.size)
        assertEquals(3, demo.karpenter.nodeClaims.size)
        assertEquals(9, demo.workloads.size)
        assertFalse(demo.workloadsTruncated)
    }

    @Test
    fun problemsListsEveryProblemStateOnce() {
        val problems = demo.problems()
        // web atMax, api limited, invoices notReady (HPA); uploader paused, mailer fallback,
        // office-hours notReady (KEDA); vault notReady (VPA); general atLimit and a pending
        // claim and a drifted claim (Karpenter). The healthy rows stay out.
        assertEquals(
            listOf(
                "billing/invoices", "shop/web", "shop/api",
                "shop/office-hours", "shop/mailer", "media/uploader",
                "vault/vault", "general", "general-x7k2p", "general-4mq9d",
            ),
            problems.map { it.label },
        )
        assertEquals(AUTOSCALE_NOT_READY, problems[0].state)
        assertEquals(AUTOSCALE_AT_MAX, problems.first { it.name == "web" }.state)
        assertEquals(AUTOSCALE_PAUSED, problems.first { it.name == "uploader" }.state)
        assertEquals(AUTOSCALE_FALLBACK, problems.first { it.name == "mailer" }.state)
    }

    @Test
    fun boundsText() {
        assertEquals("6 of 2–6", hpaBoundsText(6, 2, 6))
        assertEquals("1 of 1–4", hpaBoundsText(1, 1, 4))
    }

    @Test
    fun changeText() {
        assertEquals("100m → 250m", vpaChangeText("100m", "250m"))
        assertEquals("250m", vpaChangeText("250m", "250m"))
        assertEquals("250m", vpaChangeText("", "250m"))
        assertEquals("100m", vpaChangeText("100m", ""))
    }

    @Test
    fun filteredByNamespaceAndQuery() {
        val shop = demo.filtered("shop", "")
        assertEquals(listOf("web", "api"), shop.hpa.items.map { it.name })
        assertEquals(listOf("office-hours", "mailer"), shop.keda.scaledObjects.map { it.name })
        // Karpenter is cluster-level: it shows under every namespace.
        assertEquals(2, shop.karpenter.nodePools.size)

        val paused = demo.filtered(null, "upload")
        assertEquals(listOf("uploader"), paused.keda.scaledObjects.map { it.name })

        val kedaHpa = demo.filtered(null, "keda-hpa-transcoder")
        assertEquals(listOf("keda-hpa-transcoder"), kedaHpa.hpa.items.map { it.name })
    }

    @Test
    fun namespacesForTheFilter() {
        assertEquals(listOf("billing", "media", "shop", "vault"), demo.autoscalingNamespaces)
    }

    @Test
    fun kedaOwnedHpaLinksItsScaledObject() {
        val hpa = demo.hpa.items.first { it.name == "keda-hpa-transcoder" }
        assertEquals("keda", hpa.managedBy)
        assertEquals("transcoder", hpa.scaledObject)
        val so = demo.keda.scaledObjects.first { it.name == "transcoder" }
        assertEquals("keda-hpa-transcoder", so.hpaName)
        assertEquals(4, so.current)
    }

    @Test
    fun quantitiesStayStrings() {
        val vpa = demo.vpa.items.first { it.name == "invoices" }
        val container = vpa.containers.single()
        assertEquals("100m", container.cpuRequest)
        assertEquals("250m", container.cpuTarget)
        assertEquals("384Mi", container.memoryTarget)
        assertTrue(vpa.recommendationProvided)
    }

    @Test
    fun hpaBoundsRefuseWhatTheCoreRefuses() {
        assertTrue(hpaBoundsValid(1, 1))
        assertTrue(hpaBoundsValid(2, 10))
        assertFalse(hpaBoundsValid(0, 3))
        assertFalse(hpaBoundsValid(4, 3))
        assertFalse(hpaBoundsValid(1, MAX_SCALE_REPLICAS + 1))
    }

    @Test
    fun kedaOwnedHpaHasNoBoundsForm() {
        assertTrue(demo.hpa.items.first { it.name == "keda-hpa-transcoder" }.kedaOwned)
        assertFalse(demo.hpa.items.first { it.managedBy.isEmpty() }.kedaOwned)
    }
}
