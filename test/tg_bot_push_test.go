package test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ppoonk/pushkit"

	"github.com/gogf/gf/v2/os/gctx"
)

// go test -v test/tg_bot_push_test.go
func TestTgBotPush(t *testing.T) {
	// 配置
	config := pushkit.TgBotConfig{
		BotToken:       os.Getenv("TGBotTestToken"),
		ProxyURL:       "http://192.168.0.61:7899",
		DefaultHandler: nil,
	}

	// 创建推送服务、启动
	op, err := pushkit.WithTgBotSender(config)
	if err != nil {
		t.Error("WithTgBotSender error: ", err)
		return
	}
	p := pushkit.NewPushService(op)
	err = p.Start()
	defer p.Stop()
	if err != nil {
		t.Error("启动错误：", err)
		return
	}

	time.Sleep(time.Second) // 等待 bot启动完成再推送消息

	// 将消息推入队列进行消费
	for i := range 6 {
		req := pushkit.PushRequest{
			Type: pushkit.PushTypeTgBot,
			Message: pushkit.Message{
				To:      []string{os.Getenv("TGUserTestId")},
				Subject: "Subject",
				Content: fmt.Sprintf("第 %d 次, 当前时间: %s", i, time.Now().String()),
			},
		}
		err = p.Push(gctx.New(), req)
		if err != nil {
			t.Fatalf("第 %d 次推送失败: %v", i, err)
			return
		}
		t.Logf("第 %d 次推送成功", i)
	}

	// 超时
	t.Log("等待 >>>>>>")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	<-ctx.Done()
	t.Log("退出 >>>>>>")

}
