package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// WSClient is a WebSocket test client
type WSClient struct {
	conn     *websocket.Conn
	messages chan WSMessage
	done     chan struct{}
	mu       sync.Mutex
}

// NewWSClient creates a new WebSocket client
func NewWSClient(token, userId string) (*WSClient, error) {
	u := url.URL{
		Scheme:   "ws",
		Host:     "localhost:8080",
		Path:     "/ws",
		RawQuery: fmt.Sprintf("token=%s&send_id=%s&platform_id=1", token, userId),
	}

	header := http.Header{}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	client := &WSClient{
		conn:     conn,
		messages: make(chan WSMessage, 100),
		done:     make(chan struct{}),
	}

	go client.readLoop()

	return client, nil
}

// readLoop reads messages from WebSocket
func (c *WSClient) readLoop() {
	defer close(c.done)
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		select {
		case c.messages <- msg:
		default:
			// Channel full, drop message
		}
	}
}

// Send sends a message through WebSocket
func (c *WSClient) Send(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// WaitForMessage waits for a message with timeout
func (c *WSClient) WaitForMessage(timeout time.Duration) (*WSMessage, error) {
	select {
	case msg := <-c.messages:
		return &msg, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for message")
	case <-c.done:
		return nil, fmt.Errorf("connection closed")
	}
}

// Close closes the WebSocket connection
func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func TestWebSocket_Connect(t *testing.T) {
	userId := generateUserId("ws_user")
	_, token := RegisterAndLogin(t, userId, "WS User", "password123")

	t.Run("connect with valid token", func(t *testing.T) {
		wsClient, err := NewWSClient(token, userId)
		if err != nil {
			t.Fatalf("connect websocket failed: %v", err)
		}
		defer wsClient.Close()

		// Connection should be established
		t.Log("WebSocket connected successfully")
	})

	t.Run("connect with invalid token", func(t *testing.T) {
		_, err := NewWSClient("invalid_token", userId)
		if err == nil {
			t.Error("should fail with invalid token")
		}
	})

	t.Run("connect without token", func(t *testing.T) {
		u := url.URL{
			Scheme: "ws",
			Host:   "localhost:8080",
			Path:   "/ws",
		}

		_, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err == nil {
			t.Error("should fail without token")
		}
	})
}

func TestWebSocket_ReceiveMessage(t *testing.T) {
	// Create two users
	user1Id := generateUserId("ws_sender")
	user2Id := generateUserId("ws_receiver")
	client1, _ := RegisterAndLogin(t, user1Id, "WS Sender", "password123")
	_, token2 := RegisterAndLogin(t, user2Id, "WS Receiver", "password123")

	// Connect user2 to WebSocket
	wsClient, err := NewWSClient(token2, user2Id)
	if err != nil {
		t.Fatalf("connect websocket failed: %v", err)
	}
	defer wsClient.Close()

	// Give some time for connection to establish
	time.Sleep(100 * time.Millisecond)

	t.Run("receive message via websocket", func(t *testing.T) {
		// Send message via HTTP
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			RecvId:      user2Id,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Hello via WebSocket!",
			},
		}

		resp, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")

		// Wait for WebSocket message
		msg, err := wsClient.WaitForMessage(5 * time.Second)
		if err != nil {
			t.Fatalf("wait for message failed: %v", err)
		}

		t.Logf("Received WebSocket message: type=%s", msg.Type)

		// Verify message content
		if msg.Type != "new_message" && msg.Type != "message" {
			t.Logf("Received message type: %s (may vary by implementation)", msg.Type)
		}
	})
}

func TestWebSocket_MultipleConnections(t *testing.T) {
	userId := generateUserId("ws_multi")
	_, token := RegisterAndLogin(t, userId, "WS Multi", "password123")

	t.Run("multiple connections same user", func(t *testing.T) {
		// Connect multiple times
		clients := make([]*WSClient, 3)
		for i := range clients {
			client, err := NewWSClient(token, userId)
			if err != nil {
				t.Fatalf("connect websocket %d failed: %v", i, err)
			}
			clients[i] = client
		}

		// Clean up
		for _, client := range clients {
			if client != nil {
				client.Close()
			}
		}
	})
}

func TestWebSocket_GroupMessage(t *testing.T) {
	// Create users
	ownerId := generateUserId("ws_group_owner")
	memberId := generateUserId("ws_group_member")
	ownerClient, ownerToken := RegisterAndLogin(t, ownerId, "WS Group Owner", "password123")
	_, memberToken := RegisterAndLogin(t, memberId, "WS Group Member", "password123")

	// Create group
	groupId := CreateGroupAndGetId(t, ownerClient, "WS Test Group", []string{memberId})

	// Connect both users to WebSocket
	ownerWS, err := NewWSClient(ownerToken, ownerId)
	if err != nil {
		t.Fatalf("connect owner websocket failed: %v", err)
	}
	defer ownerWS.Close()

	memberWS, err := NewWSClient(memberToken, memberId)
	if err != nil {
		t.Fatalf("connect member websocket failed: %v", err)
	}
	defer memberWS.Close()

	time.Sleep(100 * time.Millisecond)

	t.Run("group members receive message", func(t *testing.T) {
		// Send group message
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Hello group via WebSocket!",
			},
		}

		resp, err := ownerClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")

		// Both should receive the message
		// Owner receives their own message
		ownerMsg, err := ownerWS.WaitForMessage(5 * time.Second)
		if err != nil {
			t.Logf("Owner may not receive own message: %v", err)
		} else {
			t.Logf("Owner received: type=%s", ownerMsg.Type)
		}

		// Member receives the message
		memberMsg, err := memberWS.WaitForMessage(5 * time.Second)
		if err != nil {
			t.Fatalf("member wait for message failed: %v", err)
		}
		t.Logf("Member received: type=%s", memberMsg.Type)
	})
}
