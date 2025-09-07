package pushkit

import (
	"crypto/tls"
	"sync/atomic"

	"github.com/ppoonk/utils/balancer"
	"gopkg.in/gomail.v2"
)

func _check[T balancer.WeightedServer[T]]() {
	var _ balancer.WeightedServer[T] = (*EmailSenderCore)(nil)
}

type EmailSenderCore struct {
	config        EmailSenderCoreConfig
	currentWeight atomic.Int32
	dialer        *gomail.Dialer
}
type EmailSenderCoreConfig struct {
	Name     string
	Weight   int
	Host     string
	Port     int
	Username string
	Password string
}

func NewEmailSenderCore(config EmailSenderCoreConfig) *EmailSenderCore {
	dialer := gomail.NewDialer(config.Host, config.Port, config.Username, config.Password)
	dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true} //关闭TLS认证
	return &EmailSenderCore{
		config: config,
		dialer: dialer,
	}
}

func (s *EmailSenderCore) Send(m ...*gomail.Message) error {
	for _, v := range m {
		v.SetHeader("From", s.dialer.Username)
	}
	return s.dialer.DialAndSend(m...)
}
func (s *EmailSenderCore) GetWeight() int {
	return s.config.Weight
}
func (s *EmailSenderCore) GetCurrentWeight() int {
	return int(s.currentWeight.Load())

}
func (s *EmailSenderCore) SetCurrentWeight(w int) {
	old := s.currentWeight.Load()
	s.currentWeight.CompareAndSwap(old, int32(w))
}
func (s *EmailSenderCore) GetName() string {
	return s.config.Name
}
