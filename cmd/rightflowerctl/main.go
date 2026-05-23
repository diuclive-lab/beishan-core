package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"beishan/internal/rightflower"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run ./cmd/rightflowerctl <list|enable|disable|validate> [name]")
		os.Exit(1)
	}
	cmd := os.Args[1]
	dir := "./right_flowers"

	switch cmd {
	case "list":
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			var m rightflower.Manifest
			if yaml.Unmarshal(data, &m) == nil {
				status := "disabled"
				if m.Enabled {
					status = "enabled"
				}
				fmt.Printf("  %s (%s) — endpoint=%s, status=%s\n", m.Name, m.Type, m.Endpoint, status)
			}
		}

	case "enable":
		if len(os.Args) < 3 {
			fmt.Println("需要指定 flower name")
			os.Exit(1)
		}
		name := os.Args[2]
		examplePath := filepath.Join(dir, name+".yaml.example")
		targetPath := filepath.Join(dir, name+".yaml")
		data, err := os.ReadFile(examplePath)
		if err != nil {
			fmt.Printf("未找到 %s.yaml.example\n", name)
			os.Exit(1)
		}
		var m rightflower.Manifest
		if err := yaml.Unmarshal(data, &m); err != nil {
			fmt.Printf("解析失败: %v\n", err)
			os.Exit(1)
		}
		if err := rightflower.ValidateManifest(&m); err != nil {
			fmt.Printf("配置不合法: %v\n", err)
			os.Exit(1)
		}
		m.Enabled = true
		out, _ := yaml.Marshal(&m)
		os.WriteFile(targetPath, out, 0644)
		fmt.Printf("✅ %s 已启用（endpoint=%s, capabilities=%v）\n", m.Name, m.Endpoint, m.Capabilities)

	case "disable":
		if len(os.Args) < 3 {
			fmt.Println("需要指定 flower name")
			os.Exit(1)
		}
		name := os.Args[2]
		targetPath := filepath.Join(dir, name+".yaml")
		if err := os.Remove(targetPath); os.IsNotExist(err) {
			// also try setting enabled=false
			data, err := os.ReadFile(targetPath)
			if err != nil {
				fmt.Printf("%s 未注册\n", name)
				os.Exit(1)
			}
			var m rightflower.Manifest
			yaml.Unmarshal(data, &m)
			m.Enabled = false
			out, _ := yaml.Marshal(&m)
			os.WriteFile(targetPath, out, 0644)
		} else if err == nil {
			fmt.Printf("✅ %s 已禁用\n", name)
		}

	case "validate":
		entries, _ := os.ReadDir(dir)
		failed := 0
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yaml.example") {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			var m rightflower.Manifest
			if err := yaml.Unmarshal(data, &m); err != nil {
				fmt.Printf("  ❌ %s: 解析失败: %v\n", e.Name(), err)
				failed++
				continue
			}
			if err := rightflower.ValidateManifest(&m); err != nil {
				if strings.Contains(err.Error(), "未启用") {
					continue // allowed for .example
				}
				fmt.Printf("  ❌ %s: %v\n", e.Name(), err)
				failed++
			}
		}
		if failed == 0 {
			fmt.Println("✅ 所有 manifest 合法")
		} else {
			fmt.Printf("❌ %d 个 manifest 不合法\n", failed)
		}
	}
}
