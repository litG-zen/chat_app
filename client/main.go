package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	pb "github.com/litG-zen/chat_app/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func runClient(myID, addr, targetID string) error {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewChatServiceClient(conn)

	// First, check if target is online via IsOnline RPC (unary)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.IsOnline(ctx, &pb.IsOnlineRequest{UserId: targetID})
	if err != nil {
		return fmt.Errorf("IsOnline RPC failed: %w", err)
	}
	if !resp.Online {
		fmt.Printf("target user %s is offline. Exiting.\n", targetID)
		return nil
	}
	fmt.Printf("target %s is online — opening chat stream...\n", targetID)

	// Open Chat stream
	stream, err := client.Chat(context.Background())
	if err != nil {
		return err
	}

	// send JOIN
	join := &pb.ChatMessage{
		UserId:    myID,
		Type:      pb.MessageType_JOIN,
		Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
	}
	if err := stream.Send(join); err != nil {
		return fmt.Errorf("failed to send join: %w", err)
	}

	// receive goroutine
	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				log.Println("stream.Recv error:", err)
				return
			}
			// only print messages that are for me (or broadcasts)
			// the server already routes direct messages for me only
			fmt.Printf("\n<< [%s] %s\n> ", in.UserId, in.Text)
		}
	}()

	// send loop: read stdin and send messages to target
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Type messages and press Enter. Ctrl+C to exit.")
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Println("read error:", err)
			break
		}
		// build message targeted to the recipient
		msg := &pb.ChatMessage{
			UserId:    myID,
			To:        []string{targetID},
			Type:      pb.MessageType_MESSAGE,
			Text:      line[:len(line)-1], // drop newline
			Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
		}
		if err := stream.Send(msg); err != nil {
			log.Println("send error:", err)
			break
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: client <myUserID> <serverAddr> <targetUserID>")
		fmt.Println("example: go run client/main.go alice localhost:50051 bob")
		return
	}
	myID := os.Args[1]
	addr := os.Args[2]
	targetID := os.Args[3]

	if err := runClient(myID, addr, targetID); err != nil {
		log.Fatalf("client failed: %v", err)
	}
}
