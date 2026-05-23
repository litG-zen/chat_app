package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/litG-zen/chat_app/logs"
	pb "github.com/litG-zen/chat_app/proto"
	"github.com/litG-zen/chat_app/utils"
	"google.golang.org/grpc"
)

const SERVER_INIT_LOGO = `-----LIT SERVER INITATTED----`

// New client structure, with request-context and connection-steam variables.
type Client struct {
	userID string                    //UniqueIdentifier for the registered client
	stream pb.ChatService_ChatServer // established gRPC stream connection
	send   chan *pb.ChatMessage      // message channel to be shared between the connected clients for peer-peer communication
	ctx    context.Context           // request-context definition
	cancel context.CancelFunc        // context cancel function initiation
}

type Hub struct {
	mu      sync.RWMutex       // protects the clients map; ensures thread-safe registration and deregistration of users
	clients map[string]*Client // map of registered clients.
}

var (
	hubInstance    *Hub
	serverInstance *Server
	serverOnce     sync.Once
	once           sync.Once
)

// Singelton property mimicking.
// this way we can keep calling NewHub() but it will always return the same hub.
func GetHub() *Hub {
	once.Do(func() {
		hubInstance = &Hub{
			clients: make(map[string]*Client),
		}
	})
	return hubInstance
}

func (h *Hub) Register(c *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c.userID]; ok {
		log_string := fmt.Sprintf("%v : %v user already registered into the system", time.Now(), c.userID)
		logs.Logger(log_string, true)
		return true
	}
	h.clients[c.userID] = c
	log_string := fmt.Sprintf("%v : %v user registered into the system", time.Now(), c.userID)
	logs.Logger(log_string, false)

	return false
}

func (h *Hub) Unregister(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[userID]; ok {
		log_string := fmt.Sprintf("%v : %v user unregistered from the system", time.Now(), c.userID)
		logs.Logger(log_string, false)

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

// CloseAll cancels every connected client's context and drains the registry.
// Used on shutdown so active bidi streams can unwind instead of blocking
// GracefulStop indefinitely.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for uid, c := range h.clients {
		c.cancel()
		close(c.send)
		delete(h.clients, uid)
	}
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
			message := utils.RedisMessage{
				Sender:    msg.UserId,
				Receiver:  uid,
				Content:   msg.Text,
				Timestamp: msg.Timestamp,
			}
			if err := utils.AddMessageForUser(message); err != nil {
				log.Printf("redis write error for %s: %v", uid, err)
				logs.Logger(fmt.Sprintf("%v : redis write error for %s: %v", time.Now(), uid, err), true)
			}
		}
	}
}

// Broadcast sends msg to all connected clients (non-blocking).
// It will attempt a non-blocking send to each client's send channel.
func (h *Hub) Broadcast(senderID string, msg *pb.ChatMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	log_string := fmt.Sprintf("%v : %v message broadcasted to all users by %v ", time.Now(), msg.Text, senderID)
	logs.Logger(log_string, false)

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

func GetServer() *Server {
	serverOnce.Do(func() {
		serverInstance = &Server{
			hub: GetHub(), // singleton hub
		}
	})
	return serverInstance
}

func containsStar(recipients []string) bool {
	return slices.Contains(recipients, "*")
}

func (s *Server) Chat(stream pb.ChatService_ChatServer) error {
	// Expect first message to be JOIN with user_id
	firstMsg, err := stream.Recv()
	if err != nil {
		return err
	}
	if firstMsg.Type != pb.MessageType_JOIN || firstMsg.UserId == "" {

		log_string := fmt.Sprintf("%v: %v's first message is not of SERVER_JOIN", time.Now(), firstMsg.UserId)
		logs.Logger(log_string, true)

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
	if isAlreadyRegistered := s.hub.Register(client); isAlreadyRegistered {
		log.Printf("%v : passed userid(%v) is already registered, please use a unique UserID", time.Now(), userID)

		log_string := fmt.Sprintf("%v : passed userid(%v) is already registered, please use a unique UserID", time.Now(), userID)
		logs.Logger(log_string, true)

		return errors.New("username already exists, aborting ")
	} else {
		go func(c *Client) {
			msgs, err := utils.FlushMessagesForUser(c.userID)
			if err != nil {
				// log and continue: we don't want to block connection if flush fails
				log.Printf("error flushing messages for %s: %v", c.userID, err)
				logs.Logger(fmt.Sprintf("%v : error flushing messages for %s: %v", time.Now(), c.userID, err), true)
				return
			}
			if len(msgs) == 0 {
				return
			}

			log.Printf("delivering %d stored messages to %s", len(msgs), c.userID)
			for _, rm := range msgs {
				// construct a pb.ChatMessage so the writer goroutine sends it to the client stream
				chatMsg := &pb.ChatMessage{
					Type:      pb.MessageType_MESSAGE,
					UserId:    rm.Sender,
					To:        []string{rm.Receiver},
					Text:      rm.Content,
					Timestamp: rm.Timestamp,
				}

				// Non-blocking send into client's buffered channel (to avoid deadlocks)
				select {
				case c.send <- chatMsg:
				default:
					// client's send buffer is full; log and drop (or handle backpressure differently)
					log.Printf("dropping persisted message for %s: send buffer full", c.userID)
				}
			}
		}(client)
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
					log_string := fmt.Sprintf("%v : send error to %s: %v", time.Now(), client.userID, err)
					logs.Logger(log_string, true)

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
				s.hub.Broadcast(userID, msg)
			} else if len(msg.To) == 0 {
				log.Printf("message from %s had no recipients; ignoring", userID)
			} else {
				// direct message(s)
				s.hub.SendTo(msg.To, msg)
			}
		case pb.MessageType_LEAVE:
			log.Printf("%s requested leave", userID)
			// perform post connection closure actions
			goto CLEANUP
		default:
			// handle PING/TYPING etc
		}
	}

CLEANUP:
	s.hub.Unregister(userID)
	log_string := fmt.Sprintf("%v : %v has been unregistered", time.Now(), userID)
	logs.Logger(log_string, true)
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
	srv := GetServer()
	pb.RegisterChatServiceServer(grpcServer, srv)
	log.Println(SERVER_INIT_LOGO)
	logs.Logger(fmt.Sprintf("%v : Server Bootup!", time.Now()), false)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- grpcServer.Serve(lis)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			log.Fatalf("serve failed: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("received %v, shutting down...", sig)
		logs.Logger(fmt.Sprintf("%v : shutdown signal %v received", time.Now(), sig), false)

		// Force-cancel client streams so handlers return, then let GracefulStop
		// flush in-flight RPCs. Fall back to Stop() if it drags on.
		srv.hub.CloseAll()

		done := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(done)
		}()

		select {
		case <-done:
			log.Println("server stopped gracefully")
		case <-time.After(10 * time.Second):
			log.Println("graceful stop timed out, forcing shutdown")
			grpcServer.Stop()
		}
	}
}
