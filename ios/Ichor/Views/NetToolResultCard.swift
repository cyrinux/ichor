import SwiftUI
import IchorCore

/// Below this many days left, a certificate's expiry is a warning.
private let certWarnDays = 14

/// A check's result as fields: only what the tool's section holds.
struct NetToolResultSections: View {
    let result: NetToolResult

    var body: some View {
        Section {
            StatusPill(label: result.ok ? String(localized: "OK") : String(localized: "Problem"),
                       color: result.ok ? .statusOK : .statusBad)
            if let dns = result.dns { dnsRows(dns) }
            if let ping = result.ping { pingRows(ping) }
            if let port = result.port { portRows(port) }
            if let http = result.http { httpRows(http) }
        }
        if let dns = result.dns {
            Section("Records (\(dns.records.count))") {
                if dns.records.isEmpty { Text("No record.").foregroundStyle(.secondary) }
                ForEach(Array(dns.records.enumerated()), id: \.offset) { _, r in
                    HStack {
                        Text(verbatim: r.type).font(.caption.bold())
                        Text(verbatim: r.value).font(.body.monospaced()).textSelection(.enabled)
                        Spacer()
                        Text("TTL \(r.ttl) s").font(.caption).foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        }
        if let hops = result.trace {
            Section("Hops (\(hops.count))") {
                ForEach(hops) { h in
                    HStack {
                        Text(verbatim: "\(h.hop).").font(.caption.bold())
                        Text(verbatim: h.silent ? String(localized: "no answer") : h.host)
                            .font(.body.monospaced())
                            .foregroundStyle(h.silent ? Color.secondary : Color.primary)
                        Spacer()
                        if !h.silent { Text(verbatim: "\(ms(h.avgMs)) · \(pct(h.lossPct))").font(.caption).foregroundStyle(.secondary) }
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        }
        if let http = result.http, http.tls {
            Section("Certificate") {
                LabeledContent("Verified") {
                    Text(http.tlsOk ? "Yes" : "No").foregroundStyle(http.tlsOk ? Color.statusOK : Color.statusBad)
                }
                if !http.subject.isEmpty { LabeledContent("Subject") { Text(verbatim: http.subject).font(.caption.monospaced()) } }
                if !http.issuer.isEmpty { LabeledContent("Issuer") { Text(verbatim: http.issuer).font(.caption.monospaced()) } }
                if !http.notAfter.isEmpty {
                    HStack {
                        Text("Expires \(String(http.notAfter.prefix(10)))")
                        Spacer()
                        StatusPill(label: String(localized: "\(http.daysLeft) days left"),
                                   color: http.daysLeft < 0 ? .statusBad : http.daysLeft < certWarnDays ? .statusWarn : .statusOK)
                    }
                }
            }
        }
    }

    @ViewBuilder private func dnsRows(_ dns: NetDnsResult) -> some View {
        LabeledContent("Status") { Text(verbatim: dns.status.isEmpty ? "—" : dns.status).font(.body.monospaced()) }
        LabeledContent("Resolver") { Text(verbatim: dns.server.isEmpty ? "—" : dns.server).font(.body.monospaced()) }
        LabeledContent("Query time") { Text(verbatim: "\(dns.queryMs) ms") }
    }

    @ViewBuilder private func pingRows(_ ping: NetPingResult) -> some View {
        LabeledContent("Received") { Text(verbatim: "\(ping.received) / \(ping.sent)") }
        LabeledContent("Loss") { Text(verbatim: pct(ping.lossPct)).foregroundStyle(ping.lossPct > 0 ? Color.statusWarn : Color.statusOK) }
        if ping.received > 0 {
            LabeledContent("Min / avg / max") { Text(verbatim: "\(num(ping.minMs)) / \(num(ping.avgMs)) / \(ms(ping.maxMs))") }
        }
    }

    @ViewBuilder private func portRows(_ port: NetPortResult) -> some View {
        Text(port.open ? "Open" : "Closed or filtered")
            .font(.headline)
            .foregroundStyle(port.open ? Color.statusOK : Color.statusBad)
        if !port.message.isEmpty { Text(verbatim: port.message).font(.caption).foregroundStyle(.secondary) }
    }

    @ViewBuilder private func httpRows(_ http: NetHttpResult) -> some View {
        LabeledContent("HTTP status") { Text(verbatim: http.status == 0 ? "—" : "\(http.status)") }
        LabeledContent("Total time") { Text(verbatim: ms(http.totalMs)) }
        if !http.redirectUrl.isEmpty { LabeledContent("Redirect") { Text(verbatim: http.redirectUrl).font(.caption.monospaced()) } }
        if !http.error.isEmpty { Text(verbatim: http.error).font(.caption).foregroundStyle(.statusBad) }
    }
}

private func num(_ value: Double) -> String {
    value == value.rounded() ? String(format: "%.0f", value) : String(format: "%.1f", value)
}

private func ms(_ value: Double) -> String { "\(num(value)) ms" }

private func pct(_ value: Double) -> String { "\(num(value)) %" }

/// A result as plain text, to paste in a chat or a ticket: the fields, then the raw output.
func netToolShareText(_ result: NetToolResult, hostname: String) -> String {
    var lines = ["\(result.tool.uppercased()) \(result.target) — \(hostname)",
                 result.ok ? String(localized: "OK") : String(localized: "Problem")]
    if let d = result.dns {
        lines.append("\(d.status) · \(d.server) · \(d.queryMs) ms")
        lines += d.records.map { "\($0.name) \($0.ttl) \($0.type) \($0.value)" }
    }
    if let p = result.ping {
        lines.append("\(p.received)/\(p.sent), \(pct(p.lossPct)), \(num(p.minMs))/\(num(p.avgMs))/\(ms(p.maxMs))")
    }
    if let p = result.port {
        lines.append((p.open ? String(localized: "Open") : String(localized: "Closed or filtered")) + " · " + p.message)
    }
    if let hops = result.trace {
        lines += hops.map { "\($0.hop). \($0.host) \(ms($0.avgMs)) \(pct($0.lossPct))" }
    }
    if let h = result.http {
        lines.append("HTTP \(h.status) · \(ms(h.totalMs))")
        if h.tls { lines.append("\(h.subject) · \(h.issuer) · \(h.notAfter) (\(String(localized: "\(h.daysLeft) days left")))") }
    }
    if !result.raw.isEmpty { lines += ["", result.raw.trimmingCharacters(in: .whitespacesAndNewlines)] }
    return lines.joined(separator: "\n")
}
