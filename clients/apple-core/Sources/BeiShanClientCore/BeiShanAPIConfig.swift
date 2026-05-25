import Foundation

public struct BeiShanAPIConfig: Sendable, Codable, Hashable {
    public var baseURL: URL
    public var userID: String
    public var basicAuthUsername: String?
    public var basicAuthPassword: String?

    public init(
        baseURL: URL = URL(string: "http://localhost:8013")!,
        userID: String = "user_default",
        basicAuthUsername: String? = nil,
        basicAuthPassword: String? = nil
    ) {
        self.baseURL = baseURL
        self.userID = userID
        self.basicAuthUsername = basicAuthUsername
        self.basicAuthPassword = basicAuthPassword
    }
}
