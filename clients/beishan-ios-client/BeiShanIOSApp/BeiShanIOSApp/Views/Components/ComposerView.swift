import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct ComposerView: View {
    @Bindable var viewModel: BeiShanRemoteViewModel
    @State private var inputText = ""
    @State private var isAsync = false

    var body: some View {
        VStack(spacing: 8) {
            // Mode Select bar
            HStack {
                Text("消息模式:")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                
                Picker("模式", selection: $isAsync) {
                    Text("同步响应").tag(false)
                    Text("异步后台").tag(true)
                }
                .pickerStyle(.segmented)
                .frame(width: 160)
                
                Spacer()
                
                if viewModel.isThinking {
                    HStack(spacing: 6) {
                        ProgressView()
                            .controlSize(.mini)
                        Text(isAsync ? "智能体在后台规划..." : "正在推理...")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)

            HStack(alignment: .bottom, spacing: 10) {
                TextField("输入消息...", text: $inputText, axis: .vertical)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .background(Color(.secondarySystemBackground))
                    .clipShape(RoundedRectangle(cornerRadius: 20))
                    .lineLimit(1...6)
                    .disabled(viewModel.isThinking)
                
                Button {
                    let textToSend = inputText
                    inputText = ""
                    viewModel.sendMessage(textToSend, async: isAsync)
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 32))
                        .foregroundStyle(inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || viewModel.isThinking ? Color(.systemGray4) : Color.accentColor)
                }
                .disabled(inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || viewModel.isThinking)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 12)
        }
    }
}
