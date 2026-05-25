import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct BeiShanIOSRootView: View {
    @Binding var profile: BeiShanConnectionProfile
    @State private var isShowingConnectionSheet = false
    @State private var isShowingSessionsSheet = false
    
    // Using @State for the modernized @Observable ViewModel
    @State private var viewModel: BeiShanRemoteViewModel

    init(profile: Binding<BeiShanConnectionProfile>) {
        _profile = profile
        let config = BeiShanAPIConfig(
            baseURL: URL(string: profile.wrappedValue.baseURL) ?? URL(string: "http://localhost:8013")!,
            userID: profile.wrappedValue.userID,
            basicAuthUsername: profile.wrappedValue.basicAuthUsername,
            basicAuthPassword: profile.wrappedValue.basicAuthPassword
        )
        let service = BeiShanAPIService(config: config)
        _viewModel = State(initialValue: BeiShanRemoteViewModel(service: service))
    }

    var body: some View {
        BeiShanIOSScreen(
            viewModel: viewModel,
            onOpenConnectionSettings: {
                isShowingConnectionSheet = true
            },
            onOpenSessions: {
                isShowingSessionsSheet = true
            }
        )
        .sheet(isPresented: $isShowingConnectionSheet) {
            ConnectionSheet(
                profile: $profile,
                isPresented: $isShowingConnectionSheet,
                onSave: { newProfile in
                    profile = newProfile
                    BeiShanConnectionStore.save(newProfile)
                    
                    let config = BeiShanAPIConfig(
                        baseURL: URL(string: newProfile.baseURL) ?? URL(string: "http://localhost:8013")!,
                        userID: newProfile.userID,
                        basicAuthUsername: newProfile.basicAuthUsername,
                        basicAuthPassword: newProfile.basicAuthPassword
                    )
                    let service = BeiShanAPIService(config: config)
                    viewModel = BeiShanRemoteViewModel(service: service)
                    Task { await viewModel.refreshHealth() }
                }
            )
        }
        .sheet(isPresented: $isShowingSessionsSheet) {
            SessionsSheet(
                viewModel: viewModel,
                isPresented: $isShowingSessionsSheet
            )
        }
        .task {
            // Bootstrap on appear
            await viewModel.refreshHealth()
            await viewModel.loadDashboard()
        }
    }
}
