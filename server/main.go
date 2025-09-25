package main

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/litG-zen/chat_app/proto"
	"google.golang.org/grpc"
)

type Client struct {
	userID string
	stream pb.ChatService_ChatServer
	send   chan *pb.ChatMessage
	ctx    context.Context
	cancel context.CancelFunc
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*Client),
	}
}

func (h *Hub) Register(c *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c.userID]; ok {
		return false
	}
	h.clients[c.userID] = c
	return true
}

func (h *Hub) Unregister(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[userID]; ok {
		close(c.send)
		delete(h.clients, userID)
	}
}

func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

// deliver to specific users (non-blocking)
func (h *Hub) SendTo(recipients []string, msg *pb.ChatMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, uid := range recipients {
		if c, ok := h.clients[uid]; ok {
			select {
			case c.send <- msg:
			default:
				log.Printf("dropping message for %s (send buffer full)", uid)
			}
		} else {
			log.Printf("user %s offline, persisting message", uid)
			// ToDo : Add redis data logging for message persistence
		}
	}
}

// Broadcast sends msg to all connected clients (non-blocking).
// It will attempt a non-blocking send to each client's send channel.
func (h *Hub) Broadcast(msg *pb.ChatMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for uid, c := range h.clients {
		select {
		case c.send <- msg:
		default:
			log.Printf("dropping broadcast message for %s (send buffer full)", uid)
		}
	}
}

type Server struct {
	pb.UnimplementedChatServiceServer
	hub *Hub
}

func NewServer() *Server {
	return &Server{hub: NewHub()}
}

func containsStar(recipients []string) bool {
	for _, r := range recipients {
		if r == "*" {
			return true
		}
	}
	return false
}

func (s *Server) Chat(stream pb.ChatService_ChatServer) error {
	// Expect first message to be JOIN with user_id
	firstMsg, err := stream.Recv()
	if err != nil {
		return err
	}
	if firstMsg.Type != pb.MessageType_JOIN || firstMsg.UserId == "" {
		return errors.New("first message must be JOIN with user_id")
	}
	userID := firstMsg.UserId

	ctx, cancel := context.WithCancel(stream.Context())
	client := &Client{
		userID: userID,
		stream: stream,
		send:   make(chan *pb.ChatMessage, 64),
		ctx:    ctx,
		cancel: cancel,
	}
	if is_registered := s.hub.Register(client); !is_registered {
		log.Printf("passed userid is already registered, please use a unique UserID")
		return errors.New("username already exists, aborting ")
	}
	log.Printf("user %s joined", userID)

	// writer goroutine
	go func() {
		for {
			select {
			case <-client.ctx.Done():
				return
			case msg, ok := <-client.send:
				if !ok {
					return
				}
				if err := stream.Send(msg); err != nil {
					log.Printf("send error to %s: %v", client.userID, err)
					return
				}
			}
		}
	}()

	// read loop
	for {
		msg, err := stream.Recv()
		if err != nil {
			log.Printf("recv error for %s: %v", userID, err)
			break
		}

		if msg.Timestamp == 0 {
			msg.Timestamp = time.Now().UnixNano() / int64(time.Millisecond)
		}

		switch msg.Type {
		case pb.MessageType_MESSAGE:
			// if To contains "*" -> broadcast
			if len(msg.To) > 0 && containsStar(msg.To) {
				log.Printf("broadcast from %s: %s", userID, msg.Text)
				// ToDO: validate permissions here (e.g., check if user is allowed to broadcast)
				s.hub.Broadcast(msg)
			} else if len(msg.To) == 0 {
				log.Printf("message from %s had no recipients; ignoring", userID)
			} else {
				// direct message(s)
				s.hub.SendTo(msg.To, msg)
			}
		case pb.MessageType_LEAVE:
			log.Printf("%s requested leave", userID)
			goto CLEANUP
		default:
			// handle PING/TYPING etc
		}
	}

CLEANUP:
	s.hub.Unregister(userID)
	cancel()
	return nil
}

// Unary RPC to check if a user is online
func (s *Server) IsOnline(ctx context.Context, req *pb.IsOnlineRequest) (*pb.IsOnlineResponse, error) {
	online := s.hub.IsOnline(req.UserId)
	return &pb.IsOnlineResponse{Online: online}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	grpcServer := grpc.NewServer() // add interceptors / TLS creds in production
	pb.RegisterChatServiceServer(grpcServer, NewServer())
	log.Println("gRPC server listening on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve failed: %v", err)
	}
}
