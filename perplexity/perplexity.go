package perplexity

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"time"
)

const (
	apiURL    = "https://labs-api.perplexity.ai/socket.io/"
	wsURL     = "wss://labs-api.perplexity.ai/socket.io/"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:123.0) Gecko/20100101 Firefox/123.0"
)

const (
	CLAUDE = "claude-3-haiku-20240307"
)

var (
	// Make sure we don't get cloudflare'd.
	tlsConfig = &tls.Config{
		MinVersion:       tls.VersionTLS13,
		CipherSuites:     []uint16{tls.TLS_AES_128_GCM_SHA256},
		CurvePreferences: []tls.CurveID{tls.X25519},
	}

	ALL_MODELS = []string{
		"sonar-small-online",
		"sonar-medium-online",
		"sonar-small-chat",
		"sonar-medium-chat",
		"claude-3-haiku-20240307",
		"codellama-70b-instruct",
		"mistral-7b-instruct",
		"llava-v1.5-7b-wrapper",
		"llava-v1.6-34b",
		"mixtral-8x7b-instruct",
		"mistral-medium",
		"gemma-2b-it",
		"gemma-7b-it",
		"related",
	}
)

type ChatClient struct {
	t                string
	sid              string
	client           *http.Client
	jar              *cookiejar.Jar
	History          []Message
	websocket        *websocket.Conn
	receive          chan string
	ctx              context.Context
	cancel           context.CancelFunc
	debug            bool
	conversationMode bool
}

func NewChatClient(debug bool, conversationMode bool) *ChatClient {
	ctx, cancel := context.WithCancel(context.Background())
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Println("unable to create cookie jar:", err)
	}
	return &ChatClient{
		t: fmt.Sprintf("%08x", rand.Uint32()),
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
			Timeout: time.Second * 25,
			Jar:     jar,
		},
		jar:              jar,
		History:          []Message{},
		receive:          make(chan string, 1),
		debug:            debug,
		ctx:              ctx,
		cancel:           cancel,
		conversationMode: conversationMode,
	}
}

func (c *ChatClient) Backtrack() error {
	if len(c.History) < 2 {
		return errors.New("no history to backtrack")
	}
	c.History = c.History[:len(c.History)-2]
	return nil
}

func (c *ChatClient) ReadForever() error {
	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
			_, message, err := c.websocket.ReadMessage()
			if err != nil {
				return err
			}
			str := string(message)
			if c.debug {
				log.Println("[RECV] Received from perplexity labs: ", str)
			}

			switch {
			case str == "2":
				if err := c.websocket.WriteMessage(websocket.TextMessage, []byte("3")); err != nil {
					return err
				}
			case len(str) < 2 || str[:2] != "42":
				return fmt.Errorf("unexpected message: %s", str)
			default:
				var ex []interface{}
				if err := json.Unmarshal([]byte(str[2:]), &ex); err != nil {
					return err
				}

				if len(ex) != 2 {
					return fmt.Errorf("unexpected JSON structure: %v", ex)
				}

				_, ok := ex[0].(string)
				if !ok {
					return errors.New("unexpected type for the first element")
				}

				outputData, ok := ex[1].(map[string]interface{})
				if !ok {
					return errors.New("unexpected type for the second element")
				}

				var response Response
				mrs, err := json.Marshal(outputData)
				if err != nil {
					return err
				}
				err = json.Unmarshal(mrs, &response)
				if err != nil {
					return err
				}

				if response.Final && response.Status == "completed" {
					c.History = append(c.History, Message{
						Role:     "assistant",
						Content:  response.Output,
						Priority: 0,
					})
					if c.conversationMode {
						fmt.Println(fmt.Sprintf("\r\nAssistant:\r\n %s\r\n", response.Output))
					}
					c.receive <- response.Output
				}
			}
		}
	}
}

func (c *ChatClient) Connect() error {
	sid, err := c.getSessionID()
	if err != nil {
		return err
	}
	c.sid = sid
	if err := c.postData(); err != nil {
		return err
	}

	if err := c.getData(); err != nil {
		return err
	}

	c.websocket, err = c.connectWebSocket()
	if err != nil {
		return err
	}

	if c.debug {
		log.Println("Connected to websocket")
	}
	if err := c.websocket.WriteMessage(websocket.TextMessage, []byte("2probe")); err != nil {
		return err
	}
	_, message, err := c.websocket.ReadMessage()
	if err != nil {
		return err
	}
	if string(message) != "3probe" {
		return fmt.Errorf("unexpected response: %s", message)
	}

	if err := c.websocket.WriteMessage(websocket.TextMessage, []byte("5")); err != nil {
		return err
	}
	_, message, err = c.websocket.ReadMessage()
	if err != nil {
		return err
	}
	if string(message) != "6" {
		return fmt.Errorf("unexpected response: %s", message)
	}
	go c.ReadForever()
	return nil
}

func (c *ChatClient) Close() error {
	c.cancel()
	close(c.receive)
	if err := c.websocket.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		return err
	}
	return c.websocket.Close()
}

func (c *ChatClient) ReceiveMessage(timeout time.Duration) (string, error) {
	select {
	case msg := <-c.receive:
		return msg, nil
	case <-time.After(timeout):
		return "", errors.New("timeout waiting for message")
	}
}

func (c *ChatClient) SendMessage(message string, model string) error {
	if c.conversationMode {
		fmt.Println(fmt.Sprintf("\r\nUser:\r\n %s\r\n", message))
	}
	c.History = append(c.History, Message{
		Role:     "user",
		Content:  message,
		Priority: 0,
	})
	req := Request{
		Version:  "2.5",
		Source:   "default",
		Model:    model,
		Messages: c.History,
		Timezone: "Europe/Athens",
	}

	x, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if c.debug {
		log.Println("[SEND] Sending message to perplexity labs: ", string(x))
	}
	if err := c.websocket.WriteMessage(websocket.TextMessage, []byte("42[\"perplexity_labs\","+string(x)+"]")); err != nil {
		return err
	}

	return nil
}

func (c *ChatClient) getSessionID() (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s?EIO=4&transport=polling&t=%s", apiURL, c.t), nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("User-Agent", userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	read, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	skippedStr := string(read)[1:]
	var data struct {
		Sid string `json:"sid"`
	}

	if err := json.NewDecoder(bytes.NewReader([]byte(skippedStr))).Decode(&data); err != nil {
		return "", err
	}

	return data.Sid, nil
}

func (c *ChatClient) postData() error {
	postData := []byte(`40{"jwt":"anonymous-ask-user"}`)
	req, err := http.NewRequest("POST", fmt.Sprintf("%s?EIO=4&transport=polling&t=%s&sid=%s", apiURL, c.t, c.sid), bytes.NewReader(postData))
	if err != nil {
		return err
	}
	req.Header.Add("User-Agent", userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *ChatClient) getData() error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s?EIO=4&transport=polling&t=%s&sid=%s", apiURL, c.t, c.sid), nil)
	if err != nil {
		return err
	}
	req.Header.Add("User-Agent", userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *ChatClient) connectWebSocket() (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		TLSClientConfig: tlsConfig,
		Jar:             c.jar,
	}
	conn, _, err := dialer.Dial(fmt.Sprintf("%s?EIO=4&transport=websocket&sid=%s", wsURL, c.sid), http.Header{
		"User-Agent": []string{userAgent},
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}
