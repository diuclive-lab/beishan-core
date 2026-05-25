import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct BeiShanIOSScreen: View {
    @Bindable var viewModel: BeiShanRemoteViewModel
    let onOpenConnectionSettings: () -> Void
    let onOpenSessions: () -> Void

    var body: some View {
        NavigationStack {
            messageList
                .background(Color(.systemBackground))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button(action: onOpenSessions) {
                            Image(systemName: "bubble.left.and.bubble.right")
                        }
                    }
                    ToolbarItem(placement: .principal) {
                        titleView
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            viewModel.resetSession()
                        } label: {
                            Image(systemName: "square.and.pencil")
                        }
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        mainMenu
                    }
                }
                .safeAreaInset(edge: .bottom) {
                    ComposerView(viewModel: viewModel)
                        .background(.ultraThinMaterial)
                }
        }
    }

    // MARK: - Subviews
    
    private var titleView: some View {
        VStack(spacing: 2) {
            HStack(spacing: 6) {
                Circle()
                    .fill(viewModel.isOnline ? Color.green : Color.red)
                    .frame(width: 6, height: 6)
                Text("北山 Agent")
                    .font(.system(.headline, design: .rounded).weight(.semibold))
            }
            Text(viewModel.isOnline ? (viewModel.isThinking ? "正在推理规划..." : "在线") : "未连接")
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
    }

    private var mainMenu: some View {
        Menu {
            Button {
                Task {
                    await viewModel.refreshHealth()
                    await viewModel.loadDashboard()
                }
            } label: {
                Label("刷新状态", systemImage: "arrow.clockwise")
            }
            
            Button(action: onOpenConnectionSettings) {
                Label("连接设置", systemImage: "network")
            }
            
            Button(role: .destructive) {
                viewModel.resetSession()
            } label: {
                Label("新建会话", systemImage: "plus.bubble")
            }
        } label: {
            Image(systemName: "ellipsis.circle")
        }
    }

    private var messageList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 16) {
                    StatusBannerView(viewModel: viewModel)
                    
                    RemoteOverviewCard(viewModel: viewModel)
                    
                    if viewModel.messages.isEmpty {
                        emptyState
                    } else {
                        ForEach(viewModel.messages) { message in
                            MessageRow(message: message)
                                .id(message.id)
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.top, 12)
                .padding(.bottom, 12)
            }
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: viewModel.messages.count) { _, _ in
                if let lastID = viewModel.messages.last?.id {
                    withAnimation(.easeOut(duration: 0.25)) {
                        proxy.scrollTo(lastID, anchor: .bottom)
                    }
                }
            }
        }
    }

    private var emptyState: some View {
        VStack(spacing: 16) {
            Spacer().frame(height: 60)
            
            ZStack {
                Circle()
                    .fill(Color.accentColor.opacity(0.08))
                    .frame(width: 80, height: 80)
                
                Image(systemName: "sparkles")
                    .font(.system(size: 36))
                    .foregroundStyle(
                        LinearGradient(
                            colors: [.blue, .purple],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
            }
            
            VStack(spacing: 6) {
                Text("开始与北山 Agent 远程对话")
                    .font(.system(.subheadline, design: .rounded).weight(.bold))
                
                Text("您可以发送问题或启动长文本写作与法律审查工作流。")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 24)
            }
            
            Spacer()
        }
        .frame(maxWidth: .infinity)
    }
}
