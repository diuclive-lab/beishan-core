import Foundation
import Observation

public struct ChatMessage: Identifiable, Codable, Hashable, Sendable {
    public let id: UUID
    public let role: String // "user", "assistant", etc.
    public let content: String
    public let timestamp: Date

    public init(id: UUID = UUID(), role: String, content: String, timestamp: Date = Date()) {
        self.id = id
        self.role = role
        self.content = content
        self.timestamp = timestamp
    }
}

@Observable
@MainActor
public final class BeiShanRemoteViewModel {
    public var messages: [ChatMessage] = []
    public var sessionID: String? = nil
    public var isOnline: Bool = false
    public var isThinking: Bool = false
    public var dashboard: DashboardResponse? = nil
    public var errorMessage: String? = nil

    private var activePollTask: Task<Void, Never>? = nil
    public var service: BeiShanAPIService

    public init(service: BeiShanAPIService) {
        self.service = service
    }

    public func refreshHealth() async {
        do {
            let res = try await service.health()
            if res.status == "ok" {
                isOnline = true
            } else {
                isOnline = false
            }
        } catch {
            isOnline = false
        }
    }

    public func loadDashboard() async {
        do {
            let dash = try await service.loadDashboard()
            self.dashboard = dash
            self.isOnline = (dash.health == "ok")
        } catch {
            self.errorMessage = "加载监控面板失败: \(error.localizedDescription)"
        }
    }

    public func resetSession() {
        activePollTask?.cancel()
        activePollTask = nil
        messages.removeAll()
        sessionID = nil
        isThinking = false
    }

    public func selectSession(id: String, existingMessages: [ChatMessage] = []) {
        activePollTask?.cancel()
        activePollTask = nil
        self.sessionID = id
        self.messages = existingMessages
        self.isThinking = false
    }

    public func sendMessage(_ text: String, async: Bool = false, mode: String? = nil) {
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        
        let userMsg = ChatMessage(role: "user", content: text)
        messages.append(userMsg)
        isThinking = true
        errorMessage = nil

        let currentSessionID = sessionID

        Task {
            do {
                let response = try await service.sendMessage(
                    sessionID: currentSessionID,
                    message: text,
                    async: async,
                    mode: mode
                )
                
                if let newSessionID = response.session_id.isEmpty ? nil : response.session_id {
                    self.sessionID = newSessionID
                }

                if async && response.status == "pending" {
                    // Start polling for result
                    pollAsyncResult(sessionID: response.session_id)
                } else {
                    let botMsg = ChatMessage(role: response.sender, content: response.payloadString)
                    await MainActor.run {
                        self.messages.append(botMsg)
                        self.isThinking = false
                    }
                    // Refresh dashboard after a complete interaction
                    await loadDashboard()
                }
            } catch {
                await MainActor.run {
                    self.errorMessage = "发送消息失败: \(error.localizedDescription)"
                    self.isThinking = false
                }
            }
        }
    }

    private func pollAsyncResult(sessionID: String) {
        activePollTask?.cancel()
        
        activePollTask = Task {
            var retries = 0
            let maxRetries = 60 // 2 minutes with 2s interval
            
            while retries < maxRetries && !Task.isCancelled {
                do {
                    try await Task.sleep(nanoseconds: 2_000_000_000) // Sleep 2 seconds
                    let response = try await service.getAsyncResult(sessionID: sessionID)
                    
                    if response.status != "pending" {
                        let botMsg = ChatMessage(role: response.sender, content: response.payloadString)
                        await MainActor.run {
                            self.messages.append(botMsg)
                            self.isThinking = false
                        }
                        await loadDashboard()
                        break
                    }
                } catch {
                    // Silently ignore networking hiccups during polling unless we exceed retries
                }
                retries += 1
            }
            
            if retries >= maxRetries {
                await MainActor.run {
                    self.errorMessage = "异步处理超时，请在仪表盘确认代理运行状态。"
                    self.isThinking = false
                }
            }
        }
    }
}
