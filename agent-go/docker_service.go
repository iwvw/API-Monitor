package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/client"
)

var (
	dockerCli     *client.Client
	dockerCliOnce sync.Once
	dockerAvailable bool
)

// InitDockerClient 初始化 Docker 客户端
func InitDockerClient() {
	dockerCliOnce.Do(func() {
		var err error
		// 使用 WithAPIVersionNegotiation 自动协商 API 版本
		dockerCli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			log.Printf("[Docker] 初始化客户端失败: %v", err)
			dockerAvailable = false
			return
		}

		// 测试连接
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		_, err = dockerCli.Ping(ctx)
		if err != nil {
			log.Printf("[Docker] 连接失败 (Docker 可能未运行): %v", err)
			dockerAvailable = false
		} else {
			log.Printf("[Docker] 客户端初始化成功")
			dockerAvailable = true
		}
	})
}

// GetDockerClient 获取 Docker 客户端，如果未初始化或不可用返回 nil
func GetDockerClient() *client.Client {
	if dockerCli == nil {
		InitDockerClient()
	}
	if !dockerAvailable {
		// 尝试重新连接 (简单的重试机制，避免一直死掉)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if _, err := dockerCli.Ping(ctx); err == nil {
			dockerAvailable = true
		}
	}
	
	if dockerAvailable {
		return dockerCli
	}
	return nil
}
