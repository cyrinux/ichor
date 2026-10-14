import XCTest
@testable import IchorCore

final class AutoscalingTests: XCTestCase {
    /// What go/ichorgo demoAutoscaling() answers, as KubeAutoscaling writes it.
    private let demo: AutoscalingReport = {
        let json = """
        {
         "hpa": {
          "installed": true,
          "version": "v2",
          "error": "",
          "truncated": false,
          "items": [
           {
            "namespace": "billing",
            "name": "invoices",
            "target": {
             "kind": "Deployment",
             "name": "invoices"
            },
            "min": 1,
            "max": 4,
            "current": 1,
            "desired": 1,
            "metrics": [
             {
              "type": "Resource",
              "name": "cpu",
              "current": "-",
              "target": "75%"
             }
            ],
            "conditions": {
             "ableToScale": true,
             "scalingActive": false,
             "scalingLimited": false,
             "message": "the HPA was unable to compute the replica count: unable to get metrics for resource cpu"
            },
            "atMax": false,
            "lastScaleAt": 0,
            "state": "notReady"
           },
           {
            "namespace": "shop",
            "name": "web",
            "target": {
             "kind": "Deployment",
             "name": "web"
            },
            "min": 2,
            "max": 6,
            "current": 6,
            "desired": 6,
            "metrics": [
             {
              "type": "Resource",
              "name": "cpu",
              "current": "94%",
              "target": "70%"
             }
            ],
            "conditions": {
             "ableToScale": true,
             "scalingActive": true,
             "scalingLimited": true,
             "message": "the desired replica count is more than the maximum replica count"
            },
            "atMax": true,
            "lastScaleAt": 1760000000000,
            "state": "atMax"
           },
           {
            "namespace": "shop",
            "name": "api",
            "target": {
             "kind": "Deployment",
             "name": "api"
            },
            "min": 3,
            "max": 10,
            "current": 3,
            "desired": 3,
            "metrics": [
             {
              "type": "Resource",
              "name": "cpu",
              "current": "12%",
              "target": "60%"
             }
            ],
            "conditions": {
             "ableToScale": true,
             "scalingActive": true,
             "scalingLimited": true,
             "message": "the desired replica count is less than the minimum replica count"
            },
            "atMax": false,
            "lastScaleAt": 1759990000000,
            "state": "limited"
           },
           {
            "namespace": "media",
            "name": "keda-hpa-transcoder",
            "target": {
             "kind": "Deployment",
             "name": "transcoder"
            },
            "min": 1,
            "max": 8,
            "current": 4,
            "desired": 4,
            "metrics": [
             {
              "type": "External",
              "name": "s0-prometheus",
              "current": "41",
              "target": "10"
             }
            ],
            "conditions": {
             "ableToScale": true,
             "scalingActive": true,
             "scalingLimited": false
            },
            "atMax": false,
            "lastScaleAt": 0,
            "managedBy": "keda",
            "scaledObject": "transcoder",
            "state": "ok"
           }
          ]
         },
         "keda": {
          "installed": true,
          "version": "v1alpha1",
          "error": "",
          "truncated": false,
          "scaledObjects": [
           {
            "namespace": "shop",
            "name": "office-hours",
            "target": {
             "kind": "Deployment",
             "name": "backoffice"
            },
            "min": 0,
            "max": 2,
            "current": 0,
            "desired": 0,
            "triggers": [
             {
              "type": "cron",
              "summary": "0 8 * * 1-5 → 0 19 * * 1-5 Europe/Berlin"
             }
            ],
            "fallbackReplicas": 0,
            "paused": false,
            "ready": false,
            "active": false,
            "fallback": false,
            "message": "ScaledObject doesn't have correct scaleTargetRef specification",
            "state": "notReady"
           },
           {
            "namespace": "shop",
            "name": "mailer",
            "target": {
             "kind": "Deployment",
             "name": "mailer"
            },
            "min": 1,
            "max": 5,
            "current": 2,
            "desired": 2,
            "triggers": [
             {
              "type": "redis",
              "summary": "outbox"
             }
            ],
            "fallbackReplicas": 2,
            "paused": false,
            "ready": true,
            "active": false,
            "fallback": true,
            "message": "scaler redis failed 3 times: falling back to 2 replicas",
            "hpaName": "keda-hpa-mailer",
            "state": "fallback"
           },
           {
            "namespace": "media",
            "name": "uploader",
            "target": {
             "kind": "Deployment",
             "name": "uploader"
            },
            "min": 0,
            "max": 20,
            "current": 0,
            "desired": 0,
            "triggers": [
             {
              "type": "kafka",
              "summary": "uploads"
             }
            ],
            "fallbackReplicas": 0,
            "paused": true,
            "pausedReplicas": 5,
            "ready": true,
            "active": false,
            "fallback": false,
            "state": "paused"
           },
           {
            "namespace": "media",
            "name": "transcoder",
            "target": {
             "kind": "Deployment",
             "name": "transcoder"
            },
            "min": 1,
            "max": 8,
            "current": 4,
            "desired": 4,
            "triggers": [
             {
              "type": "prometheus",
              "summary": "sum(rate(jobs_queued_total[2m]))"
             }
            ],
            "fallbackReplicas": 0,
            "paused": false,
            "ready": true,
            "active": true,
            "fallback": false,
            "hpaName": "keda-hpa-transcoder",
            "state": "ok"
           }
          ],
          "scaledJobs": [
           {
            "namespace": "media",
            "name": "thumbnailer",
            "triggers": [
             {
              "type": "rabbitmq",
              "summary": "thumbnails"
             }
            ],
            "maxReplicaCount": 20,
            "paused": false,
            "ready": true,
            "state": "ok"
           }
          ]
         },
         "vpa": {
          "installed": true,
          "version": "v1",
          "error": "",
          "truncated": false,
          "items": [
           {
            "namespace": "vault",
            "name": "vault",
            "target": {
             "kind": "StatefulSet",
             "name": "vault"
            },
            "updateMode": "Initial",
            "containers": [],
            "recommendationProvided": false,
            "message": "no pods match this VPA object",
            "state": "notReady"
           },
           {
            "namespace": "billing",
            "name": "invoices",
            "target": {
             "kind": "Deployment",
             "name": "invoices"
            },
            "updateMode": "Off",
            "containers": [
             {
              "name": "app",
              "cpuTarget": "250m",
              "memoryTarget": "384Mi",
              "cpuLower": "120m",
              "cpuUpper": "900m",
              "memoryLower": "256Mi",
              "memoryUpper": "1Gi",
              "cpuRequest": "100m",
              "memoryRequest": "256Mi"
             }
            ],
            "recommendationProvided": true,
            "state": "ok"
           }
          ]
         },
         "karpenter": {
          "installed": true,
          "version": "v1",
          "error": "",
          "truncated": false,
          "nodePools": [
           {
            "name": "general",
            "limits": {
             "cpu": "64",
             "memory": "256Gi"
            },
            "resources": {
             "cpu": "64",
             "memory": "212Gi",
             "nodes": 8
            },
            "atLimit": true,
            "disruption": {
             "consolidationPolicy": "WhenEmptyOrUnderutilized",
             "consolidateAfter": "1m",
             "budgets": [
              {
               "nodes": "10%",
               "reasons": []
              },
              {
               "nodes": "0",
               "schedule": "0 9 * * mon-fri",
               "duration": "8h",
               "reasons": [
                "Underutilized"
               ]
              }
             ]
            },
            "weight": 10,
            "ready": true,
            "capacityTypes": [
             "spot",
             "on-demand"
            ]
           },
           {
            "name": "gpu",
            "limits": {
             "cpu": "32",
             "memory": ""
            },
            "resources": {
             "cpu": "8",
             "memory": "60Gi",
             "nodes": 1
            },
            "atLimit": false,
            "disruption": {
             "consolidationPolicy": "WhenEmpty",
             "consolidateAfter": "10m",
             "budgets": [
              {
               "nodes": "1",
               "reasons": []
              }
             ]
            },
            "weight": 0,
            "ready": true,
            "capacityTypes": [
             "on-demand"
            ]
           }
          ],
          "nodeClaims": [
           {
            "name": "general-x7k2p",
            "pool": "general",
            "nodeName": "",
            "instanceType": "m7g.2xlarge",
            "capacityType": "spot",
            "launched": true,
            "registered": false,
            "initialized": false,
            "ready": false,
            "message": "Node not registered with cluster",
            "created": 1760000000000,
            "pending": true,
            "drifted": false,
            "disrupting": false
           },
           {
            "name": "general-4mq9d",
            "pool": "general",
            "nodeName": "node-10-0-12-7",
            "instanceType": "m7g.2xlarge",
            "capacityType": "spot",
            "launched": true,
            "registered": true,
            "initialized": true,
            "ready": true,
            "created": 1759900000000,
            "pending": false,
            "drifted": true,
            "disrupting": false
           },
           {
            "name": "gpu-s2v8w",
            "pool": "gpu",
            "nodeName": "node-10-0-40-3",
            "instanceType": "g6.2xlarge",
            "capacityType": "on-demand",
            "launched": true,
            "registered": true,
            "initialized": true,
            "ready": true,
            "created": 1759800000000,
            "pending": false,
            "drifted": false,
            "disrupting": false
           }
          ]
         },
         "workloads": [
          {
           "kind": "Deployment",
           "namespace": "billing",
           "name": "invoices",
           "by": "hpa",
           "autoscaler": "invoices",
           "min": 1,
           "max": 4,
           "current": 1,
           "desired": 1,
           "paused": false,
           "state": "notReady"
          },
          {
           "kind": "Deployment",
           "namespace": "shop",
           "name": "backoffice",
           "by": "keda",
           "autoscaler": "office-hours",
           "min": 0,
           "max": 2,
           "current": 0,
           "desired": 0,
           "paused": false,
           "state": "notReady"
          },
          {
           "kind": "StatefulSet",
           "namespace": "vault",
           "name": "vault",
           "by": "vpa",
           "autoscaler": "vault",
           "min": 0,
           "max": 0,
           "current": 0,
           "desired": 0,
           "paused": false,
           "state": "notReady"
          },
          {
           "kind": "Deployment",
           "namespace": "shop",
           "name": "web",
           "by": "hpa",
           "autoscaler": "web",
           "min": 2,
           "max": 6,
           "current": 6,
           "desired": 6,
           "paused": false,
           "state": "atMax"
          },
          {
           "kind": "Deployment",
           "namespace": "shop",
           "name": "api",
           "by": "hpa",
           "autoscaler": "api",
           "min": 3,
           "max": 10,
           "current": 3,
           "desired": 3,
           "paused": false,
           "state": "limited"
          },
          {
           "kind": "Deployment",
           "namespace": "shop",
           "name": "mailer",
           "by": "keda",
           "autoscaler": "mailer",
           "min": 1,
           "max": 5,
           "current": 2,
           "desired": 2,
           "paused": false,
           "state": "fallback"
          },
          {
           "kind": "Deployment",
           "namespace": "media",
           "name": "uploader",
           "by": "keda",
           "autoscaler": "uploader",
           "min": 0,
           "max": 20,
           "current": 0,
           "desired": 0,
           "paused": true,
           "state": "paused"
          },
          {
           "kind": "Deployment",
           "namespace": "billing",
           "name": "invoices",
           "by": "vpa",
           "autoscaler": "invoices",
           "min": 0,
           "max": 0,
           "current": 0,
           "desired": 0,
           "paused": false,
           "state": "ok"
          },
          {
           "kind": "Deployment",
           "namespace": "media",
           "name": "transcoder",
           "by": "keda",
           "autoscaler": "transcoder",
           "min": 1,
           "max": 8,
           "current": 4,
           "desired": 4,
           "paused": false,
           "state": "ok"
          }
         ],
         "workloadsTruncated": false
        }
        """
        return try! JSONDecoder().decode(AutoscalingReport.self, from: Data(json.utf8))
    }()

    func testDecodesTheDemoReport() throws {
        XCTAssertTrue(demo.hpa.state.installed)
        XCTAssertEqual(demo.hpa.state.version, "v2")
        XCTAssertEqual(demo.hpa.items.count, 4)
        XCTAssertEqual(demo.keda.scaledObjects.count, 4)
        XCTAssertEqual(demo.keda.scaledJobs.count, 1)
        XCTAssertEqual(demo.vpa.items.count, 2)
        XCTAssertEqual(demo.karpenter.nodePools.count, 2)
        XCTAssertEqual(demo.karpenter.nodeClaims.count, 3)
        XCTAssertEqual(demo.workloads.count, 9)
        XCTAssertFalse(demo.workloadsTruncated)
    }

    func testProblemsListEveryProblemStateOnce() {
        // invoices notReady, web atMax, api limited (HPA); office-hours notReady, mailer
        // fallback, uploader paused (KEDA); vault notReady (VPA); the general pool at its
        // limit, a pending and a drifted claim (Karpenter). The healthy rows stay out.
        XCTAssertEqual(demo.problems.map(\.label), [
            "billing/invoices", "shop/web", "shop/api",
            "shop/office-hours", "shop/mailer", "media/uploader",
            "vault/vault", "general", "general-x7k2p", "general-4mq9d",
        ])
        XCTAssertEqual(demo.problems[0].state, .notReady)
        XCTAssertEqual(demo.problems.first { $0.name == "web" }?.state, .atMax)
        XCTAssertEqual(demo.problems.first { $0.name == "uploader" }?.state, .paused)
        XCTAssertEqual(demo.problems.first { $0.name == "mailer" }?.state, .fallback)
    }

    func testBoundsText() {
        XCTAssertEqual(hpaBoundsText(6, 2, 6), "6 of 2–6")
        XCTAssertEqual(hpaBoundsText(1, 1, 4), "1 of 1–4")
    }

    func testChangeText() {
        XCTAssertEqual(vpaChangeText("100m", "250m"), "100m → 250m")
        XCTAssertEqual(vpaChangeText("250m", "250m"), "250m")
        XCTAssertEqual(vpaChangeText("", "250m"), "250m")
        XCTAssertEqual(vpaChangeText("100m", ""), "100m")
    }

    func testFilteredByNamespaceAndQuery() {
        let shop = demo.filtered(namespace: "shop", query: "")
        XCTAssertEqual(shop.hpa.items.map(\.name), ["web", "api"])
        XCTAssertEqual(shop.keda.scaledObjects.map(\.name), ["office-hours", "mailer"])
        // Karpenter is cluster-level: it shows under every namespace.
        XCTAssertEqual(shop.karpenter.nodePools.count, 2)

        let paused = demo.filtered(namespace: nil, query: "upload")
        XCTAssertEqual(paused.keda.scaledObjects.map(\.name), ["uploader"])

        let kedaHpa = demo.filtered(namespace: nil, query: "keda-hpa-transcoder")
        XCTAssertEqual(kedaHpa.hpa.items.map(\.name), ["keda-hpa-transcoder"])
    }

    func testNamespacesForThePicker() {
        XCTAssertEqual(demo.namespaceNames, ["billing", "media", "shop", "vault"])
    }

    func testKedaOwnedHpaLinksItsScaledObject() {
        let hpa = try! XCTUnwrap(demo.hpa.items.first { $0.name == "keda-hpa-transcoder" })
        XCTAssertEqual(hpa.managedBy, "keda")
        XCTAssertEqual(hpa.scaledObject, "transcoder")
        let so = try! XCTUnwrap(demo.keda.scaledObjects.first { $0.name == "transcoder" })
        XCTAssertEqual(so.hpaName, "keda-hpa-transcoder")
        XCTAssertEqual(so.current, 4)
    }

    func testQuantitiesStayStrings() throws {
        let vpa = try XCTUnwrap(demo.vpa.items.first { $0.name == "invoices" })
        let container = try XCTUnwrap(vpa.containers.first)
        XCTAssertEqual(container.cpuRequest, "100m")
        XCTAssertEqual(container.cpuTarget, "250m")
        XCTAssertEqual(container.memoryTarget, "384Mi")
        XCTAssertTrue(vpa.recommendationProvided)
    }

    func testAbsentGroupsAnswerInstalledFalse() throws {
        let json = """
        {"hpa":{"installed":true,"version":"v2","error":"","truncated":false,"items":[]},"keda":{"installed":false,"version":"","error":"","truncated":false,"scaledObjects":[],"scaledJobs":[]},"vpa":{"installed":false,"version":"","error":"","truncated":false,"items":[]},"karpenter":{"installed":false,"version":"","error":"","truncated":false,"nodePools":[],"nodeClaims":[]},"workloads":[],"workloadsTruncated":false}
        """
        let report = try JSONDecoder().decode(AutoscalingReport.self, from: Data(json.utf8))
        XCTAssertTrue(report.hpa.state.installed)
        XCTAssertFalse(report.keda.state.installed)
        XCTAssertTrue(report.keda.scaledObjects.isEmpty)
        XCTAssertFalse(report.vpa.state.installed)
        XCTAssertFalse(report.karpenter.state.installed)
        XCTAssertTrue(report.problems.isEmpty)
    }
}
