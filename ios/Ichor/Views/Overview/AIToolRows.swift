import SwiftUI
import IchorCore

/// Everything the model asked for: each request, and on demand the exact text sent back.
struct AIToolRows: View {
    let calls: [AIToolCall]
    let unsupported: Bool

    var body: some View {
        if !calls.isEmpty {
            Text("Looked at").font(.subheadline.weight(.semibold))
        }
        ForEach(calls) { call in
            row(call)
        }
        if unsupported {
            Text("This server does not take requests for more data: the model answered from the report alone.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func row(_ call: AIToolCall) -> some View {
        if let error = call.error {
            VStack(alignment: .leading, spacing: 2) {
                Text("Not read: \(summary(call))").font(.footnote)
                Text(verbatim: error).font(.caption).foregroundStyle(.secondary)
            }
        } else if call.result.isEmpty {
            Text(verbatim: summary(call)).font(.footnote)
        } else {
            DisclosureGroup {
                Text(verbatim: call.result)
                    .font(.system(.caption2, design: .monospaced))
                    .textSelection(.enabled)
            } label: {
                let size = ByteCountFormatter.string(fromByteCount: Int64(call.bytes), countStyle: .binary)
                Text(verbatim: "\(summary(call)) (\(size))").font(.footnote)
            }
        }
    }

    private func summary(_ call: AIToolCall) -> String {
        let args = call.args
        switch call.tool {
        case "pod_logs": return String(localized: "logs of \(args.pod)")
        case "object_events": return String(localized: "events of \(args.kind) \(args.name)")
        case "object_yaml": return String(localized: "YAML of \(args.kind) \(args.name)")
        case "service_logs": return String(localized: "\(args.service) logs on \(args.node)")
        default: return String(localized: "a request for \(call.tool)")
        }
    }
}
