package pushkit

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gogf/gf/v2/container/gqueue"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcache"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/grpool"
)

var (
	_ Sender = (*TgBotSender)(nil)
)

const (
	PushTypeTgBot       PushType      = "TgBot"
	tgBotSenderLogTag   string        = "[TgBot sender]"
	tgBotSendTimeLimit  time.Duration = 3 * time.Second
	defaultCacheTimeout time.Duration = 5 * time.Second
	tgbotLogTag                       = "[TgBot]"
	tgbotLogPath                      = "./log/tgbot.log"
	tgbotLogLevel                     = "debug"
)

type (
	TgBotSender struct {
		config    TgBotConfig
		b         *bot.Bot
		queue     *gqueue.Queue
		cache     *gcache.Cache
		workPoll  *grpool.Pool
		stopCtx   context.Context
		logger    *glog.Logger
		isRunning atomic.Bool // 存储启动状态
	}
	TgBotConfig struct {
		// tg bot token
		BotToken string
		// 自定义的 bot 自动回复
		DefaultHandler bot.HandlerFunc
		// https://github.com/go-telegram/bot/issues/140
		// https://goframe.org/docs/web/http-client-proxy
		// 代理地址，支持 http 和 socks5 两种形式，分别为 http://USER:PASSWORD@IP:PORT 或 socks5://USER:PASSWORD@IP:PORT 形式
		ProxyURL string
	}
	tgBotMessage struct {
		Ctx      context.Context
		Msg      *bot.SendMessageParams
		SendTime *time.Time
	}
)

func WithTgBotSender(config TgBotConfig) (Option, error) {
	sender, err := NewTgBotSender(config)
	if err != nil {
		return nil, err
	}

	return func(ps *PushService) {
		ps.senders.LoadOrStore(PushTypeTgBot, sender)
	}, nil
}

func NewTgBotSender(config TgBotConfig) (*TgBotSender, error) {
	var opts []bot.Option
	if config.DefaultHandler != nil {
		opts = append(opts, bot.WithDefaultHandler(config.DefaultHandler))
	} else {
		opts = append(opts, bot.WithDefaultHandler(defaultHandler))
	}

	// 代理
	if config.ProxyURL != "" {
		client := g.Client()
		client.SetProxy(config.ProxyURL)
		client.SetTimeout(8 * time.Second)
		opts = append(opts, bot.WithHTTPClient(5*time.Second, client)) // 此处的超时时间应小于上面的超时时间，否则报错：[TGBOT] [ERROR] error get updates, error do request for method getUpdates XXXXXXXXXXXX
	}

	b, err := bot.New(config.BotToken, opts...)
	if err != nil {
		return nil, err
	}
	// 设置日志
	l := glog.New()
	_ = l.SetPath(tgbotLogPath)
	_ = l.SetLevelStr(tgbotLogLevel)
	l.SetPrefix(tgbotLogTag)
	l.SetStack(false)

	return &TgBotSender{
		config:   config,
		b:        b,
		queue:    gqueue.New(),
		cache:    gcache.New(),
		workPoll: grpool.New(20),
		logger:   l,
	}, nil
}

func (s *TgBotSender) Push(ctx context.Context, message Message) (err error) {
	if !s.isRunning.Load() {
		return errors.New("tg bot sender not running")
	}
	m := &bot.SendMessageParams{
		Text: message.Content,
	}
	for _, v := range message.To {
		m.ChatID = v
		s.queue.Push(&tgBotMessage{Ctx: ctx, Msg: m})
	}
	return nil
}

func (s *TgBotSender) Start(stopCtx context.Context) (err error) {
	if !s.isRunning.CompareAndSwap(false, true) {
		s.logger.Info(stopCtx, "tg bot sender already running")
		return
	}
	s.stopCtx = stopCtx

	// 启动 bot
	s.workPoll.AddWithRecover(
		stopCtx,
		func(ctx context.Context) {
			s.b.Start(s.stopCtx) // 该库的 b.Start() 会阻塞，所以放到协程里启动
		},
		func(ctx context.Context, exception error) {
			s.logger.Error(ctx, exception.Error())
		})

	// 消息处理
	s.workPoll.AddWithRecover(
		stopCtx,
		func(ctx context.Context) {
			s.loopMessageHandler()
		},
		func(ctx context.Context, exception error) {
			s.logger.Error(ctx, exception.Error())
		})

	return err
}
func (s *TgBotSender) loopMessageHandler() {
	for {
		select {
		case <-s.stopCtx.Done():
			s.clean()
			return
		default:
			originalMessage, err := s.processMessage()
			if err != nil && originalMessage != nil {
				s.logger.Error(originalMessage.Ctx, err.Error())
			}
		}
	}
}
func (s *TgBotSender) clean() {
	ctx := gctx.New()
	s.queue.Close()
	s.cache.Close(ctx)
	s.workPoll.Close()
	s.b.Close(ctx)
	s.isRunning.Store(false)
}

func (s *TgBotSender) processMessage() (msg *tgBotMessage, err error) {
	v := s.queue.Pop()
	if v == nil {
		return nil, errors.New("tg bot queue message null")
	}
	msg = v.(*tgBotMessage)

	s.workPoll.AddWithRecover(
		msg.Ctx,
		func(ctx context.Context) {
			err = s.sendMessage(msg)
			if err != nil {
				s.logger.Error(ctx, err.Error())
			}
		},
		func(ctx context.Context, exception error) {
			s.logger.Error(ctx, exception.Error())
		},
	)
	return
}
func (s *TgBotSender) sendMessage(msg *tgBotMessage) (err error) {
	// 判断是否可以发送
	delay := s.checkRateLimit(msg)
	if delay > 0 {
		time.AfterFunc(delay, func() {
			s.queue.Push(msg) // 延迟后重新入队
		})
		return
	}
	_, err = s.b.SendMessage(msg.Ctx, msg.Msg)
	if err != nil {
		return
	}
	// 更新发送时间
	now := time.Now()
	msg.SendTime = &now
	return s.cache.Set(msg.Ctx, msg.Msg.ChatID, msg, defaultCacheTimeout)
}
func (s *TgBotSender) checkRateLimit(msg *tgBotMessage) (delay time.Duration) {
	ca, _ := s.cache.Get(msg.Ctx, msg.Msg.ChatID)
	if ca == nil {
		return 0 // 无缓存记录，直接发送
	}
	var cachedMsg *tgBotMessage
	if err := ca.Scan(&cachedMsg); err != nil {
		return 0 // 出错时，直接发送
	}
	diff := time.Since(*cachedMsg.SendTime)
	if diff >= tgBotSendTimeLimit {
		return 0 // 超过时间间隔，直接发送
	}
	return tgBotSendTimeLimit - diff // 延时发送
}
func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// log.Println("update.Message:", update.Message)
	// chatId := update.Message.Chat.ID
	// log.Println("chatId:", chatId)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   update.Message.Text,
	})
}
