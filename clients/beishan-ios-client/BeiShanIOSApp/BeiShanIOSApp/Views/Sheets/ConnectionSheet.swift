import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct ConnectionSheet: View {
    @Binding var profile: BeiShanConnectionProfile
    @State private var draftProfile: BeiShanConnectionProfile
    @Binding var isPresented: Bool
    let onSave: (BeiShanConnectionProfile) -> Void

    init(profile: Binding<BeiShanConnectionProfile>, isPresented: Binding<Bool>, onSave: @escaping (BeiShanConnectionProfile) -> Void) {
        _profile = profile
        _draftProfile = State(initialValue: profile.wrappedValue)
        _isPresented = isPresented
        self.onSave = onSave
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("远程连接") {
                    TextField("Base URL", text: $draftProfile.baseURL)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                        .autocorrectionDisabled()
                    TextField("User ID", text: $draftProfile.userID)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }

                Section("Basic Auth (可选)") {
                    TextField("用户名", text: Binding(
                        get: { draftProfile.basicAuthUsername ?? "" },
                        set: { draftProfile.basicAuthUsername = $0.isEmpty ? nil : $0 }
                    ))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    SecureField("密码", text: Binding(
                        get: { draftProfile.basicAuthPassword ?? "" },
                        set: { draftProfile.basicAuthPassword = $0.isEmpty ? nil : $0 }
                    ))
                }
            }
            .navigationTitle("连接设置")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { isPresented = false }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") {
                        onSave(draftProfile)
                        isPresented = false
                    }
                }
            }
        }
    }
}
