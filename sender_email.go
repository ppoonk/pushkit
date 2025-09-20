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

const (
	PushTypeEmail PushType = "Email"
	emailLogTag   string   = "[Email sender]"
	emailLogPath  string   = "./log/email/"
	emailLogLevel string   = "debug"
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

func WithEmailSender(config ...EmailSenderCoreConfig) Option {
	return func(ps *PushService) {
		ps.senders.LoadOrStore(PushTypeEmail, NewEmailSender(config...))
	}
}

func NewEmailSender(config ...EmailSenderCoreConfig) *EmailSender {
	var servers []*EmailSenderCore

	for _, v := range config {
		dialer := gomail.NewDialer(v.Host, v.Port, v.Username, v.Password)
		dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		servers = append(servers, &EmailSenderCore{
			config: v,
			dialer: dialer,
		})
	}
	// 设置日志
	l := glog.New()
	_ = l.SetPath(emailLogPath)
	_ = l.SetLevelStr(emailLogLevel)
	l.SetPrefix(emailLogTag)
	l.SetStack(false)

	return &EmailSender{
		queue:    gqueue.New(),
		workPoll: grpool.New(10),
		logger:   l,
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
