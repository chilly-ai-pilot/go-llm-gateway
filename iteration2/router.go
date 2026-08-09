package main

import (
	"fmt"
	"path/filepath"
)

// Route 路由配置
type Route struct {
	ModelPattern string `yaml:"model_pattern"` // 模型匹配模式（支持通配符）
	Target       string `yaml:"target"`        // 目标 Provider URL
	Adapter      string `yaml:"adapter"`       // 适配器类型
	APIKeyEnv    string `yaml:"api_key_env"`   // API Key 环境变量名
}

// MatchRoute 根据 model 名称匹配路由
// 按配置顺序进行模式匹配，返回第一个匹配的路由
func MatchRoute(model string, routes []Route) (*Route, error) {
	if model == "" {
		return nil, fmt.Errorf("model cannot be empty")
	}

	for i := range routes {
		route := &routes[i]

		// 使用 filepath.Match 进行通配符匹配
		// 支持 * 和 ? 通配符
		matched, err := filepath.Match(route.ModelPattern, model)
		if err != nil {
			// 匹配模式错误，跳过这个路由
			continue
		}

		if matched {
			return route, nil
		}
	}

	return nil, fmt.Errorf("no route found for model: %s", model)
}

// ValidateRoute 验证路由配置的有效性
func ValidateRoute(route *Route) error {
	if route == nil {
		return fmt.Errorf("route cannot be nil")
	}
	if route.ModelPattern == "" {
		return fmt.Errorf("model_pattern cannot be empty")
	}
	if route.Target == "" {
		return fmt.Errorf("target cannot be empty")
	}
	if route.Adapter == "" {
		return fmt.Errorf("adapter cannot be empty")
	}
	return nil
}

