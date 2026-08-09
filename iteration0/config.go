package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 配置结构体
type Config struct {
	Port      int    `yaml:"port"`
	TargetURL string `yaml:"target_url"`
}

// LoadConfig 从 YAML 文件加载配置
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 验证必需字段
	if config.Port == 0 {
		return nil, fmt.Errorf("配置错误: port 不能为空")
	}
	if config.TargetURL == "" {
		return nil, fmt.Errorf("配置错误: target_url 不能为空")
	}

	return &config, nil
}
