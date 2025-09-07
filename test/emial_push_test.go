package test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ppoonk/pushkit"

	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/glog"
)

// clear && go test -v test/emial_push_test.go
func TestEmailPush(t *testing.T) {
	// 邮箱配置，替换成有效的邮箱配置
	emailConfig := []pushkit.EmailSenderCoreConfig{
		{
			Host:     os.Getenv("QQEmailHost1"),
			Port:     465,
			Username: os.Getenv("QQEmailTestUser1"),
			Password: os.Getenv("QQEmailTestPassword1"),
			Name:     "QQ Email1",
			Weight:   2,
		},
		{
			Host:     os.Getenv("QQEmailHost2"),
			Port:     465,
			Username: os.Getenv("QQEmailTestUser2"),
			Password: os.Getenv("QQEmailTestPassword2"),
			Name:     "QQ Email2",
			Weight:   1,
		},
	}
	// 日志
	l := glog.New()
	l.SetPath(".log/email")

	// 创建推送服务、启动
	op := pushkit.WithEmailSender(l, emailConfig...)
	p := pushkit.NewPushService(op)
	err := p.Start()
	defer p.Stop()
	if err != nil {
		t.Error("启动错误：", err)
		return
	}

	// 将消息推入队列进行消费
	for i := range 12 {
		req := pushkit.PushRequest{
			Type: pushkit.PushTypeEmail,
			Message: pushkit.Message{
				To:      []string{os.Getenv("EmailTest")},
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
