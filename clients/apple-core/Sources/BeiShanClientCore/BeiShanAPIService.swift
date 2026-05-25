import Foundation

public struct BeiShanAPIService: Sendable {
    public let config: BeiShanAPIConfig
    private let session: URLSession

    public init(config: BeiShanAPIConfig, session: URLSession = .shared) {
        self.config = config
        self.session = session
    }

    public func health() async throws -> HealthResponse {
        let request = makeRequest(path: "/health")
        let (data, response) = try await session.data(for: request)
        try validate(response: response, data: data)
        return try JSONDecoder().decode(HealthResponse.self, from: data)
    }

    public func sendMessage(
        sessionID: String? = nil,
        message: String,
        async: Bool = false,
        mode: String? = nil
    ) async throws -> ChatResponse {
        var request = makeRequest(path: "/api/chat")
        request.httpMethod = "POST"
        request.timeoutInterval = 180
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        
        let chatReq = ChatRequest(session_id: sessionID, message: message, async: async, mode: mode)
        request.httpBody = try JSONEncoder().encode(chatReq)
        
        let (data, response) = try await session.data(for: request)
        try validate(response: response, data: data)
        return try JSONDecoder().decode(ChatResponse.self, from: data)
    }

    public func getAsyncResult(sessionID: String) async throws -> ChatResponse {
        let request = makeRequest(path: "/api/result/\(sessionID)")
        let (data, response) = try await session.data(for: request)
        try validate(response: response, data: data)
        return try JSONDecoder().decode(ChatResponse.self, from: data)
    }

    public func loadDashboard() async throws -> DashboardResponse {
        let request = makeRequest(path: "/api/dashboard")
        let (data, response) = try await session.data(for: request)
        try validate(response: response, data: data)
        return try JSONDecoder().decode(DashboardResponse.self, from: data)
    }

    private func makeRequest(path: String) -> URLRequest {
        let url = config.baseURL.appendingPathComponent(path)
        var request = URLRequest(url: url)
        applyBasicAuth(to: &request)
        return request
    }

    private func applyBasicAuth(to request: inout URLRequest) {
        if let username = config.basicAuthUsername,
           let password = config.basicAuthPassword,
           !username.isEmpty {
            let raw = "\(username):\(password)"
            if let encoded = raw.data(using: .utf8)?.base64EncodedString() {
                request.setValue("Basic \(encoded)", forHTTPHeaderField: "Authorization")
            }
        }
    }

    private func validate(response: URLResponse, data: Data) throws {
        guard let http = response as? HTTPURLResponse else {
            throw BeiShanAPIError.badServerResponse
        }
        guard (200..<300).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw BeiShanAPIError.http(statusCode: http.statusCode, body: body)
        }
    }
}

public enum BeiShanAPIError: LocalizedError, Sendable {
    case badServerResponse
    case http(statusCode: Int, body: String)

    public var errorDescription: String? {
        switch self {
        case .badServerResponse:
            return "后端返回了无法识别的响应。"
        case .http(let statusCode, let body):
            if body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return "BeiShan API 请求失败：HTTP \(statusCode)"
            }
            return "BeiShan API 请求失败：HTTP \(statusCode) - \(body)"
        }
    }
}
