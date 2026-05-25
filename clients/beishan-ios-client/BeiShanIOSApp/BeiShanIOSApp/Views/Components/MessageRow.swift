import SwiftUI
#if canImport(BeiShanClientCore)
import BeiShanClientCore
#endif

struct MessageRow: View {
    let message: ChatMessage

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            if isUser { Spacer(minLength: 40) }
            
            if !isUser { roleIcon }
            
            VStack(alignment: isUser ? .trailing : .leading, spacing: 4) {
                Text(roleName)
                    .font(.system(size: 10, weight: .bold, design: .rounded))
                    .foregroundStyle(.secondary)
                
                Text(message.content)
                    .textSelection(.enabled)
                    .font(.system(.body, design: .rounded))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(bubbleColor)
                    .foregroundStyle(textColor)
                    .clipShape(
                        RoundedCorner(
                            radius: 16,
                            corners: isUser ? [.topLeft, .bottomLeft, .bottomRight] : [.topRight, .bottomLeft, .bottomRight]
                        )
                    )
            }
            
            if isUser { roleIcon }
            
            if !isUser { Spacer(minLength: 40) }
        }
        .padding(.vertical, 4)
    }

    // MARK: - Helper Views
    
    private var roleIcon: some View {
        ZStack {
            Circle()
                .fill(roleColor.opacity(0.12))
                .frame(width: 32, height: 32)
            Image(systemName: roleIconName)
                .font(.footnote)
                .foregroundStyle(roleColor)
        }
        .padding(.top, 2)
    }

    // MARK: - Computed Properties
    
    private var isUser: Bool {
        message.role.lowercased() == "user"
    }
    
    private var isSystem: Bool {
        let r = message.role.lowercased()
        return r == "system" || r == "info"
    }

    private var roleName: String {
        if isUser { return "您" }
        if isSystem { return "系统" }
        // For plugins or agents
        let rawRole = message.role
        if rawRole.lowercased() == "assistant" || rawRole.lowercased() == "beishan" {
            return "北山 Agent"
        }
        // Map common plugins to clean names
        if rawRole.contains("_plugin") {
            return rawRole.replacingOccurrences(of: "_plugin", with: " 插件").uppercased()
        }
        return rawRole
    }

    private var roleIconName: String {
        if isUser { return "person.fill" }
        if isSystem { return "info.circle.fill" }
        let rawRole = message.role.lowercased()
        if rawRole.contains("search") { return "magnifyingglass" }
        if rawRole.contains("browser") { return "safari.fill" }
        if rawRole.contains("write") || rawRole.contains("file") { return "doc.text.fill" }
        if rawRole.contains("memory") || rawRole.contains("session") { return "brain.fill" }
        if rawRole.contains("terminal") { return "terminal.fill" }
        return "sparkles"
    }

    private var roleColor: Color {
        if isUser { return .blue }
        if isSystem { return .gray }
        let rawRole = message.role.lowercased()
        if rawRole.contains("search") || rawRole.contains("browser") { return .teal }
        if rawRole.contains("write") { return .orange }
        if rawRole.contains("terminal") { return .red }
        return .purple
    }
    
    private var bubbleColor: Color {
        if isUser {
            return .blue
        }
        if isSystem {
            return Color(.systemGray6)
        }
        return Color(.secondarySystemBackground)
    }
    
    private var textColor: Color {
        if isUser {
            return .white
        }
        return .primary
    }
}

// MARK: - Rounded Corner Helper Shape

struct RoundedCorner: Shape {
    var radius: CGFloat = .infinity
    var corners: UIRectCorner = .allCorners

    func path(in rect: CGRect) -> Path {
        let path = UIBezierPath(roundedRect: rect, byRoundingCorners: corners, cornerRadii: CGSize(width: radius, height: radius))
        return Path(path.cgPath)
    }
}
