package rightflower

import (
	"encoding/json"
	"fmt"

	"beishan/kernel"
)

// Plugin implements kernel.Plugin, dispatching calls to external right flowers via HTTP.
type Plugin struct {
	Name     string
	Client   *Client
	Manifest *Manifest
}

// OnMessage dispatches to the right flower via HTTP and returns the result.
func (p *Plugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	req := &Request{
		ID:        msg.CorrelationID,
		Type:      "dispatch",
		Sender:    msg.Sender,
		Recipient: msg.Recipient,
		Method:    msg.Type,
		Params: func() map[string]interface{} {
			obj := map[string]interface{}{}
			if err := json.Unmarshal(msg.Payload, &obj); err == nil {
				return obj
			}
			return map[string]interface{}{"payload": string(msg.Payload)}
		}(),
	}

	resp, err := p.Client.Dispatch(p.Manifest.Endpoint, req)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("右花 %s 失败: %w", p.Name, err)
	}
	if resp.Error != "" {
		return kernel.Message{}, fmt.Errorf("右花 %s 返回错误: %s", p.Name, resp.Error)
	}

	if resp.Result != nil {
		SecurityWrapper(resp.Result, p.Name, req.Method)
	}

	payload, _ := json.Marshal(resp.Result)
	return kernel.Message{Type: msg.Type + ".result", Payload: payload}, nil
}

// RegisterAll loads manifests from a directory and registers them as kernel plugins.
func RegisterAll(k *kernel.Kernel, manifestDir string) error {
	reg := NewRegistry()
	if err := reg.LoadDir(manifestDir); err != nil {
		return err
	}
	client := NewClient()
	for name, m := range reg.Flowers {
		p := &Plugin{
			Name:     name,
			Client:   client,
			Manifest: m,
		}
		meta := kernel.Meta{
			Description: fmt.Sprintf("右花: %s (%s)", m.Name, m.Type),
			Tags:        []string{"rightflower", m.Type},
		}
		if m.RouteExposed {
			meta.Types = m.Capabilities
		}
		k.Register(name, p, meta)
		fmt.Printf("[rightflower] 插件注册: %s\n", name)
	}
	return nil
}
