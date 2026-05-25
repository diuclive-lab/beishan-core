import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct RemoteOverviewCard: View {
    let viewModel: BeiShanRemoteViewModel
    @State private var isShowingDetailList = false

    var body: some View {
        if let dash = viewModel.dashboard {
            VStack(alignment: .leading, spacing: 16) {
                // Header Row
                HStack(alignment: .center, spacing: 10) {
                    Image(systemName: "cpu.fill")
                        .font(.system(size: 16, weight: .bold))
                        .foregroundStyle(
                            LinearGradient(
                                colors: [.blue, .purple],
                                startPoint: .topLeading,
                                endPoint: .bottomTrailing
                            )
                        )
                    
                    Text("北山 Agent 运行状态")
                        .font(.system(.subheadline, design: .rounded).weight(.bold))
                    
                    Spacer()
                    
                    // Pulse online indicator
                    HStack(spacing: 5) {
                        Circle()
                            .fill(dash.health == "ok" ? Color.green : Color.red)
                            .frame(width: 8, height: 8)
                        Text(dash.health == "ok" ? "运行中" : "异常")
                            .font(.caption2.weight(.semibold))
                            .foregroundStyle(dash.health == "ok" ? .green : .red)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Color.primary.opacity(0.04))
                    .clipShape(Capsule())
                }
                
                // Diagnostic & Uptime Pills
                HStack(spacing: 8) {
                    Label(dash.uptime, systemImage: "clock.fill")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(Color.primary.opacity(0.05))
                        .clipShape(Capsule())
                    
                    Label(viewModel.service.config.baseURL.host ?? "localhost", systemImage: "network")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(Color.primary.opacity(0.05))
                        .clipShape(Capsule())
                }
                
                // Grid of 6 metrics
                let columns = [
                    GridItem(.flexible(), spacing: 12),
                    GridItem(.flexible(), spacing: 12)
                ]
                
                LazyVGrid(columns: columns, spacing: 12) {
                    // 1. Knowledge count
                    metricCell(
                        title: "知识存储",
                        value: getStatString(dash.knowledge) ?? "0",
                        subtitle: "个记忆与事实",
                        icon: "brain.head.profile",
                        color: .blue
                    )
                    
                    // 2. Session Count
                    metricCell(
                        title: "会话总数",
                        value: getStatString(dash.sessions) ?? "0",
                        subtitle: "个对话序列",
                        icon: "bubble.left.and.bubble.right.fill",
                        color: .indigo
                    )
                    
                    // 3. Tokens / Usage today
                    metricCell(
                        title: "本日调用",
                        value: getUsageString(dash.usage) ?? "0",
                        subtitle: "次 LLM 请求",
                        icon: "key.fill",
                        color: .purple
                    )
                    
                    // 4. Integrated Tools
                    metricCell(
                        title: "集成工具",
                        value: "\(dash.tools)",
                        subtitle: "个控制端 API",
                        icon: "wrench.and.screwdriver.fill",
                        color: .orange
                    )
                    
                    // 5. Workflows
                    metricCell(
                        title: "可用技能",
                        value: "\(dash.workflows?.count ?? 0)",
                        subtitle: "个流程工作流",
                        icon: "play.flow.fill",
                        color: .teal
                    )
                    
                    // 6. Registered Plugins
                    metricCell(
                        title: "运行插件",
                        value: "\(dash.plugins?.count ?? 0)",
                        subtitle: "个系统常驻模块",
                        icon: "puzzlepiece.extension.fill",
                        color: .pink
                    )
                }
                
                // Toggle expansion button
                Button {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                        isShowingDetailList.toggle()
                    }
                } label: {
                    HStack {
                        Text(isShowingDetailList ? "隐藏模块详情" : "展开模块详情")
                        Image(systemName: isShowingDetailList ? "chevron.up" : "chevron.down")
                    }
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.vertical, 4)
                }
                
                if isShowingDetailList {
                    VStack(alignment: .leading, spacing: 12) {
                        // Workflows List
                        if let wfs = dash.workflows, !wfs.isEmpty {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("工作流技能")
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(.secondary)
                                ScrollView(.horizontal, showsIndicators: false) {
                                    HStack(spacing: 8) {
                                        ForEach(wfs, id: \.self) { wf in
                                            Text(wf)
                                                .font(.caption2.weight(.medium))
                                                .padding(.horizontal, 8)
                                                .padding(.vertical, 4)
                                                .background(Color.accentColor.opacity(0.1))
                                                .clipShape(Capsule())
                                        }
                                    }
                                }
                            }
                        }
                        
                        // Plugins List
                        if let plugins = dash.plugins, !plugins.isEmpty {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("已启用内核插件")
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(.secondary)
                                ScrollView(.horizontal, showsIndicators: false) {
                                    HStack(spacing: 8) {
                                        ForEach(plugins, id: \.self) { plugin in
                                            Text(plugin)
                                                .font(.caption2.weight(.medium))
                                                .padding(.horizontal, 8)
                                                .padding(.vertical, 4)
                                                .background(Color.primary.opacity(0.05))
                                                .clipShape(Capsule())
                                        }
                                    }
                                }
                            }
                        }
                    }
                    .padding(10)
                    .background(Color.primary.opacity(0.02))
                    .clipShape(RoundedRectangle(cornerRadius: 12))
                }
            }
            .padding(16)
            .background {
                RoundedRectangle(cornerRadius: 24)
                    .fill(Color(.secondarySystemBackground))
            }
            .overlay {
                RoundedRectangle(cornerRadius: 24)
                    .strokeBorder(Color.primary.opacity(0.04), lineWidth: 1)
            }
        }
    }

    // MARK: - Grid Cell Component
    
    private func metricCell(title: String, value: String, subtitle: String, icon: String, color: Color) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Image(systemName: icon)
                    .font(.caption)
                    .foregroundStyle(color)
                Text(title)
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(.secondary)
                Spacer()
            }
            
            Text(value)
                .font(.system(.title2, design: .rounded).weight(.bold))
                .padding(.top, 2)
            
            Text(subtitle)
                .font(.system(size: 9))
                .foregroundStyle(.tertiary)
        }
        .padding(10)
        .background {
            RoundedRectangle(cornerRadius: 16)
                .fill(Color(.tertiarySystemBackground))
                .shadow(color: Color.black.opacity(0.02), radius: 2, x: 0, y: 1)
        }
    }
    
    // MARK: - Data Helpers
    
    private func getStatString(_ val: JSONValue?) -> String? {
        guard let val = val else { return nil }
        switch val {
        case .number(let n):
            return String(format: "%.0f", n)
        case .string(let s):
            return s
        case .object(let obj):
            if let total = obj["total"] ?? obj["count"] {
                return getStatString(total)
            }
            return "\(obj.count)"
        case .array(let arr):
            return "\(arr.count)"
        default:
            return nil
        }
    }
    
    private func getUsageString(_ dict: [String: JSONValue]?) -> String? {
        guard let dict = dict else { return nil }
        if let total = dict["total_requests"] ?? dict["requests"] ?? dict["total"] {
            return getStatString(total)
        }
        return "\(dict.count)"
    }
}
