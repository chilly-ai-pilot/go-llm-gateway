package main

import (
	"net"
	"sync"
)

// connTracker 包装 net.Conn,记录连接对象
// 用于在建立 TCP 连接时捕获底层 net.Conn,以便后续设置读超时
type connTracker struct {
	net.Conn
	conn *net.Conn // 指向实际连接的指针
	mu   sync.Mutex
}

func (c *connTracker) Close() error {
	// 清理引用
	c.mu.Lock()
	if c.conn != nil {
		*c.conn = nil
	}
	c.mu.Unlock()
	return c.Conn.Close()
}
