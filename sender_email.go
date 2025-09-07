package pushkit

import (
	"context"
	"crypto/tls"
	"errors"
	"sync/atomic"

	"github.com/gogf/gf/v2/container/gqueue"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/grpool"
	"github.com/ppoonk/utils/balancer"
	"gopkg.in/gomail.v2"
)

// TODO 根据限制规则，完善功能
/*
可能的一些限制（数据来自网络）：

QQ邮箱：
一次性不能超过10个
一分钟不能超过40封
一天不能超过100封

gmail：
每小时 20 封
每 24 小时期间500 封

163邮箱：
普通用户每日可享有200-400封发信量
基础版会员每日可享有600-800封发信量
超级会员每日可享有1200-1400封发信量

oOutlook.com：
每日收件人：5,000
每封邮件的最大收件人数：500
每日非关系收件人：1,000

*/

const (
	PushTypeEmail     PushType = "Email"
	emailSenderLogTag string   = "[Email sender]"
)

var (
	_ Sender = (*EmailSender)(nil)
)

type (
	EmailSender struct {
		stopCtx   context.Context
		queue     *gqueue.Queue
		workPoll  *grpool.Pool
		logger    *glog.Logger
		swrr      balancer.Balancer[*EmailSenderCore] // 平滑权重轮询器
		isRunning atomic.Bool                         // 存储启动状态
	}
	emailMessage struct {
		Ctx context.Context
		Msg *gomail.Message
	}
)

func WithEmailSender(logger *glog.Logger, config ...EmailSenderCoreConfig) Option {
	return func(ps *PushService) {
		ps.senders.LoadOrStore(PushTypeEmail, NewEmailSender(logger, config...))
	}
}

func NewEmailSender(logger *glog.Logger, config ...EmailSenderCoreConfig) *EmailSender {
	var servers []*EmailSenderCore

	for _, v := range config {
		dialer := gomail.NewDialer(v.Host, v.Port, v.Username, v.Password)
		dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		servers = append(servers, &EmailSenderCore{
			config: v,
			dialer: dialer,
		})
	}
	logger.SetPrefix(logger.GetConfig().Prefix + emailSenderLogTag)

	return &EmailSender{
		queue:    gqueue.New(),
		workPoll: grpool.New(10),
		logger:   logger,
		swrr:     balancer.NewSmoothWeightedRoundRobin(servers),
	}
}

func (s *EmailSender) Push(ctx context.Context, message Message) (err error) {
	if !s.isRunning.Load() {
		return errors.New("email sender not running")
	}

	m := gomail.NewMessage()
	m.SetHeader("Subject", message.Subject)
	m.SetBody("text/plain", message.Content)
	m.SetHeader("To", message.To...)

	s.queue.Push(&emailMessage{Ctx: ctx, Msg: m})
	return nil
}

func (s *EmailSender) Start(stopCtx context.Context) (err error) {
	if !s.isRunning.CompareAndSwap(false, true) {
		s.logger.Info(stopCtx, "email sender already running")
		return
	}
	s.stopCtx = stopCtx
	s.workPoll.AddWithRecover(
		stopCtx,
		func(ctx context.Context) {
			s.loopMessageHandler()
		},
		func(ctx context.Context, exception error) {
			s.logger.Error(ctx, exception.Error())
		})
	return
}
func (s *EmailSender) loopMessageHandler() {
	for {
		select {
		case <-s.stopCtx.Done():
			s.logger.Info(s.stopCtx, "email sender exited")
			s.clean()
			return
		case v, ok := <-s.queue.C:
			if !ok {
				s.logger.Info(s.stopCtx, "email queue closed, worker exited")
				s.clean()
				return
			}
			msg, ok := v.(*emailMessage)
			if !ok {
				continue
			}
			err := s.swrr.Select().Send(msg.Msg)
			if err != nil && msg != nil {
				s.logger.Error(msg.Ctx, err.Error())
			}
		}
	}
}

func (s *EmailSender) clean() {
	s.queue.Close()
	s.workPoll.Close()
	s.isRunning.Store(false)
}
