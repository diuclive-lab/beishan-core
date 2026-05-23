package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"beishan/internal/rightflower"
	"gopkg.in/yaml.v3"
)

func enableFlower(dir, name string) error {
	examplePath := filepath.Join(dir, name+".yaml.example")
	targetPath := filepath.Join(dir, name+".yaml")
	data, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("未找到 %s.yaml.example", name)
	}
	var m rightflower.Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("解析失败: %w", err)
	}
	m.Enabled = true
	if err := rightflower.ValidateManifest(&m); err != nil {
		return fmt.Errorf("配置不合法: %w", err)
	}
	out, _ := yaml.Marshal(&m)
	return os.WriteFile(targetPath, out, 0644)
}

func disableFlower(dir, name string) error {
	targetPath := filepath.Join(dir, name+".yaml")
	if err := os.Remove(targetPath); os.IsNotExist(err) {
		return fmt.Errorf("%s 未注册", name)
	}
	return nil
}

func validateDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
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
			msg := err.Error()
			if strings.Contains(msg, "未启用") {
				if strings.HasSuffix(e.Name(), ".yaml") {
					fmt.Printf("  ⚠️ %s: enabled=false（.yaml 文件建议启用或删除）\n", e.Name())
					failed++
				}
				continue
			}
			fmt.Printf("  ❌ %s: %v\n", e.Name(), err)
			failed++
		}
		if m.Enabled && strings.HasSuffix(e.Name(), ".yaml.example") {
			fmt.Printf("  ⚠️ %s: .example 不应为 enabled=true\n", e.Name())
		}
	}
	return failed, nil
}

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
		dryRun := false
		for _, a := range os.Args[3:] {
			if a == "--dry-run" {
				dryRun = true
			}
		}
		examplePath := filepath.Join(dir, name+".yaml.example")
		data, err := os.ReadFile(examplePath)
		if dryRun {
			if err != nil {
				fmt.Printf("[dry-run] 未找到 %s.yaml.example\n", name)
			} else {
				fmt.Printf("[dry-run] 将写入 %s.yaml:\n%s\n", name, string(data))
			}
			os.Exit(0)
		}
		if err := enableFlower(dir, name); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("✅ %s 已启用\n", name)

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

	case "generate":
		if len(os.Args) < 3 {
			fmt.Println("Usage: go run ./cmd/rightflowerctl generate <name>")
			os.Exit(1)
		}
		name := os.Args[2]
		tpl := fmt.Sprintf("name: \"%s\"\ntype: \"custom\"\nprotocol: \"http\"\nendpoint: \"http://localhost:9530\"\nenabled: false\nroute_exposed: false\ncapabilities:\n  - custom.method\noutput_format: \"json\"\nsafety_level: \"sandbox\"\n", name)
		os.WriteFile(filepath.Join(dir, name+".yaml.example"), []byte(tpl), 0644)
		fmt.Printf("✅ %s.yaml.example 已生成（编辑 endpoint 和 capabilities 后启用）\n", name)

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
