package plugins

// intent_keywords.go — 唯一关键词源
//
// 所有意图判断关键词在此集中定义。其他文件只引用，不自行定义。
// 修改任何词表只改这一个文件。

// ── 系统指令词（精确匹配，零歧义，最高优先级）─────────────────────────────
// 匹配时不做任何检索，直接执行状态机操作。

var cmdConfirmWords   = []string{"确认", "是", "yes"}
var cmdForceSaveWords = []string{"强制记录", "是的，强制记录", "强制入库"}
var cmdMergeWords     = []string{"确认合并", "合并"}

// cmdRejectRemember 显式拒绝入库（有 pending 时生效）
var cmdRejectRemember = []string{
	"不用了", "不用记", "取消", "取消记录", "不需要", "不需要记",
	"不对", "错了", "不是这样", "算了", "忽略",
}

// cmdCorrectTypePrefix 改正分类前缀（后跟类型名，有 pending 时生效）
var cmdCorrectTypePrefix = []string{"改成", "应该是", "类型是", "类型改为"}

const cmdBatchPrefix = "确认 "

var cmdRememberTriggers = []string{
	"已放弃", "最终方案", "确认结论", "架构决定",
	"经验教训", "踩坑记录", "最终决定",
}
var cmdReviewTriggers = []string{
	"审查一下", "知识审查", "审查对话", "审查最近", "knowledge review",
}
var cmdListReviews = []string{"待审查报告", "审查队列", "review queue"}
var cmdConfirmAll  = []string{"确认全部", "全部入库", "confirm all"}
var cmdSkipAll     = []string{"跳过", "清理审查", "skip"}

// ── 当前 Session 词（指向"刚刚发生的事"）─────────────────────────────────────
// 匹配时：只读当前 session 历史，不触发知识库检索。

var ctxCurrentSession = []string{
	"刚才", "刚刚", "上一句", "你刚才", "你没",
}

// ── 跨 Session 词（指向"某个时候发生的事"）──────────────────────────────────
// 匹配时：优先触发 episodic 情景检索，查找历史 session。

var ctxCrossSession = []string{
	"上次", "之前", "以前", "过去", "曾经", "那次", "当时",
	"讨论过", "聊过", "说过", "提到过", "做过", "决定过",
	"那时候", "过往", "历史", "记得", "还记得", "什么时候",
	"昨天", "前两天", "最近", "几月",
}

// ── 语义知识词（指向"结论/决策/原则"）──────────────────────────────────────
// 匹配时：优先触发知识库语义检索。

var ctxSemantic = []string{
	"决策", "决定", "结论", "方案", "教训", "原则",
	"为什么放弃", "最终", "确定", "选择了", "机制",
	"架构", "流程", "设计", "放弃", "为什么", "标准", "规则",
}

// ── 模糊指代词（需要查询改写）────────────────────────────────────────────────
// 匹配时：用 LLM 将口语查询改写为精确检索关键词再检索。

var ctxVagueRef = []string{
	"那个", "这个", "那篇", "这篇", "那个改动", "那个问题",
	"怎么说的", "怎么搞的", "什么情况", "怎么样了",
}

// ── 代码相关词────────────────────────────────────────────────────────────────
// 匹配时：优先触发代码检索（源码扫描）。

var ctxCode = []string{
	"代码", "函数", "实现", "源码", "调用", "定义在哪",
	"怎么实现", "code", "func", "implementation", "where",
	"怎么写的", "在哪", "什么方法",
}
