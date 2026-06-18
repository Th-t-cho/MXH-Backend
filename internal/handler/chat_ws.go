package handler

import (
	"core/internal/model"
	"core/internal/repo"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/google/uuid"
)

type chatWSRequest struct {
	Type           string    `json:"type"`
	ConversationID uuid.UUID `json:"conversation_id"`
	ReceiverID     uuid.UUID `json:"receiver_id"`
	Content        string    `json:"content"`
}

type chatWSEvent struct {
	Type           string      `json:"type"`
	ConversationID uuid.UUID   `json:"conversation_id,omitempty"`
	Data           interface{} `json:"data,omitempty"`
	Message        string      `json:"message,omitempty"`
}

type chatClient struct {
	userID uuid.UUID
	conn   *websocket.Conn
	send   chan chatWSEvent
}

type chatHubState struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*chatClient]struct{}
}

var chatHub = &chatHubState{
	clients: make(map[uuid.UUID]map[*chatClient]struct{}),
}

func (hub *chatHubState) register(client *chatClient) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if hub.clients[client.userID] == nil {
		hub.clients[client.userID] = make(map[*chatClient]struct{})
	}
	hub.clients[client.userID][client] = struct{}{}
}

func (hub *chatHubState) unregister(client *chatClient) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	userClients := hub.clients[client.userID]
	if userClients == nil {
		return
	}
	if _, ok := userClients[client]; ok {
		delete(userClients, client)
		close(client.send)
	}
	if len(userClients) == 0 {
		delete(hub.clients, client.userID)
	}
}

func (hub *chatHubState) sendToUsers(userIDs []uuid.UUID, event chatWSEvent) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	for _, userID := range userIDs {
		for client := range hub.clients[userID] {
			select {
			case client.send <- event:
			default:
			}
		}
	}
}

// ChatWebSocket handles realtime chat events.
func ChatWebSocket(c *websocket.Conn) {
	user, err := currentUserFromToken(websocketToken(c))
	if err != nil {
		_ = c.WriteJSON(chatWSEvent{Type: "error", Message: "Unauthorized"})
		_ = c.Close()
		return
	}

	client := &chatClient{
		userID: user.ID,
		conn:   c,
		send:   make(chan chatWSEvent, 32),
	}
	chatHub.register(client)
	defer chatHub.unregister(client)

	go chatWritePump(client)
	client.send <- chatWSEvent{
		Type:    "connected",
		Message: "WebSocket connected",
		Data: map[string]interface{}{
			"user_id": user.ID,
		},
	}
	chatReadPump(client, user)
}

func websocketToken(c *websocket.Conn) string {
	token := strings.TrimSpace(c.Query("token"))
	if token != "" {
		return token
	}

	authHeader := strings.TrimSpace(c.Headers("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

func chatWritePump(client *chatClient) {
	for event := range client.send {
		if err := client.conn.WriteJSON(event); err != nil {
			return
		}
	}
}

func chatReadPump(client *chatClient, user model.User) {
	for {
		req := chatWSRequest{}
		if err := client.conn.ReadJSON(&req); err != nil {
			return
		}

		eventType := strings.ToLower(strings.TrimSpace(req.Type))
		switch eventType {
		case "send_message":
			handleChatSendMessage(client, user, req)
		case "typing":
			handleChatBroadcast(client, user, req.ConversationID, "typing")
		case "seen":
			handleChatSeen(client, user, req.ConversationID)
		case "":
			client.send <- chatWSEvent{Type: "error", Message: "type is required"}
		default:
			client.send <- chatWSEvent{Type: "error", Message: "Unsupported event type. Allowed: send_message, typing, seen"}
		}
	}
}

func handleChatSendMessage(client *chatClient, user model.User, req chatWSRequest) {
	conversationID := req.ConversationID
	if conversationID == uuid.Nil {
		if req.ReceiverID == uuid.Nil {
			client.send <- chatWSEvent{Type: "error", Message: "receiver_id is required"}
			return
		}

		conversation, err := repo.GetOrCreateDirectConversation(user.ID, req.ReceiverID)
		if err != nil {
			client.send <- chatWSEvent{Type: "error", Message: "Failed to create conversation"}
			return
		}
		conversationID = conversation.ID
	}

	message, err := repo.CreateMessage(conversationID, user.ID, req.Content)
	if err != nil {
		client.send <- chatWSEvent{Type: "error", ConversationID: conversationID, Message: "Failed to send message"}
		return
	}

	userIDs, err := repo.GetConversationMemberIDs(conversationID)
	if err != nil {
		client.send <- chatWSEvent{Type: "error", ConversationID: conversationID, Message: "Failed to deliver message"}
		return
	}

	chatHub.sendToUsers(userIDs, chatWSEvent{
		Type:           "message",
		ConversationID: conversationID,
		Data:           message,
	})
}

func handleChatBroadcast(client *chatClient, user model.User, conversationID uuid.UUID, eventType string) {
	if conversationID == uuid.Nil {
		client.send <- chatWSEvent{Type: "error", Message: "conversation_id is required"}
		return
	}

	if ok, err := repo.IsConversationMember(conversationID, user.ID); err != nil || !ok {
		client.send <- chatWSEvent{Type: "error", ConversationID: conversationID, Message: "Access denied"}
		return
	}

	userIDs, err := repo.GetConversationMemberIDs(conversationID)
	if err != nil {
		client.send <- chatWSEvent{Type: "error", ConversationID: conversationID, Message: "Failed to broadcast event"}
		return
	}

	chatHub.sendToUsers(userIDs, chatWSEvent{
		Type:           eventType,
		ConversationID: conversationID,
		Data: map[string]interface{}{
			"user_id": user.ID,
			"time":    time.Now(),
		},
	})
}

func handleChatSeen(client *chatClient, user model.User, conversationID uuid.UUID) {
	if conversationID == uuid.Nil {
		client.send <- chatWSEvent{Type: "error", Message: "conversation_id is required"}
		return
	}

	if err := repo.MarkConversationRead(conversationID, user.ID); err != nil {
		client.send <- chatWSEvent{Type: "error", ConversationID: conversationID, Message: "Failed to mark seen"}
		return
	}

	handleChatBroadcast(client, user, conversationID, "seen")
}
