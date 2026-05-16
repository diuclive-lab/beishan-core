package tools

import (
	"encoding/json"
	"fmt"
)

/* ValidateAndExecute 是 L4 调用 L3 的唯一入口。

   执行流程：
   1. 从 Schema 注册中心获取工具 Schema
   2. 对 payload 执行严格 JSON Schema 校验（L3 硬化）
   3. 校验通过后，调用内部 Execute

   L4 插件永远只调此函数，禁止直接调用 Execute。
   这是防止 LLM 不可靠输出穿透到业务逻辑的关键防线。
*/
func ValidateAndExecute(name string, payload json.RawMessage) *ToolResult {
	schema, ok := GetToolSchema(name)
	if !ok {
		return errorResult(fmt.Sprintf("未知工具: %s", name))
	}

	if err := ValidateParams(schema, payload); err != nil {
		return errorResult(fmt.Sprintf("参数校验不通过: %s", err))
	}

	return Execute(name, string(payload))
}
