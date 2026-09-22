package max

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	maxbot "github.com/max-messenger/max-bot-api-client-go"
	"github.com/max-messenger/max-bot-api-client-go/schemes"
)

type Options struct {
	// InsecureSkipVerify is a temporary local workaround for certificate errors.
	InsecureSkipVerify bool
}

//go:embed certs/russian_trusted_root_ca.pem
var russianRootCA []byte

// Run starts long polling and echoes text messages until ctx is cancelled.
func Run(ctx context.Context, token string, options Options) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("переменная окружения MAX_BOT_TOKEN не задана")
	}
	client, err := newHTTPClient(options)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	if options.InsecureSkipVerify {
		slog.Warn("MAX: проверка TLS-сертификата временно отключена")
	}
	api, err := maxbot.New(strings.TrimSpace(token), maxbot.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("создание клиента MAX: %w", err)
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	info, err := api.Bots.GetBot(startup)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		slog.Warn("не удалось получить сведения о боте; пробуем получать обновления", "error", err)
	} else if info != nil {
		slog.Info("подключение к MAX установлено", "name", info.Name, "username", info.Username)
	}
	slog.Info("MAX эхо-бот запущен")
	updates, failures := api.GetUpdates(ctx), api.GetErrors()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			if ctx.Err() == nil {
				slog.Warn("ошибка MAX SDK", "error", err)
			}
		case update, ok := <-updates:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("канал обновлений MAX закрыт")
			}
			if ctx.Err() != nil {
				return nil
			}
			if err := echo(ctx, api, update); err != nil && ctx.Err() == nil {
				slog.Warn("не удалось отправить эхо-ответ", "error", err)
			}
		}
	}
}

func echo(ctx context.Context, api *maxbot.Api, update schemes.UpdateInterface) error {
	created, ok := update.(*schemes.MessageCreatedUpdate)
	if !ok || created == nil || created.Message.Sender.IsBot {
		return nil
	}
	text := created.Message.Body.Text
	if strings.TrimSpace(text) == "" {
		text = "[сообщение без текста или вложение]"
	}
	msg := maxbot.NewMessage().SetText("Эхо: " + text)
	if created.Message.Recipient.ChatId != 0 {
		msg.SetChat(created.Message.Recipient.ChatId)
	} else if created.Message.Sender.UserId != 0 {
		msg.SetUser(created.Message.Sender.UserId)
	} else {
		return errors.New("в сообщении MAX отсутствует адресат для ответа")
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return api.Messages.Send(sendCtx, msg)
}

func newHTTPClient(options Options) (*http.Client, error) {
	return newHTTPClientWithCA(options, russianRootCA)
}

func newHTTPClientWithCA(options Options, caPEM []byte) (*http.Client, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("чтение системных корневых сертификатов: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("не удалось загрузить корневой сертификат MAX из PEM")
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("стандартный HTTP-транспорт Go недоступен")
	}
	transport := base.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	// Only this MAX client is affected. Never mutate http.DefaultTransport.
	transport.TLSClientConfig.RootCAs = roots
	transport.TLSClientConfig.InsecureSkipVerify = options.InsecureSkipVerify
	return &http.Client{
		Transport:     transport,
		Timeout:       90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}
