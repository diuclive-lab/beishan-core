package glue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

/* Manifest 定义了插件的元信息。

   每个插件目录下必须有一个 manifest.json 文件。
   胶水层扫描插件目录，读取 manifest 后 spawn 对应的子进程。
*/
type Manifest struct {
	Name  string `json:"name"`  // 插件名，也是内核注册表中的名字
	Entry string `json:"entry"` // 启动文件，相对于插件目录，如 "main.py"
	Type  string `json:"type"`  // 插件类型："python" 或 "go"
}

/* ScanDir 扫描插件目录，返回所有合法的 manifest。

   规则：
   - plugins/ 下每个子目录对应一个插件
   - 子目录内必须包含 manifest.json
   - manifest 中的 Name 不能为空
   - 不合法则跳过，不影响其他插件加载
*/
func ScanDir(dir string) ([]Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取插件目录失败: %w", err)
	}

	var manifests []Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(dir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // 没有 manifest.json 的目录不算插件
		}

		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Name == "" {
			continue
		}

		manifests = append(manifests, m)
	}

	return manifests, nil
}
