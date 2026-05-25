import Foundation

public struct BeiShanConnectionProfile: Codable, Hashable, Sendable {
    public var baseURL: String
    public var userID: String
    public var basicAuthUsername: String?
    public var basicAuthPassword: String?

    public init(
        baseURL: String = "http://localhost:8013",
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

public struct BeiShanConnectionStore {
    private static let key = "beishan.connection.profile"

    public static func save(_ profile: BeiShanConnectionProfile) {
        if let encoded = try? JSONEncoder().encode(profile) {
            UserDefaults.standard.set(encoded, forKey: key)
        }
    }

    public static func load() -> BeiShanConnectionProfile {
        if let data = UserDefaults.standard.data(forKey: key),
           let profile = try? JSONDecoder().decode(BeiShanConnectionProfile.self, from: data) {
            return profile
        }
        return BeiShanConnectionProfile()
    }
}
