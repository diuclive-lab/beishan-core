import Foundation

public struct HealthResponse: Codable, Sendable, Hashable {
    public let status: String

    public init(status: String) {
        self.status = status
    }
}

public struct ChatRequest: Codable, Sendable {
    public let session_id: String?
    public let message: String
    public let async: Bool?
    public let mode: String?

    public init(session_id: String? = nil, message: String, async: Bool? = nil, mode: String? = nil) {
        self.session_id = session_id
        self.message = message
        self.async = async
        self.mode = mode
    }
}

public struct ChatResponse: Codable, Sendable, Hashable {
    public let session_id: String
    public let sender: String
    public let type: String
    public let payload: JSONValue?
    public let status: String? // for async pending responses

    public init(session_id: String, sender: String, type: String, payload: JSONValue?, status: String? = nil) {
        self.session_id = session_id
        self.sender = sender
        self.type = type
        self.payload = payload
        self.status = status
    }

    public var payloadString: String {
        guard let payload = payload else { return "" }
        switch payload {
        case .string(let s):
            return s
        case .number(let n):
            return String(n)
        case .bool(let b):
            return String(b)
        case .null:
            return ""
        case .object(let obj):
            if let data = try? JSONEncoder().encode(obj), let s = String(data: data, encoding: .utf8) {
                return s
            }
            return String(describing: obj)
        case .array(let arr):
            if let data = try? JSONEncoder().encode(arr), let s = String(data: data, encoding: .utf8) {
                return s
            }
            return String(describing: arr)
        }
    }
}

public struct DashboardResponse: Codable, Sendable, Hashable {
    public let knowledge: JSONValue?
    public let sessions: JSONValue?
    public let usage: [String: JSONValue]?
    public let workflows: [String]?
    public let plugins: [String]?
    public let tools: Int
    public let health: String
    public let uptime: String

    public init(
        knowledge: JSONValue? = nil,
        sessions: JSONValue? = nil,
        usage: [String: JSONValue]? = nil,
        workflows: [String]? = nil,
        plugins: [String]? = nil,
        tools: Int = 0,
        health: String = "ok",
        uptime: String = "0s"
    ) {
        self.knowledge = knowledge
        self.sessions = sessions
        self.usage = usage
        self.workflows = workflows
        self.plugins = plugins
        self.tools = tools
        self.health = health
        self.uptime = uptime
    }
}

// MARK: - Generic JSON Value Support

public enum JSONValue: Codable, Hashable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            self = .object(try container.decode([String: JSONValue].self))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .string(let value):
            try container.encode(value)
        case .number(let value):
            try container.encode(value)
        case .bool(let value):
            try container.encode(value)
        case .object(let value):
            try container.encode(value)
        case .array(let value):
            try container.encode(value)
        case .null:
            try container.encodeNil()
        }
    }
}
