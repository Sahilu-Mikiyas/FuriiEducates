package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// outsideMessage is one message to send on a channel.
type outsideMessage struct {
	Channel, Address, Subject, Body string
}

// sender sends messages on one channel and returns the provider's reference.
type sender interface {
	Send(ctx context.Context, m outsideMessage) (string, error)
}

// permanentError marks a failure that retrying will not fix, such as a
// rejected address or a wrong key.
type permanentError struct{ error }

func permanent(err error) error { return permanentError{err} }

// notifier holds the senders for the channels this service has set up. A nil
// sender means that channel is not set up.
type notifier struct {
	email, sms, telegram sender
	emailProvider        string
	smsProvider          string
	telegramBot          string // the bot's username, for t.me links
	telegramToken        string
	telegramAPI          string
	webURL               string // the web app's address, for links in messages
	client               *http.Client
}

func (n *notifier) ready(channel string) bool {
	return n.senderFor(channel) != nil
}

func (n *notifier) senderFor(channel string) sender {
	if n == nil {
		return nil
	}
	switch channel {
	case "email":
		return n.email
	case "sms":
		return n.sms
	case "telegram":
		return n.telegram
	}
	return nil
}

// newNotifier reads the channel settings:
//   - PUBLIC_WEB_URL: the web app's address for links in messages. Defaults to
//     the first https origin in CORS_ALLOWED_ORIGINS.
//   - Email: RESEND_API_KEY and EMAIL_FROM for Resend, which works on Render's
//     free plan; or SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD and
//     EMAIL_FROM. Render's free plan blocks SMTP.
//   - SMS: SMS_PROVIDER=afromessage with AFROMESSAGE_TOKEN, and optionally
//     AFROMESSAGE_IDENTIFIER_ID and AFROMESSAGE_SENDER_NAME; or
//     SMS_PROVIDER=webhook with SMS_WEBHOOK_URL and SMS_WEBHOOK_TOKEN, which
//     receives {"to","message"} as JSON.
//   - Telegram: TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_USERNAME.
func newNotifier(allowedOrigins []string) *notifier {
	env := func(key string) string { return strings.TrimSpace(os.Getenv(key)) }
	n := &notifier{client: &http.Client{Timeout: 20 * time.Second}}
	n.webURL = strings.TrimRight(env("PUBLIC_WEB_URL"), "/")
	if n.webURL == "" {
		for _, origin := range allowedOrigins {
			if strings.HasPrefix(origin, "https://") {
				n.webURL = strings.TrimRight(origin, "/")
				break
			}
		}
	}
	from := env("EMAIL_FROM")
	switch {
	case env("RESEND_API_KEY") != "" && from != "":
		n.email = &resendSender{key: env("RESEND_API_KEY"), from: from, url: envOr("RESEND_API_URL", "https://api.resend.com/emails"), client: n.client}
		n.emailProvider = "Resend"
	case env("SMTP_HOST") != "" && from != "":
		n.email = &smtpSender{host: env("SMTP_HOST"), port: envOr("SMTP_PORT", "587"), username: env("SMTP_USERNAME"), password: os.Getenv("SMTP_PASSWORD"), from: from}
		n.emailProvider = "SMTP"
	}
	switch strings.ToLower(env("SMS_PROVIDER")) {
	case "afromessage":
		if env("AFROMESSAGE_TOKEN") != "" {
			n.sms = &afroMessageSender{token: env("AFROMESSAGE_TOKEN"), identifier: env("AFROMESSAGE_IDENTIFIER_ID"), senderName: env("AFROMESSAGE_SENDER_NAME"),
				url: envOr("AFROMESSAGE_API_URL", "https://api.afromessage.com/api/send"), client: n.client}
			n.smsProvider = "AfroMessage"
		}
	case "webhook":
		if strings.HasPrefix(env("SMS_WEBHOOK_URL"), "https://") || strings.HasPrefix(env("SMS_WEBHOOK_URL"), "http://localhost") || strings.HasPrefix(env("SMS_WEBHOOK_URL"), "http://127.0.0.1") {
			n.sms = &webhookSMSSender{url: env("SMS_WEBHOOK_URL"), token: env("SMS_WEBHOOK_TOKEN"), client: n.client}
			n.smsProvider = "Webhook"
		}
	}
	if env("TELEGRAM_BOT_TOKEN") != "" && env("TELEGRAM_BOT_USERNAME") != "" {
		n.telegramToken = env("TELEGRAM_BOT_TOKEN")
		n.telegramBot = strings.TrimPrefix(env("TELEGRAM_BOT_USERNAME"), "@")
		n.telegramAPI = envOr("TELEGRAM_API_URL", "https://api.telegram.org")
		n.telegram = &telegramSender{n: n}
	}
	return n
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// postJSON sends a JSON request and returns the response body. A 4xx answer
// other than 408 and 429 is permanent.
func postJSON(ctx context.Context, client *http.Client, url, bearer string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, permanent(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		failure := fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(truncateRunes(string(body), 200)))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 && resp.StatusCode != 429 {
			return body, permanent(failure)
		}
		return body, failure
	}
	return body, nil
}

type resendSender struct {
	key, from, url string
	client         *http.Client
}

func (s *resendSender) Send(ctx context.Context, m outsideMessage) (string, error) {
	body, err := postJSON(ctx, s.client, s.url, s.key, map[string]any{"from": s.from, "to": []string{m.Address}, "subject": m.Subject, "text": m.Body})
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &out)
	return out.ID, nil
}

type smtpSender struct {
	host, port, username, password, from string
}

func (s *smtpSender) Send(ctx context.Context, m outsideMessage) (string, error) {
	addr := net.JoinHostPort(s.host, s.port)
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if s.port == "465" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return "", err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return "", err
	}
	defer client.Close()
	if s.port != "465" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return "", permanent(errors.New("the mail server does not offer STARTTLS"))
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return "", err
		}
	}
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return "", permanent(err)
		}
	}
	fromAddress := s.from
	if start := strings.LastIndex(fromAddress, "<"); start >= 0 {
		fromAddress = strings.TrimSuffix(fromAddress[start+1:], ">")
	}
	if err := client.Mail(fromAddress); err != nil {
		return "", err
	}
	if err := client.Rcpt(m.Address); err != nil {
		return "", permanent(err)
	}
	w, err := client.Data()
	if err != nil {
		return "", err
	}
	headers := "From: " + s.from + "\r\nTo: " + m.Address + "\r\nSubject: " + mime.QEncoding.Encode("utf-8", m.Subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n"
	if _, err := w.Write([]byte(headers + strings.ReplaceAll(m.Body, "\n", "\r\n"))); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return "", client.Quit()
}

// afroMessageSender sends through AfroMessage, an Ethiopian SMS gateway. The
// request shape follows AfroMessage's published client libraries; check it
// against the account's own documentation when the account is opened.
type afroMessageSender struct {
	token, identifier, senderName, url string
	client                             *http.Client
}

func (s *afroMessageSender) Send(ctx context.Context, m outsideMessage) (string, error) {
	payload := map[string]string{"to": m.Address, "message": m.Body}
	if s.identifier != "" {
		payload["from"] = s.identifier
	}
	if s.senderName != "" {
		payload["sender"] = s.senderName
	}
	body, err := postJSON(ctx, s.client, s.url, s.token, payload)
	if err != nil {
		return "", err
	}
	var out struct {
		Acknowledge string          `json:"acknowledge"`
		Response    json.RawMessage `json:"response"`
	}
	if json.Unmarshal(body, &out) == nil && out.Acknowledge != "" && out.Acknowledge != "success" {
		return "", permanent(fmt.Errorf("AfroMessage refused the message: %s", truncateRunes(string(out.Response), 200)))
	}
	var ref struct {
		MessageID string `json:"message_id"`
	}
	_ = json.Unmarshal(out.Response, &ref)
	return ref.MessageID, nil
}

type webhookSMSSender struct {
	url, token string
	client     *http.Client
}

func (s *webhookSMSSender) Send(ctx context.Context, m outsideMessage) (string, error) {
	_, err := postJSON(ctx, s.client, s.url, s.token, map[string]string{"to": m.Address, "message": m.Body})
	return "", err
}

type telegramSender struct{ n *notifier }

func (s *telegramSender) Send(ctx context.Context, m outsideMessage) (string, error) {
	body, err := postJSON(ctx, s.n.client, s.n.telegramAPI+"/bot"+s.n.telegramToken+"/sendMessage", "", map[string]any{"chat_id": m.Address, "text": m.Body, "disable_web_page_preview": true})
	if err != nil {
		// The token must not reach logs or the delivery record.
		return "", redactToken(err, s.n.telegramToken)
	}
	var out struct {
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &out)
	return fmt.Sprint(out.Result.MessageID), nil
}

func redactToken(err error, token string) error {
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	clean := errors.New(strings.ReplaceAll(err.Error(), token, "[token]"))
	var p permanentError
	if errors.As(err, &p) {
		return permanent(clean)
	}
	return clean
}
