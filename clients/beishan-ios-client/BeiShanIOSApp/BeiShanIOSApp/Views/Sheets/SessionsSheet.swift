import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct SessionsSheet: View {
    @Bindable var viewModel: BeiShanRemoteViewModel
    @Binding var isPresented: Bool

    var body: some View {
        NavigationStack {
            List {
                Section("当前会话信息") {
                    if let sid = viewModel.sessionID {
                        HStack {
                            Text("会话 ID")
                            Spacer()
                            Text(sid)
                                .font(.system(.body, design: .monospaced))
                                .foregroundStyle(.secondary)
                        }
                    } else {
                        Text("无活动会话（发送首条消息后自动生成）")
                            .foregroundStyle(.secondary)
                    }
                }
                
                Section {
                    Button(role: .destructive) {
                        viewModel.resetSession()
                        isPresented = false
                    } label: {
                        HStack {
                            Image(systemName: "plus.bubble")
                            Text("重置并新建对话")
                        }
                        .foregroundStyle(.red)
                        .frame(maxWidth: .infinity, alignment: .center)
                    }
                } footer: {
                    Text("beishan-core 采用极简会话与跨会话自动记忆模型。如果你希望清理当前对话的短期记忆上下文，请选择重置新建。")
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle("会话管理")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("关闭") { isPresented = false }
                }
            }
        }
    }
}
