import SwiftUI

@main
struct BeiShanIOSApp: App {
    @State private var profile = BeiShanConnectionStore.load()

    var body: some Scene {
        WindowGroup {
            BeiShanIOSRootView(
                profile: $profile
            )
        }
    }
}
