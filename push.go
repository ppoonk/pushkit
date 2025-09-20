package pushkit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type (
	PushRequest struct {
		Type    PushType
		Message Message
	}
	Message struct {
		To      []string
		Subject string
		Content string
	}
	PushType string

	Option func(*PushService)

	PushService struct {
		senders   sync.Map // key=PushType, value=Sender
		cancel    context.CancelFunc
		isRunning atomic.Bool
	}

	Sender interface {
		Push(ctx context.Context, message Message) error
		Start(context.Context) error
	}
)

func NewPushService(opts ...Option) *PushService {
	ps := &PushService{}
	for _, opt := range opts {
		opt(ps)
	}
	return ps
}

func (ps *PushService) Push(ctx context.Context, req PushRequest) (err error) {
	if !ps.isRunning.Load() {
		return errors.New("pushService not running ")
	}
	senderVal, ok := ps.senders.Load(req.Type)
	if !ok {
		return errors.New("invalid sender type")
	}

	sender, ok := senderVal.(Sender)
	if !ok {
		return errors.New("invalid sender type")
	}
	return sender.Push(ctx, req.Message)
}

func (ps *PushService) Start() (err error) {
	if !ps.isRunning.CompareAndSwap(false, true) {
		return errors.New("pushService already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ps.cancel = cancel

	ps.senders.Range(func(key, value any) bool {
		sender, ok := value.(Sender)
		if !ok || sender == nil {
			return true
		}

		if err = sender.Start(ctx); err != nil {
			return false // 返回 false，停止迭代。
		}
		return true
	})

	if err != nil {
		ps.cancel()
		ps.isRunning.Store(false)
		return
	}
	return
}

func (ps *PushService) Stop() {
	if !ps.isRunning.Load() {
		return
	}
	ps.cancel()
	ps.isRunning.Store(false)
}
