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
	Type           string      `json:"type"`
	ConversationID uuid.UUID   `json:"conversation_id"`
	ReceiverID     uuid.UUID   `json:"receiver_id"`
	Content        string      `json:"content"`
	Signal         interface{} `json:"signal,omitempty"` // WebRTC SDP / ICE payload
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

func (hub *chatHubState) getOnlineUserIDs() []uuid.UUID {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(hub.clients))
	for id := range hub.clients {
		ids = append(ids, id)
	}
	return ids
}

func (hub *chatHubState) isOnline(userID uuid.UUID) bool {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return len(hub.clients[userID]) > 0
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

	go chatWritePump(client)
	client.send <- chatWSEvent{
		Type:    "connected",
		Message: "WebSocket connected",
		Data: map[string]interface{}{
			"user_id":      user.ID,
			"online_users": chatHub.getOnlineUserIDs(),
		},
	}

	// Broadcast online status to conversation partners
	go broadcastPresence(user.ID, "user_online")

	chatReadPump(client, user)

	// Client disconnected — persist last seen then broadcast
	chatHub.unregister(client)
	if !chatHub.isOnline(user.ID) {
		_ = repo.UpdateUserLastSeen(user.ID)
		go broadcastPresence(user.ID, "user_offline")
	}
}

func broadcastPresence(userID uuid.UUID, eventType string) {
	conversations, err := repo.ListConversations(userID)
	if err != nil {
		return
	}

	eventData := map[string]interface{}{"user_id": userID}
	if eventType == "user_offline" {
		eventData["last_seen_at"] = time.Now()
	}

	notified := map[uuid.UUID]bool{}
	for _, conv := range conversations {
		memberIDs, err := repo.GetConversationMemberIDs(conv.ID)
		if err != nil {
			continue
		}
		filtered := make([]uuid.UUID, 0, len(memberIDs))
		for _, id := range memberIDs {
			if !notified[id] {
				notified[id] = true
				filtered = append(filtered, id)
			}
		}
		chatHub.sendToUsers(filtered, chatWSEvent{
			Type: eventType,
			Data: eventData,
		})
	}
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
		case "call_offer", "call_answer", "call_ice", "call_end", "call_reject":
			handleCallSignal(client, user, req, eventType)
		case "":
			client.send <- chatWSEvent{Type: "error", Message: "type is required"}
		default:
			client.send <- chatWSEvent{Type: "error", Message: "Unsupported type"}
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

func handleCallSignal(client *chatClient, user model.User, req chatWSRequest, eventType string) {
	if req.ReceiverID == uuid.Nil {
		client.send <- chatWSEvent{Type: "error", Message: "receiver_id required for call signals"}
		return
	}
	// Khi gửi offer mà receiver không online → báo lại ngay cho caller
	if eventType == "call_offer" && !chatHub.isOnline(req.ReceiverID) {
		client.send <- chatWSEvent{
			Type: "call_unavailable",
			Data: map[string]interface{}{"receiver_id": req.ReceiverID},
		}
		return
	}
	chatHub.sendToUsers([]uuid.UUID{req.ReceiverID}, chatWSEvent{
		Type: eventType, // use pre-normalized type from switch
		Data: map[string]interface{}{
			"caller_id": user.ID,
			"signal":    req.Signal,
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
