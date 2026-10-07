import Foundation
import Ichorgo
import IchorCore

/// Small kubectl actions: scale, CronJob suspend/resume, Deployment rollback and pod logs
/// through the Kubernetes API (os:admin; see TalosClient for the conventions).
extension TalosClient {
    /// `kubectl scale`: returns a warning when a HorizontalPodAutoscaler manages the replicas
    /// ("" when none).
    func scale(_ workload: KubeWorkload, replicas: Int) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeScale(config, context, kubeServer, workload.kind, workload.namespace, workload.name, replicas, error)
        }
    }

    /// Suspends (no new runs) or resumes a CronJob; running Jobs go on.
    func suspendCronJob(_ cronJob: KubeCronJob, suspend: Bool) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeSuspendCronJob(config, context, kubeServer, cronJob.namespace, cronJob.name, suspend, error)
        }
    }

    /// A Deployment's revisions, newest first, like `kubectl rollout history`.
    func deploymentRevisions(_ workload: KubeWorkload) async throws -> [DeploymentRevision] {
        let list: DeploymentRevisionList = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeDeploymentRevisions(config, context, kubeServer, workload.namespace, workload.name, $0)
        }
        return list.revisions
    }

    /// `kubectl rollout undo --to-revision`: starts a rolling update to that revision's template.
    func rollback(_ workload: KubeWorkload, to revision: Int) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeRollbackDeployment(config, context, kubeServer, workload.namespace, workload.name, revision, error)
        }
    }

    /// The last `tailLines` of a container's log (the previous run's with `previous`), as text.
    /// `container` may be "" for a pod with one container.
    func podLogs(_ pod: KubePod, container: String, previous: Bool, tailLines: Int = kubePodLogTail) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubePodLogs(config, context, kubeServer, pod.namespace, pod.name, container, previous, tailLines, error)
        }
    }
}
