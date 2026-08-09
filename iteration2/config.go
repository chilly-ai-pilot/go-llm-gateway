package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 配置结构体
type Config struct {
	Port     int           `yaml:"port"`
	Routes   []Route       `yaml:"routes"`
	Timeouts TimeoutConfig `yaml:"timeouts"`
}

// TimeoutConfig 超时配置
type TimeoutConfig struct {
	DialTimeout int `yaml:"dial_timeout"` // 连接超时(秒)
	TTFBTimeout int `yaml:"ttfb_timeout"` // 首字节超时(秒)
	IdleTimeout int `yaml:"idle_timeout"` // chunk 间隔超时(秒)
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
	if len(config.Routes) == 0 {
		return nil, fmt.Errorf("配置错误: routes 不能为空")
	}

	// 设置超时默认值
	if config.Timeouts.DialTimeout == 0 {
		config.Timeouts.DialTimeout = 5
	}
	if config.Timeouts.TTFBTimeout == 0 {
		config.Timeouts.TTFBTimeout = 30
	}
	if config.Timeouts.IdleTimeout == 0 {
		config.Timeouts.IdleTimeout = 60
	}

	// 验证每个路由
	for i, route := range config.Routes {
		if err := ValidateRoute(&route); err != nil {
			return nil, fmt.Errorf("配置错误: routes[%d] - %w", i, err)
		}
	}

	return &config, nil
}

// GetAPIKey 从环境变量获取 API Key
func GetAPIKey(envName string) string {
	if envName == "" {
		return ""
	}
	return os.Getenv(envName)
}
