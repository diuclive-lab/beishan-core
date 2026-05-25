import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct StatusBannerView: View {
    let viewModel: BeiShanRemoteViewModel

    var body: some View {
        if !viewModel.isOnline {
            HStack(spacing: 8) {
                Image(systemName: "wifi.slash")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.red)
                VStack(alignment: .leading, spacing: 2) {
                    Text("智能体服务离线")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.red)
                    Text("请在 [连接设置] 中检查服务器 IP，或确保 Mac 上的 beishan-core 服务已运行。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                Spacer(minLength: 0)
            }
            .padding(12)
            .background(Color.red.opacity(0.12))
            .clipShape(RoundedRectangle(cornerRadius: 14))
        } else if let error = viewModel.errorMessage {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.orange)
                VStack(alignment: .leading, spacing: 2) {
                    Text("服务请求出错")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.orange)
                    Text(error)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                Spacer(minLength: 0)
            }
            .padding(12)
            .background(Color.orange.opacity(0.12))
            .clipShape(RoundedRectangle(cornerRadius: 14))
        }
    }
}
