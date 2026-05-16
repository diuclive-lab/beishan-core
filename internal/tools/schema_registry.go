package tools

import (
	"encoding/json"
	"fmt"
)

/* ToolSchema 定义一个工具的完整参数契约。

   Name：工具名，与 Registry 中的名字一致
   Description：工具用途描述（用于审计和错误提示）
   Schema：JSON Schema 定义，如 {"type":"object","required":["query"],"properties":...}
*/
type ToolSchema struct {
	Name        string
	Description string
	Schema      map[string]interface{}
}

var schemaRegistry = make(map[string]ToolSchema)

/* RegisterToolSchema 注册一个工具的 Schema。

   Schema 注册中心是 L3 硬化的唯一数据源。
   Router（L1）只查询工具是否存在，不接触 Payload。
   ValidateAndExecute（L3）用完整 Schema 做严格校验。
*/
func RegisterToolSchema(name string, schema ToolSchema) {
	schemaRegistry[name] = schema
}

/* GetToolSchema 根据工具名查询 Schema。

   提供给 Router 做轻量路由验证（仅确认工具是否存在）。
   绝不接收或返回 Payload，严守"Payload 对内核不透明"铁律。
*/
func GetToolSchema(name string) (ToolSchema, bool) {
	s, ok := schemaRegistry[name]
	return s, ok
}

/* GetAvailableTools 返回所有已注册工具的名称列表。

   用于 Router 构建提示词时告知 DeepSeek 可选的路由目标。
*/
func GetAvailableTools() []string {
	names := make([]string, 0, len(schemaRegistry))
	for name := range schemaRegistry {
		names = append(names, name)
	}
	return names
}

/* ValidateParams 对 payload 执行严格 JSON Schema 校验。

   校验规则：
   1. payload 必须是合法 JSON 对象
   2. 不允许出现 Schema 未定义的字段（禁止额外字段注入）
   3. required 字段必须存在
   4. 字段类型必须匹配（string / integer / boolean / array / object）
   5. 数组类型递归校验 items schema

   任何一条不满足则返回错误，不走降级。
   这是 L3 硬化的第一道防线。
*/
func ValidateParams(schema ToolSchema, payload json.RawMessage) error {
	if len(payload) == 0 {
		return fmt.Errorf("[schema] payload 为空")
	}

	var params map[string]interface{}
	if err := json.Unmarshal(payload, &params); err != nil {
		return fmt.Errorf("[schema] payload 不是合法 JSON: %w", err)
	}
	if params == nil {
		return fmt.Errorf("[schema] payload 必须是 JSON 对象")
	}

	schemaObj := schema.Schema
	props, _ := schemaObj["properties"].(map[string]interface{})

	// 规则 2：禁止额外字段
	for key := range params {
		if props != nil {
			if _, ok := props[key]; !ok {
				return fmt.Errorf("[schema] 未知字段: %s", key)
			}
		}
	}

	// 规则 3：required 字段必须存在
	if required, ok := schemaObj["required"].([]interface{}); ok {
		for _, r := range required {
			name, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := params[name]; !exists {
				return fmt.Errorf("[schema] 缺少必填字段: %s", name)
			}
		}
	}

	// 规则 4+5：字段类型校验
	for key, value := range params {
		if props == nil {
			continue
		}
		propRaw, ok := props[key]
		if !ok {
			continue
		}
		prop, ok := propRaw.(map[string]interface{})
		if !ok {
			continue
		}

		expectedType, _ := prop["type"].(string)
		if expectedType == "" {
			continue
		}

		if err := validateFieldType(key, value, expectedType); err != nil {
			return err
		}

		// 规则 5：数组 items 递归校验
		if expectedType == "array" {
			if itemsRaw, ok := prop["items"].(map[string]interface{}); ok {
				if arr, ok := value.([]interface{}); ok {
					for i, item := range arr {
						if itemType, ok := itemsRaw["type"].(string); ok {
							if err := validateFieldType(
								fmt.Sprintf("%s[%d]", key, i), item, itemType); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func validateFieldType(name string, value interface{}, expected string) error {
	if value == nil {
		return nil // null 对任何类型合法
	}
	switch expected {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("[schema] 字段 %q: 期望 string, 实际 %T", name, value)
		}
	case "integer":
		v, ok := value.(float64)
		if !ok {
			return fmt.Errorf("[schema] 字段 %q: 期望 integer, 实际 %T", name, value)
		}
		if v != float64(int64(v)) {
			return fmt.Errorf("[schema] 字段 %q: 期望 integer, 实际 float", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("[schema] 字段 %q: 期望 boolean, 实际 %T", name, value)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("[schema] 字段 %q: 期望 array, 实际 %T", name, value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("[schema] 字段 %q: 期望 object, 实际 %T", name, value)
		}
	}
	return nil
}
